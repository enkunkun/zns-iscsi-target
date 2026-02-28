// Package journal implements the Write-Ahead Journal for the ZTL.
package journal

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"time"

	"github.com/enkunkun/zns-iscsi-target/internal/ztl"
)

// WAL record magic number.
const RecordMagic = uint32(0xADA50001)

// Record types.
const (
	RecordTypeL2PUpdate  = uint8(0x01)
	RecordTypeZoneOpen   = uint8(0x02)
	RecordTypeZoneClose  = uint8(0x03)
	RecordTypeZoneReset  = uint8(0x04)
	RecordTypeCheckpoint = uint8(0xFF)
)

// baseRecordSize is the size of the fixed fields in a WAL record (excluding checkpoint payload).
// Offset  Size  Field
// 0       4     Magic
// 4       4     CRC32C (over bytes 8..end of record)
// 8       1     RecordType
// 9       8     LSN
// 17      8     SegmentID / ZoneID
// 25      8     OldPhysAddr
// 33      8     NewPhysAddr
// 41      8     Timestamp (Unix nanoseconds)
// Total: 49 bytes
const baseRecordSize = 49

// crc32cTable is the CRC32C (Castagnoli) polynomial table.
var crc32cTable = crc32.MakeTable(crc32.Castagnoli)

// Record represents a single WAL record.
type Record struct {
	Type        uint8
	LSN         uint64
	SegmentID   uint64 // SegmentID for L2PUpdate, ZoneID for zone operations
	OldPhysAddr ztl.PhysAddr
	NewPhysAddr ztl.PhysAddr
	Timestamp   int64 // Unix nanoseconds
}

// MarshalBinary encodes the record to binary format.
func (r *Record) MarshalBinary() ([]byte, error) {
	buf := make([]byte, baseRecordSize)
	binary.BigEndian.PutUint32(buf[0:4], RecordMagic)
	// bytes 4-7: CRC32C (filled after encoding the rest)
	buf[8] = r.Type
	binary.BigEndian.PutUint64(buf[9:17], r.LSN)
	binary.BigEndian.PutUint64(buf[17:25], r.SegmentID)
	binary.BigEndian.PutUint64(buf[25:33], uint64(r.OldPhysAddr))
	binary.BigEndian.PutUint64(buf[33:41], uint64(r.NewPhysAddr))
	binary.BigEndian.PutUint64(buf[41:49], uint64(r.Timestamp))

	// Compute CRC32C over bytes 8..48
	crc := crc32.Checksum(buf[8:], crc32cTable)
	binary.BigEndian.PutUint32(buf[4:8], crc)

	return buf, nil
}

// UnmarshalBinary decodes a record from binary format.
// Returns an error if the magic or CRC32C is invalid.
func (r *Record) UnmarshalBinary(data []byte) error {
	if len(data) < baseRecordSize {
		return fmt.Errorf("record too short: %d < %d", len(data), baseRecordSize)
	}

	magic := binary.BigEndian.Uint32(data[0:4])
	if magic != RecordMagic {
		return fmt.Errorf("invalid magic: 0x%08X (expected 0x%08X)", magic, RecordMagic)
	}

	storedCRC := binary.BigEndian.Uint32(data[4:8])
	computedCRC := crc32.Checksum(data[8:baseRecordSize], crc32cTable)
	if storedCRC != computedCRC {
		return fmt.Errorf("CRC32C mismatch: stored=0x%08X computed=0x%08X", storedCRC, computedCRC)
	}

	r.Type = data[8]
	r.LSN = binary.BigEndian.Uint64(data[9:17])
	r.SegmentID = binary.BigEndian.Uint64(data[17:25])
	r.OldPhysAddr = ztl.PhysAddr(binary.BigEndian.Uint64(data[25:33]))
	r.NewPhysAddr = ztl.PhysAddr(binary.BigEndian.Uint64(data[33:41]))
	r.Timestamp = int64(binary.BigEndian.Uint64(data[41:49]))

	return nil
}

// newRecord creates a base record with the given type and timestamps.
func newRecord(recordType uint8, lsn uint64) *Record {
	return &Record{
		Type:      recordType,
		LSN:       lsn,
		Timestamp: time.Now().UnixNano(),
	}
}

// CheckpointRecord is a special record with an embedded L2P snapshot.
type CheckpointRecord struct {
	Record
	L2PTableLen uint64
	L2PEntries  []ztl.PhysAddr
}

// MarshalBinary encodes the checkpoint record, including the L2P snapshot.
func (c *CheckpointRecord) MarshalBinary() ([]byte, error) {
	// Base record encodes the L2PTableLen in the SegmentID field for simplicity
	c.Record.SegmentID = c.L2PTableLen

	base, err := c.Record.MarshalBinary()
	if err != nil {
		return nil, err
	}

	// Append L2P entries (8 bytes each)
	entryBuf := make([]byte, len(c.L2PEntries)*8)
	for i, entry := range c.L2PEntries {
		binary.BigEndian.PutUint64(entryBuf[i*8:], uint64(entry))
	}

	return append(base, entryBuf...), nil
}

// UnmarshalBinary decodes a checkpoint record.
func (c *CheckpointRecord) UnmarshalBinary(data []byte) error {
	if err := c.Record.UnmarshalBinary(data); err != nil {
		return err
	}

	c.L2PTableLen = c.Record.SegmentID
	expectedLen := baseRecordSize + int(c.L2PTableLen)*8
	if len(data) < expectedLen {
		return fmt.Errorf("checkpoint record too short: %d < %d", len(data), expectedLen)
	}

	c.L2PEntries = make([]ztl.PhysAddr, c.L2PTableLen)
	offset := baseRecordSize
	for i := uint64(0); i < c.L2PTableLen; i++ {
		c.L2PEntries[i] = ztl.PhysAddr(binary.BigEndian.Uint64(data[offset:]))
		offset += 8
	}

	return nil
}
