package scsi

import (
	"encoding/binary"
	"fmt"
)

// READ opcode constants.
const (
	OpcodeRead6  byte = 0x08
	OpcodeRead10 byte = 0x28
	OpcodeRead16 byte = 0x88
)

// handleRead6 processes a READ(6) CDB.
// CDB layout (6 bytes):
//   0: opcode (0x08)
//   1-3: LBA (21 bits: bits 4-0 of byte 1 + bytes 2-3)
//   4: Transfer Length (0 = 256)
//   5: Control
func handleRead6(cdb []byte, dev BlockDevice) ([]byte, error) {
	if len(cdb) < 6 {
		return nil, fmt.Errorf("READ(6) CDB too short: %d bytes", len(cdb))
	}
	lba := uint64(cdb[1]&0x1F)<<16 | uint64(cdb[2])<<8 | uint64(cdb[3])
	count := uint32(cdb[4])
	if count == 0 {
		count = 256
	}
	return dev.Read(lba, count)
}

// handleRead10 processes a READ(10) CDB.
// CDB layout (10 bytes):
//   0: opcode (0x28)
//   1: flags
//   2-5: LBA (big-endian uint32)
//   6: Group number
//   7-8: Transfer Length (big-endian uint16)
//   9: Control
func handleRead10(cdb []byte, dev BlockDevice) ([]byte, error) {
	if len(cdb) < 10 {
		return nil, fmt.Errorf("READ(10) CDB too short: %d bytes", len(cdb))
	}
	lba := uint64(binary.BigEndian.Uint32(cdb[2:6]))
	count := uint32(binary.BigEndian.Uint16(cdb[7:9]))
	if count == 0 {
		return []byte{}, nil
	}
	return dev.Read(lba, count)
}

// handleRead16 processes a READ(16) CDB.
// CDB layout (16 bytes):
//   0: opcode (0x88)
//   1: flags
//   2-9: LBA (big-endian uint64)
//   10-13: Transfer Length (big-endian uint32)
//   14: Group number
//   15: Control
func handleRead16(cdb []byte, dev BlockDevice) ([]byte, error) {
	if len(cdb) < 16 {
		return nil, fmt.Errorf("READ(16) CDB too short: %d bytes", len(cdb))
	}
	lba := binary.BigEndian.Uint64(cdb[2:10])
	count := binary.BigEndian.Uint32(cdb[10:14])
	if count == 0 {
		return []byte{}, nil
	}
	return dev.Read(lba, count)
}
