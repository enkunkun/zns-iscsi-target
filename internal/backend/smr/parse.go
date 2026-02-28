package smr

import (
	"encoding/binary"
	"fmt"

	"github.com/enkunkun/zns-iscsi-target/pkg/zbc"
)

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

// parseReportZonesResponse parses the binary response from a REPORT ZONES command.
// The response starts with a 64-byte header followed by zone descriptors.
func parseReportZonesResponse(buf []byte) ([]zbc.ZoneDescriptor, error) {
	const headerSize = zbc.ReportZonesHeaderSize
	const descSize = zbc.ZoneDescriptorSize

	if len(buf) < headerSize {
		return nil, fmt.Errorf("report zones response too short: %d < %d", len(buf), headerSize)
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
