//go:build linux

package smr

import (
	"fmt"
	"os"
	"sync"
	"sync/atomic"

	"github.com/enkunkun/zns-iscsi-target/internal/backend"
	"github.com/enkunkun/zns-iscsi-target/pkg/zbc"
)

// SMRBackend is a ZonedDevice implementation for SATA SMR drives via SG_IO.
type SMRBackend struct {
	mu           sync.Mutex
	fd           int
	file         *os.File
	zoneCount    atomic.Int32
	zoneSectors  atomic.Uint64
	maxOpenZones atomic.Int32
	capacity     atomic.Uint64
}

// Open opens a SATA SMR device at the given path and returns an SMRBackend.
func Open(devicePath string) (*SMRBackend, error) {
	f, err := os.OpenFile(devicePath, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("opening SMR device %q: %w", devicePath, err)
	}

	smr := &SMRBackend{
		fd:   int(f.Fd()),
		file: f,
	}

	// Discover zone information from device
	if err := smr.discover(); err != nil {
		f.Close()
		return nil, err
	}

	return smr, nil
}

// discover queries the device for zone information.
func (s *SMRBackend) discover() error {
	zones, err := reportZones(s.fd, 0, 1)
	if err != nil {
		return fmt.Errorf("discover: %w", err)
	}
	if len(zones) == 0 {
		return fmt.Errorf("discover: no zones reported by device")
	}

	// Use first zone to determine zone size
	s.zoneSectors.Store(zones[0].ZoneLength)

	// Get all zones to determine total zone count
	allZones, err := reportZones(s.fd, 0, 65536)
	if err != nil {
		return fmt.Errorf("discover all zones: %w", err)
	}
	s.zoneCount.Store(int32(len(allZones)))

	// Calculate total capacity
	totalSectors := uint64(len(allZones)) * zones[0].ZoneLength
	s.capacity.Store(totalSectors)

	// SMR drives typically support 14 open zones (ZAC requirement)
	// This can be overridden; for now use a conservative default.
	s.maxOpenZones.Store(14)

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
	return reportZones(s.fd, startLBA, maxZones)
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
	return zoneAction(s.fd, zbc.ZoneActionOpen, zoneStartLBA, false)
}

// CloseZone sends a CLOSE ZONE command.
func (s *SMRBackend) CloseZone(zoneStartLBA uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return zoneAction(s.fd, zbc.ZoneActionClose, zoneStartLBA, false)
}

// FinishZone sends a FINISH ZONE command.
func (s *SMRBackend) FinishZone(zoneStartLBA uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return zoneAction(s.fd, zbc.ZoneActionFinish, zoneStartLBA, false)
}

// ResetZone sends a RESET WRITE POINTER command.
func (s *SMRBackend) ResetZone(zoneStartLBA uint64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return zoneAction(s.fd, zbc.ZoneActionReset, zoneStartLBA, false)
}

// ReadSectors reads sectors from the device using SCSI READ(16).
func (s *SMRBackend) ReadSectors(lba uint64, count uint32) ([]byte, error) {
	const sectorSize = 512
	buf := make([]byte, int(count)*sectorSize)
	cdb := buildRead16CDB(lba, count)

	s.mu.Lock()
	err := sgRead(s.fd, cdb, buf, defaultTimeout)
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
	err := sgWrite(s.fd, cdb, data, defaultTimeout)
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

// Close closes the device file descriptor.
func (s *SMRBackend) Close() error {
	return s.file.Close()
}
