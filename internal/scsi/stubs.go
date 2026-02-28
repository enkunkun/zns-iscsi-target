package scsi

import "encoding/binary"

// Stub opcode constants.
const (
	OpcodeTestUnitReady     byte = 0x00
	OpcodeReportLUNs        byte = 0xA0
	OpcodeModeSense10       byte = 0x5A
	OpcodeModeSense6        byte = 0x1A
	OpcodePersistentReserveIn  byte = 0x5E
	OpcodePersistentReserveOut byte = 0x5F
	OpcodeUnmap             byte = 0x42
	OpcodeRequestSense      byte = 0x03
	OpcodeStartStopUnit     byte = 0x1B
)

// handleTestUnitReady responds to TEST UNIT READY (opcode 0x00).
// Returns empty data and StatusGood indicating the device is ready.
func handleTestUnitReady() ([]byte, byte, []byte) {
	return []byte{}, StatusGood, nil
}

// handleReportLUNs responds to REPORT LUNS (opcode 0xA0).
// Returns a single LUN 0 in the standard format.
func handleReportLUNs() []byte {
	// Header: LUN List Length (4 bytes) + Reserved (4 bytes) + LUN entries
	// Each LUN entry is 8 bytes.
	buf := make([]byte, 16)
	binary.BigEndian.PutUint32(buf[0:4], 8) // LUN list length: 8 bytes (one LUN)
	// bytes 4-7: reserved
	// LUN 0: 8 bytes, first byte = 0x00 (single-level), rest 0
	// buf[8:16] = all zeros = LUN 0
	return buf
}

// handleModeSense10 responds to MODE SENSE (10) (opcode 0x5A).
// Returns a minimal mode parameter header with no page data.
func handleModeSense10() []byte {
	// Minimal mode parameter header for MODE SENSE(10): 8 bytes
	buf := make([]byte, 8)
	// Mode Data Length (2 bytes, big-endian): length of subsequent bytes
	binary.BigEndian.PutUint16(buf[0:2], uint16(len(buf)-2)) // 6
	// Medium Type: 0x00 (default)
	buf[2] = 0x00
	// Device-Specific Parameter: 0x00 (no WP, no DPOFUA)
	buf[3] = 0x00
	// Long LBA bit: buf[4] bit 0 = 0
	buf[4] = 0x00
	// Reserved
	buf[5] = 0x00
	// Block Descriptor Length (2 bytes): 0 (no block descriptor)
	binary.BigEndian.PutUint16(buf[6:8], 0)
	return buf
}

// handleModeSense6 responds to MODE SENSE (6) (opcode 0x1A).
// Returns a minimal mode parameter header with no page data.
func handleModeSense6() []byte {
	// Minimal mode parameter header for MODE SENSE(6): 4 bytes
	buf := make([]byte, 4)
	buf[0] = byte(len(buf) - 1) // Mode Data Length: 3
	buf[1] = 0x00               // Medium Type
	buf[2] = 0x00               // Device-Specific Parameter
	buf[3] = 0x00               // Block Descriptor Length = 0
	return buf
}

// handlePersistentReserveIn responds to PERSISTENT RESERVE IN with ILLEGAL REQUEST.
func handlePersistentReserveIn() ([]byte, byte, []byte) {
	return nil, StatusCheckCondition, SenseIllegalRequest
}

// handlePersistentReserveOut responds to PERSISTENT RESERVE OUT with ILLEGAL REQUEST.
func handlePersistentReserveOut() ([]byte, byte, []byte) {
	return nil, StatusCheckCondition, SenseIllegalRequest
}

// handleUnmap processes an UNMAP CDB (opcode 0x42).
// The parameter list in dataOut contains LBA ranges to unmap.
func handleUnmap(cdb []byte, dataOut []byte, dev BlockDevice) error {
	if len(dataOut) < 8 {
		// No UNMAP block descriptors; nothing to do.
		return nil
	}

	// UNMAP parameter list header:
	//   bytes 0-1: UNMAP Data Length
	//   bytes 2-3: UNMAP Block Descriptor Data Length
	//   bytes 4-7: Reserved
	// Each UNMAP block descriptor (16 bytes):
	//   bytes 0-7: UNMAP Logical Block Address (big-endian uint64)
	//   bytes 8-11: Number of Logical Blocks (big-endian uint32)
	//   bytes 12-15: Reserved

	descDataLen := int(binary.BigEndian.Uint16(dataOut[2:4]))
	if descDataLen == 0 || len(dataOut) < 8 {
		return nil
	}

	descs := dataOut[8:]
	for len(descs) >= 16 {
		lba := binary.BigEndian.Uint64(descs[0:8])
		count := binary.BigEndian.Uint32(descs[8:12])
		if count > 0 {
			if err := dev.Unmap(lba, count); err != nil {
				return err
			}
		}
		descs = descs[16:]
	}
	return nil
}

// handleRequestSense responds to REQUEST SENSE with current no-sense data.
func handleRequestSense() []byte {
	return BuildSense(SenseKeyNoSense, ASCNoAdditionalSenseInfo, 0x00)
}

// handleStartStopUnit responds to START STOP UNIT with GOOD status.
func handleStartStopUnit() ([]byte, byte, []byte) {
	return []byte{}, StatusGood, nil
}
