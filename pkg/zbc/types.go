// Package zbc provides ZBC/ZAC (Zoned Block Commands) types and constants.
package zbc

// ZoneState represents the state of a zone.
type ZoneState uint8

const (
	ZoneStateNotWritePointer ZoneState = 0x00
	ZoneStateEmpty           ZoneState = 0x01
	ZoneStateImplicitOpen    ZoneState = 0x02
	ZoneStateExplicitOpen    ZoneState = 0x03
	ZoneStateClosed          ZoneState = 0x04
	ZoneStateReadOnly        ZoneState = 0x0D
	ZoneStateFull            ZoneState = 0x0E
	ZoneStateOffline         ZoneState = 0x0F
)

// String returns a human-readable name for the zone state.
func (s ZoneState) String() string {
	switch s {
	case ZoneStateNotWritePointer:
		return "NOT_WRITE_POINTER"
	case ZoneStateEmpty:
		return "EMPTY"
	case ZoneStateImplicitOpen:
		return "IMPLICIT_OPEN"
	case ZoneStateExplicitOpen:
		return "EXPLICIT_OPEN"
	case ZoneStateClosed:
		return "CLOSED"
	case ZoneStateReadOnly:
		return "READ_ONLY"
	case ZoneStateFull:
		return "FULL"
	case ZoneStateOffline:
		return "OFFLINE"
	default:
		return "UNKNOWN"
	}
}

// ZoneType represents the type of a zone.
type ZoneType uint8

const (
	ZoneTypeConventional    ZoneType = 0x01
	ZoneTypeSequentialWrite ZoneType = 0x02 // Sequential Write Required
	ZoneTypeSeqWritePref    ZoneType = 0x03 // Sequential Write Preferred
)

// String returns a human-readable name for the zone type.
func (t ZoneType) String() string {
	switch t {
	case ZoneTypeConventional:
		return "CONVENTIONAL"
	case ZoneTypeSequentialWrite:
		return "SEQUENTIAL_WRITE_REQUIRED"
	case ZoneTypeSeqWritePref:
		return "SEQUENTIAL_WRITE_PREFERRED"
	default:
		return "UNKNOWN"
	}
}

// ZoneCondition is encoded in the zone descriptor byte 0 bits 7-4.
type ZoneCondition uint8

const (
	ZoneConditionNotWritePointer ZoneCondition = 0x0
	ZoneConditionEmpty           ZoneCondition = 0x1
	ZoneConditionImplicitOpen    ZoneCondition = 0x2
	ZoneConditionExplicitOpen    ZoneCondition = 0x3
	ZoneConditionClosed          ZoneCondition = 0x4
	ZoneConditionReadOnly        ZoneCondition = 0xD
	ZoneConditionFull            ZoneCondition = 0xE
	ZoneConditionOffline         ZoneCondition = 0xF
)

// String returns a human-readable name for the zone condition.
func (c ZoneCondition) String() string {
	switch c {
	case ZoneConditionNotWritePointer:
		return "NOT_WRITE_POINTER"
	case ZoneConditionEmpty:
		return "EMPTY"
	case ZoneConditionImplicitOpen:
		return "IMPLICIT_OPEN"
	case ZoneConditionExplicitOpen:
		return "EXPLICIT_OPEN"
	case ZoneConditionClosed:
		return "CLOSED"
	case ZoneConditionReadOnly:
		return "READ_ONLY"
	case ZoneConditionFull:
		return "FULL"
	case ZoneConditionOffline:
		return "OFFLINE"
	default:
		return "UNKNOWN"
	}
}

// ZoneDescriptor describes a single zone on a zoned block device.
// This corresponds to the 64-byte ZBC zone descriptor format.
type ZoneDescriptor struct {
	ZoneType     ZoneType
	ZoneState    ZoneState
	ZoneCondition ZoneCondition
	Reset        bool   // Reset Recommended
	NonSeq       bool   // Non-Sequential Write Resources Active
	ZoneLength   uint64 // in sectors
	ZoneStartLBA uint64
	WritePointer uint64 // absolute LBA of next write position
}

// IsWritePointerZone returns true if this zone uses a write pointer (sequential zones).
func (z *ZoneDescriptor) IsWritePointerZone() bool {
	return z.ZoneType != ZoneTypeConventional
}

// UsedCapacitySectors returns the number of sectors written in this zone.
func (z *ZoneDescriptor) UsedCapacitySectors() uint64 {
	if z.ZoneState == ZoneStateFull {
		return z.ZoneLength
	}
	if z.WritePointer <= z.ZoneStartLBA {
		return 0
	}
	return z.WritePointer - z.ZoneStartLBA
}
