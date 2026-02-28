// Package zbc provides ZBC/ZAC (Zoned Block Commands) types and constants.
package zbc

// ZBC SCSI opcodes.
const (
	// OpcodeReportZones is the ZBC REPORT ZONES command opcode.
	OpcodeReportZones = 0x95

	// OpcodeZoneAction is the ZBC ZONE ACTION command opcode.
	OpcodeZoneAction = 0x9F
)

// Zone action codes used with OpcodeZoneAction.
const (
	ZoneActionClose = 0x01
	ZoneActionFinish = 0x02
	ZoneActionOpen  = 0x03
	ZoneActionReset = 0x04
)

// Reporting options for REPORT ZONES command (byte 14).
const (
	ReportingAll            = 0x00
	ReportingEmpty          = 0x01
	ReportingImplicitOpen   = 0x02
	ReportingExplicitOpen   = 0x03
	ReportingClosed         = 0x04
	ReportingFull           = 0x05
	ReportingReadOnly       = 0x06
	ReportingOffline        = 0x07
	ReportingNotWritePointer = 0x3F
)

// ZoneDescriptorSize is the size of a zone descriptor in bytes (ZBC standard).
const ZoneDescriptorSize = 64

// ReportZonesHeaderSize is the size of the REPORT ZONES response header in bytes.
const ReportZonesHeaderSize = 64

// SectorSize is the standard sector size for ZBC/ZAC devices.
const SectorSize = 512
