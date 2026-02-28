package scsi

import (
	"encoding/binary"
	"fmt"
)

// WRITE opcode constants.
const (
	OpcodeWrite6  byte = 0x0A
	OpcodeWrite10 byte = 0x2A
	OpcodeWrite16 byte = 0x8A
)

// handleWrite6 processes a WRITE(6) CDB.
// CDB layout (6 bytes):
//   0: opcode (0x0A)
//   1-3: LBA (21 bits: bits 4-0 of byte 1 + bytes 2-3)
//   4: Transfer Length (0 = 256 sectors)
//   5: Control
func handleWrite6(cdb []byte, data []byte, dev BlockDevice) error {
	if len(cdb) < 6 {
		return fmt.Errorf("WRITE(6) CDB too short: %d bytes", len(cdb))
	}
	lba := uint64(cdb[1]&0x1F)<<16 | uint64(cdb[2])<<8 | uint64(cdb[3])
	count := uint32(cdb[4])
	if count == 0 {
		count = 256
	}

	expectedLen := int(count) * 512
	if len(data) < expectedLen {
		return fmt.Errorf("WRITE(6) data too short: got %d, want %d", len(data), expectedLen)
	}
	return dev.Write(lba, data[:expectedLen])
}

// handleWrite10 processes a WRITE(10) CDB.
// CDB layout (10 bytes):
//   0: opcode (0x2A)
//   1: flags
//   2-5: LBA (big-endian uint32)
//   6: Group number
//   7-8: Transfer Length (big-endian uint16)
//   9: Control
func handleWrite10(cdb []byte, data []byte, dev BlockDevice) error {
	if len(cdb) < 10 {
		return fmt.Errorf("WRITE(10) CDB too short: %d bytes", len(cdb))
	}
	lba := uint64(binary.BigEndian.Uint32(cdb[2:6]))
	count := uint32(binary.BigEndian.Uint16(cdb[7:9]))
	if count == 0 {
		return nil
	}

	expectedLen := int(count) * 512
	if len(data) < expectedLen {
		return fmt.Errorf("WRITE(10) data too short: got %d, want %d", len(data), expectedLen)
	}
	return dev.Write(lba, data[:expectedLen])
}

// handleWrite16 processes a WRITE(16) CDB.
// CDB layout (16 bytes):
//   0: opcode (0x8A)
//   1: flags
//   2-9: LBA (big-endian uint64)
//   10-13: Transfer Length (big-endian uint32)
//   14: Group number
//   15: Control
func handleWrite16(cdb []byte, data []byte, dev BlockDevice) error {
	if len(cdb) < 16 {
		return fmt.Errorf("WRITE(16) CDB too short: %d bytes", len(cdb))
	}
	lba := binary.BigEndian.Uint64(cdb[2:10])
	count := binary.BigEndian.Uint32(cdb[10:14])
	if count == 0 {
		return nil
	}

	expectedLen := int(count) * 512
	if len(data) < expectedLen {
		return fmt.Errorf("WRITE(16) data too short: got %d, want %d", len(data), expectedLen)
	}
	return dev.Write(lba, data[:expectedLen])
}
