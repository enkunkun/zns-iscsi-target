// Package scsi implements SCSI command processing for the iSCSI target.
package scsi

// SCSI status bytes (SAM-4 Table 25).
const (
	StatusGood           byte = 0x00
	StatusCheckCondition byte = 0x02
	StatusBusy           byte = 0x08
	StatusReservation    byte = 0x18
)

// Sense key values (SPC-4 Table 27).
const (
	SenseKeyNoSense        byte = 0x00
	SenseKeyRecoveredError byte = 0x01
	SenseKeyNotReady       byte = 0x02
	SenseKeyMediumError    byte = 0x03
	SenseKeyHardwareError  byte = 0x04
	SenseKeyIllegalRequest byte = 0x05
	SenseKeyUnitAttention  byte = 0x06
	SenseKeyDataProtect    byte = 0x07
	SenseKeyAbortedCommand byte = 0x0B
)

// ASC/ASCQ codes used by this implementation.
const (
	ASCInvalidCommandOpCode   byte = 0x20 // Invalid command operation code
	ASCInvalidFieldInCDB      byte = 0x24 // Invalid field in CDB
	ASCLogicalUnitNotReady    byte = 0x04 // Logical unit not ready
	ASCMediumNotPresent       byte = 0x3A // Medium not present
	ASCNoAdditionalSenseInfo  byte = 0x00 // No additional sense information
	ASCWriteError             byte = 0x0C // Write error
	ASCUnrecoveredReadError   byte = 0x11 // Unrecovered read error
	ASCParameterListLengthErr byte = 0x1A // Parameter list length error
	ASCCommandSequenceError   byte = 0x2C // Command sequence error
)

// BuildSense builds a fixed-format sense data buffer (SPC-4 Section 4.5.3).
// key is the sense key, asc is the Additional Sense Code, ascq is ASC Qualifier.
// Returns an 18-byte fixed-format sense buffer.
func BuildSense(key, asc, ascq byte) []byte {
	buf := make([]byte, 18)
	buf[0] = 0x70              // Response code: current errors, fixed format
	buf[1] = 0x00              // Obsolete
	buf[2] = key & 0x0F       // Sense key (lower 4 bits)
	buf[3] = 0x00              // Information (4 bytes, big-endian)
	buf[4] = 0x00
	buf[5] = 0x00
	buf[6] = 0x00
	buf[7] = 0x0A              // Additional sense length (bytes remaining after this = 10)
	buf[8] = 0x00              // Command-specific information
	buf[9] = 0x00
	buf[10] = 0x00
	buf[11] = 0x00
	buf[12] = asc              // Additional Sense Code
	buf[13] = ascq             // Additional Sense Code Qualifier
	buf[14] = 0x00             // Field replaceable unit code
	buf[15] = 0x00             // Sense key specific
	buf[16] = 0x00
	buf[17] = 0x00
	return buf
}

// Pre-built sense buffers for common conditions.
var (
	// SenseNoSense is returned when no error occurred (sense key 0x00).
	SenseNoSense = BuildSense(SenseKeyNoSense, ASCNoAdditionalSenseInfo, 0x00)

	// SenseIllegalRequest is returned for invalid commands or CDB fields.
	SenseIllegalRequest = BuildSense(SenseKeyIllegalRequest, ASCInvalidCommandOpCode, 0x00)

	// SenseIllegalRequestInvalidField is returned for invalid field in CDB.
	SenseIllegalRequestInvalidField = BuildSense(SenseKeyIllegalRequest, ASCInvalidFieldInCDB, 0x00)

	// SenseMediumError is returned on medium read/write errors.
	SenseMediumError = BuildSense(SenseKeyMediumError, ASCUnrecoveredReadError, 0x00)

	// SenseNotReady is returned when the device is not ready.
	SenseNotReady = BuildSense(SenseKeyNotReady, ASCLogicalUnitNotReady, 0x00)
)
