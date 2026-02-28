package smr

import (
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/enkunkun/zns-iscsi-target/internal/backend"
	"github.com/enkunkun/zns-iscsi-target/pkg/zbc"
)

// SMRBackend is a ZonedDevice implementation for SATA SMR drives.
// It sends SCSI CDBs through a platform-specific ScsiTransport.
type SMRBackend struct {
	mu           sync.Mutex
	transport    ScsiTransport
	zoneCount    atomic.Int32
	zoneSectors  atomic.Uint64
	maxOpenZones atomic.Int32
	capacity     atomic.Uint64
}

// newSMRBackend creates an SMRBackend using the given transport and discovers zone geometry.
func newSMRBackend(transport ScsiTransport) (*SMRBackend, error) {
	s := &SMRBackend{transport: transport}

	if err := s.discover(); err != nil {
		transport.Close()
		return nil, err
	}

	return s, nil
}

// verifyDevice checks that the device is a Host-Managed zoned block device.
// It uses Standard INQUIRY to check the Peripheral Device Type and, if needed,
// VPD page 0xB1 to confirm the zoned model.
func (s *SMRBackend) verifyDevice() error {
	const inquiryLen = 96

	// Step 1: Standard INQUIRY
	inqBuf := make([]byte, inquiryLen)
	inqCDB := buildInquiryCDB(inquiryLen)
	if err := s.transport.ScsiRead(inqCDB, inqBuf, defaultTimeout); err != nil {
		return fmt.Errorf("INQUIRY: %w", err)
	}

	pdt, err := parseInquiryBasic(inqBuf)
	if err != nil {
		return fmt.Errorf("parse INQUIRY: %w", err)
	}

	// Step 2: Check Peripheral Device Type
	switch pdt {
	case zbc.PeripheralDeviceTypeZBC:
		// 0x14 — Host-Managed Zoned Block Device confirmed
		return nil
	case 0x00:
		// Standard disk — need VPD to check zoned model
	default:
		return fmt.Errorf("unsupported device type 0x%02x: not a zoned block device", pdt)
	}

	// Step 3: VPD page 0xB1 (Block Device Characteristics)
	const vpdLen = 64
	vpdBuf := make([]byte, vpdLen)
	vpdCDB := buildInquiryVPDCDB(zbc.VPDPageBlockDeviceChar, vpdLen)
	if err := s.transport.ScsiRead(vpdCDB, vpdBuf, defaultTimeout); err != nil {
		return fmt.Errorf("INQUIRY VPD 0xB1: %w", err)
	}

	zoned, err := parseVPDB1Zoned(vpdBuf)
	if err != nil {
		return fmt.Errorf("parse VPD B1: %w", err)
	}

	switch zoned {
	case 0x02:
		// Host-Managed confirmed via VPD
		return nil
	case 0x01:
		return fmt.Errorf("Host-Aware zoned device is not supported; sequential write constraints are advisory, not mandatory")
	case 0x00:
		return fmt.Errorf("device is not a zoned block device (VPD B1 zoned=0)")
	default:
		return fmt.Errorf("unsupported zoned model 0x%02x in VPD B1", zoned)
	}
}

// discover queries the device for zone information.
func (s *SMRBackend) discover() error {
	if err := s.verifyDevice(); err != nil {
		return fmt.Errorf("device verification failed: %w", err)
	}

	zones, err := s.reportZones(0, 1)
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}
	if len(zones) == 0 {
		return fmt.Errorf("discover: no zones reported by device")
	}

	// Use first zone to determine zone size
	s.zoneSectors.Store(zones[0].ZoneLength)

	// Get all zones to determine total zone count
	allZones, err := s.reportZones(0, 65536)
	if err != nil {
		return fmt.Errorf("discover all zones: %w", err)
	}
	s.zoneCount.Store(int32(len(allZones)))

	// Calculate total capacity
	totalSectors := uint64(len(allZones)) * zones[0].ZoneLength
	s.capacity.Store(totalSectors)

	// SMR drives typically support 14 open zones (ZAC requirement)
	s.maxOpenZones.Store(14)

	return nil
}

// reportZones issues a REPORT ZONES command and returns the zone descriptors.
func (s *SMRBackend) reportZones(startLBA uint64, maxZones int) ([]zbc.ZoneDescriptor, error) {
	const headerSize = zbc.ReportZonesHeaderSize
	const descSize = zbc.ZoneDescriptorSize

	allocLen := uint32(headerSize + maxZones*descSize)
	if allocLen > reportZonesMaxLen {
		allocLen = reportZonesMaxLen
	}

	buf := make([]byte, allocLen)
	cdb := buildReportZonesCDB(startLBA, allocLen, zbc.ReportingAll)

	if err := s.transport.ScsiRead(cdb, buf, defaultTimeout); err != nil {
		return nil, fmt.Errorf("REPORT ZONES: %w", err)
	}

	return parseReportZonesResponse(buf)
}

// zoneAction issues a ZONE ACTION command.
func (s *SMRBackend) zoneAction(action uint8, startLBA uint64, all bool) error {
	cdb := buildZoneActionCDB(action, startLBA, all)
	if err := s.transport.ScsiNoData(cdb, defaultTimeout); err != nil {
		return fmt.Errorf("ZONE ACTION 0x%02x: %w", action, err)
	}
	return nil
}

// ReportZones returns zone descriptors.
func (s *SMRBackend) ReportZones(startLBA uint64, count int) ([]zbc.ZoneDescriptor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	maxZones := count
	if maxZones <= 0 {
		maxZones = int(s.zoneCount.Load())
	}
	return s.reportZones(startLBA, maxZones)
}

// ZoneCount returns the total number of zones.
func (s *SMRBackend) ZoneCount() int {
	return int(s.zoneCount.Load())
}

// ZoneSize returns the zone size in sectors.
func (s *SMRBackend) ZoneSize() uint64 {
	return s.zoneSectors.Load()
}

// OpenZone sends an explicit OPEN ZONE command.
func (s *SMRBackend) OpenZone(zoneStartLBA uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.zoneAction(zbc.ZoneActionOpen, zoneStartLBA, false)
}

// CloseZone sends a CLOSE ZONE command.
func (s *SMRBackend) CloseZone(zoneStartLBA uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.zoneAction(zbc.ZoneActionClose, zoneStartLBA, false)
}

// FinishZone sends a FINISH ZONE command.
func (s *SMRBackend) FinishZone(zoneStartLBA uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.zoneAction(zbc.ZoneActionFinish, zoneStartLBA, false)
}

// ResetZone sends a RESET WRITE POINTER command.
func (s *SMRBackend) ResetZone(zoneStartLBA uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.zoneAction(zbc.ZoneActionReset, zoneStartLBA, false)
}

// ReadSectors reads sectors from the device using SCSI READ(16).
func (s *SMRBackend) ReadSectors(lba uint64, count uint32) ([]byte, error) {
	const sectorSize = 512
	buf := make([]byte, int(count)*sectorSize)
	cdb := buildRead16CDB(lba, count)

	s.mu.Lock()
	err := s.transport.ScsiRead(cdb, buf, defaultTimeout)
	s.mu.Unlock()

	if err != nil {
		return nil, fmt.Errorf("ReadSectors lba=%d count=%d: %w", lba, count, err)
	}
	return buf, nil
}

// WriteSectors writes sectors to the device using SCSI WRITE(16).
func (s *SMRBackend) WriteSectors(lba uint64, data []byte) error {
	const sectorSize = 512
	if len(data)%sectorSize != 0 {
		return fmt.Errorf("%w: data len %d", backend.ErrAlignmentError, len(data))
	}

	count := uint32(len(data) / sectorSize)
	cdb := buildWrite16CDB(lba, count)

	s.mu.Lock()
	err := s.transport.ScsiWrite(cdb, data, defaultTimeout)
	s.mu.Unlock()

	if err != nil {
		return fmt.Errorf("WriteSectors lba=%d count=%d: %w", lba, count, err)
	}
	return nil
}

// BlockSize returns the logical block size (always 512 for SATA).
func (s *SMRBackend) BlockSize() uint32 {
	return 512
}

// Capacity returns the total device capacity in sectors.
func (s *SMRBackend) Capacity() uint64 {
	return s.capacity.Load()
}

// MaxOpenZones returns the maximum simultaneously open zones.
func (s *SMRBackend) MaxOpenZones() int {
	return int(s.maxOpenZones.Load())
}

// Close closes the underlying transport.
func (s *SMRBackend) Close() error {
	return s.transport.Close()
}
