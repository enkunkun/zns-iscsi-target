//go:build linux

package smr

import (
	"encoding/binary"
	"fmt"

	"github.com/enkunkun/zns-iscsi-target/pkg/zbc"
)

const (
	// defaultTimeout is the SG_IO command timeout in milliseconds.
	defaultTimeout = 30000

	// reportZonesMaxLen is the maximum allocation length for REPORT ZONES.
	reportZonesMaxLen = 512 * 1024
)

// buildReportZonesCDB builds the REPORT ZONES CDB (opcode 0x95).
func buildReportZonesCDB(startLBA uint64, allocLen uint32, reportingOptions uint8) []byte {
	cdb := make([]byte, 16)
	cdb[0] = zbc.OpcodeReportZones
	cdb[1] = 0x00
	binary.BigEndian.PutUint64(cdb[2:10], startLBA)
	binary.BigEndian.PutUint32(cdb[10:14], allocLen)
	cdb[14] = reportingOptions
	cdb[15] = 0x00
	return cdb
}

// buildZoneActionCDB builds the ZONE ACTION CDB (opcode 0x9F).
func buildZoneActionCDB(action uint8, startLBA uint64, all bool) []byte {
	cdb := make([]byte, 16)
	cdb[0] = zbc.OpcodeZoneAction
	cdb[1] = action
	binary.BigEndian.PutUint64(cdb[2:10], startLBA)
	// bytes 10-13: reserved
	if all {
		cdb[14] = 0x01
	}
	cdb[15] = 0x00
	return cdb
}

// buildRead16CDB builds a SCSI READ(16) CDB.
func buildRead16CDB(lba uint64, count uint32) []byte {
	cdb := make([]byte, 16)
	cdb[0] = 0x88 // READ(16)
	binary.BigEndian.PutUint64(cdb[2:10], lba)
	binary.BigEndian.PutUint32(cdb[10:14], count)
	return cdb
}

// buildWrite16CDB builds a SCSI WRITE(16) CDB.
func buildWrite16CDB(lba uint64, count uint32) []byte {
	cdb := make([]byte, 16)
	cdb[0] = 0x8A // WRITE(16)
	binary.BigEndian.PutUint64(cdb[2:10], lba)
	binary.BigEndian.PutUint32(cdb[10:14], count)
	return cdb
}

// parseZoneDescriptor parses a 64-byte zone descriptor from the REPORT ZONES response.
func parseZoneDescriptor(data []byte) (zbc.ZoneDescriptor, error) {
	if len(data) < zbc.ZoneDescriptorSize {
		return zbc.ZoneDescriptor{}, fmt.Errorf("zone descriptor too short: %d < %d", len(data), zbc.ZoneDescriptorSize)
	}

	byte0 := data[0]
	zoneType := zbc.ZoneType(byte0 & 0x0F)
	zoneCondition := zbc.ZoneCondition((byte0 >> 4) & 0x0F)
	byte1 := data[1]
	reset := (byte1 & 0x80) != 0
	nonSeq := (byte1 & 0x40) != 0

	zoneLength := binary.BigEndian.Uint64(data[8:16])
	zoneStartLBA := binary.BigEndian.Uint64(data[16:24])
	writePointer := binary.BigEndian.Uint64(data[24:32])

	// Map zone condition to zone state
	var zoneState zbc.ZoneState
	switch zoneCondition {
	case zbc.ZoneConditionNotWritePointer:
		zoneState = zbc.ZoneStateNotWritePointer
	case zbc.ZoneConditionEmpty:
		zoneState = zbc.ZoneStateEmpty
	case zbc.ZoneConditionImplicitOpen:
		zoneState = zbc.ZoneStateImplicitOpen
	case zbc.ZoneConditionExplicitOpen:
		zoneState = zbc.ZoneStateExplicitOpen
	case zbc.ZoneConditionClosed:
		zoneState = zbc.ZoneStateClosed
	case zbc.ZoneConditionReadOnly:
		zoneState = zbc.ZoneStateReadOnly
	case zbc.ZoneConditionFull:
		zoneState = zbc.ZoneStateFull
	case zbc.ZoneConditionOffline:
		zoneState = zbc.ZoneStateOffline
	default:
		zoneState = zbc.ZoneStateEmpty
	}

	return zbc.ZoneDescriptor{
		ZoneType:      zoneType,
		ZoneState:     zoneState,
		ZoneCondition: zoneCondition,
		Reset:         reset,
		NonSeq:        nonSeq,
		ZoneLength:    zoneLength,
		ZoneStartLBA:  zoneStartLBA,
		WritePointer:  writePointer,
	}, nil
}

// reportZones issues a REPORT ZONES command and returns the zone descriptors.
func reportZones(fd int, startLBA uint64, maxZones int) ([]zbc.ZoneDescriptor, error) {
	const headerSize = zbc.ReportZonesHeaderSize
	const descSize = zbc.ZoneDescriptorSize

	allocLen := uint32(headerSize + maxZones*descSize)
	if allocLen > reportZonesMaxLen {
		allocLen = reportZonesMaxLen
	}

	buf := make([]byte, allocLen)
	cdb := buildReportZonesCDB(startLBA, allocLen, zbc.ReportingAll)

	if err := sgRead(fd, cdb, buf, defaultTimeout); err != nil {
		return nil, fmt.Errorf("REPORT ZONES: %w", err)
	}

	// Parse header: bytes 0-3 is the zone list length (excludes header itself)
	zoneListLen := binary.BigEndian.Uint32(buf[0:4])
	numZones := int(zoneListLen) / descSize
	if numZones == 0 {
		return nil, nil
	}

	zones := make([]zbc.ZoneDescriptor, 0, numZones)
	offset := headerSize
	for i := 0; i < numZones; i++ {
		if offset+descSize > len(buf) {
			break
		}
		desc, err := parseZoneDescriptor(buf[offset : offset+descSize])
		if err != nil {
			return nil, fmt.Errorf("parsing zone descriptor %d: %w", i, err)
		}
		zones = append(zones, desc)
		offset += descSize
	}

	return zones, nil
}

// zoneAction issues a ZONE ACTION command.
func zoneAction(fd int, action uint8, startLBA uint64, all bool) error {
	cdb := buildZoneActionCDB(action, startLBA, all)
	if err := sgNoData(fd, cdb, defaultTimeout); err != nil {
		return fmt.Errorf("ZONE ACTION 0x%02x: %w", action, err)
	}
	return nil
}
