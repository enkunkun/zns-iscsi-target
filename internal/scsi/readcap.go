package scsi

import "encoding/binary"

// READ CAPACITY opcodes.
const (
	OpcodeReadCapacity10 byte = 0x25
	OpcodeReadCapacity16 byte = 0x9E // service action 0x10
	ServiceActionReadCap16 byte = 0x10
)

// handleReadCapacity10 builds the READ CAPACITY (10) response.
// Returns 8 bytes: returned LBA (4 bytes) + block size (4 bytes).
// Both fields are big-endian.
func handleReadCapacity10(capacity uint64, blockSize uint32) []byte {
	buf := make([]byte, 8)
	// Returned LBA = last addressable LBA (capacity - 1), capped at 0xFFFFFFFF for RC10
	lastLBA := capacity - 1
	if lastLBA > 0xFFFFFFFF {
		lastLBA = 0xFFFFFFFF
	}
	binary.BigEndian.PutUint32(buf[0:4], uint32(lastLBA))
	binary.BigEndian.PutUint32(buf[4:8], blockSize)
	return buf
}

// handleReadCapacity16 builds the READ CAPACITY (16) response.
// Returns 32 bytes: returned LBA (8 bytes) + block size (4 bytes) + additional fields.
// All multi-byte fields are big-endian.
func handleReadCapacity16(capacity uint64, blockSize uint32) []byte {
	buf := make([]byte, 32)
	lastLBA := capacity - 1
	binary.BigEndian.PutUint64(buf[0:8], lastLBA)
	binary.BigEndian.PutUint32(buf[8:12], blockSize)
	// buf[12]: Protection Type / P_I_Exponent / LBPPBE = 0 (no protection)
	// buf[13]: LBPME=0, LBPRZ=0, LOWEST ALIGNED LBA = 0
	// bytes 14-31: reserved / 0
	return buf
}
