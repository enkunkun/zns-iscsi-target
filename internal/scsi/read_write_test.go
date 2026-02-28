package scsi

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockDevice is a simple in-memory block device for testing.
type mockDevice struct {
	data      []byte
	blockSize uint32
	capacity  uint64
	flushed   bool
	unmapped  [][2]uint64 // [lba, count] pairs
}

func newMockDevice(sectors uint64) *mockDevice {
	return &mockDevice{
		data:      make([]byte, sectors*512),
		blockSize: 512,
		capacity:  sectors,
	}
}

func (m *mockDevice) Read(lba uint64, count uint32) ([]byte, error) {
	start := lba * 512
	end := start + uint64(count)*512
	result := make([]byte, end-start)
	copy(result, m.data[start:end])
	return result, nil
}

func (m *mockDevice) Write(lba uint64, data []byte) error {
	start := lba * 512
	copy(m.data[start:], data)
	return nil
}

func (m *mockDevice) Flush() error {
	m.flushed = true
	return nil
}

func (m *mockDevice) Unmap(lba uint64, count uint32) error {
	m.unmapped = append(m.unmapped, [2]uint64{lba, uint64(count)})
	start := lba * 512
	end := start + uint64(count)*512
	for i := start; i < end && i < uint64(len(m.data)); i++ {
		m.data[i] = 0
	}
	return nil
}

func (m *mockDevice) BlockSize() uint32  { return m.blockSize }
func (m *mockDevice) Capacity() uint64   { return m.capacity }

// --- READ(6) tests ---

func TestHandleRead6(t *testing.T) {
	dev := newMockDevice(256)
	// Write some data at sector 5
	copy(dev.data[5*512:], bytes.Repeat([]byte{0xAB}, 512))

	cdb := make([]byte, 6)
	cdb[0] = OpcodeRead6
	// LBA 5 in bytes 1-3 (bits 4-0 of byte 1 + bytes 2-3)
	cdb[1] = 0x00
	cdb[2] = 0x00
	cdb[3] = 0x05
	cdb[4] = 0x01 // count = 1

	data, err := handleRead6(cdb, dev)
	require.NoError(t, err)
	require.Len(t, data, 512)
	assert.Equal(t, byte(0xAB), data[0])
}

func TestHandleRead6DefaultCount(t *testing.T) {
	dev := newMockDevice(300)
	cdb := make([]byte, 6)
	cdb[0] = OpcodeRead6
	cdb[4] = 0x00 // count = 0 means 256

	data, err := handleRead6(cdb, dev)
	require.NoError(t, err)
	assert.Len(t, data, 256*512)
}

func TestHandleRead6ShortCDB(t *testing.T) {
	dev := newMockDevice(10)
	_, err := handleRead6([]byte{0x08}, dev)
	assert.Error(t, err)
}

// --- READ(10) tests ---

func TestHandleRead10(t *testing.T) {
	dev := newMockDevice(256)
	// Write pattern at sector 10
	copy(dev.data[10*512:], bytes.Repeat([]byte{0xCD}, 2*512))

	cdb := make([]byte, 10)
	cdb[0] = OpcodeRead10
	binary.BigEndian.PutUint32(cdb[2:6], 10)  // LBA=10
	binary.BigEndian.PutUint16(cdb[7:9], 2)   // count=2

	data, err := handleRead10(cdb, dev)
	require.NoError(t, err)
	require.Len(t, data, 2*512)
	assert.Equal(t, byte(0xCD), data[0])
	assert.Equal(t, byte(0xCD), data[512])
}

func TestHandleRead10ZeroCount(t *testing.T) {
	dev := newMockDevice(10)
	cdb := make([]byte, 10)
	cdb[0] = OpcodeRead10
	// count = 0

	data, err := handleRead10(cdb, dev)
	require.NoError(t, err)
	assert.Len(t, data, 0)
}

// --- READ(16) tests ---

func TestHandleRead16(t *testing.T) {
	dev := newMockDevice(256)
	copy(dev.data[100*512:], bytes.Repeat([]byte{0xEF}, 512))

	cdb := make([]byte, 16)
	cdb[0] = OpcodeRead16
	binary.BigEndian.PutUint64(cdb[2:10], 100) // LBA=100
	binary.BigEndian.PutUint32(cdb[10:14], 1)  // count=1

	data, err := handleRead16(cdb, dev)
	require.NoError(t, err)
	require.Len(t, data, 512)
	assert.Equal(t, byte(0xEF), data[0])
}

// --- WRITE(6) tests ---

func TestHandleWrite6(t *testing.T) {
	dev := newMockDevice(256)
	cdb := make([]byte, 6)
	cdb[0] = OpcodeWrite6
	cdb[3] = 0x05 // LBA=5
	cdb[4] = 0x01 // count=1

	writeData := bytes.Repeat([]byte{0x55}, 512)
	err := handleWrite6(cdb, writeData, dev)
	require.NoError(t, err)
	assert.Equal(t, byte(0x55), dev.data[5*512])
}

func TestHandleWrite6DefaultCount(t *testing.T) {
	dev := newMockDevice(300)
	cdb := make([]byte, 6)
	cdb[0] = OpcodeWrite6
	cdb[4] = 0x00 // 0 = 256 sectors

	writeData := bytes.Repeat([]byte{0xAA}, 256*512)
	err := handleWrite6(cdb, writeData, dev)
	require.NoError(t, err)
}

func TestHandleWrite6DataTooShort(t *testing.T) {
	dev := newMockDevice(10)
	cdb := make([]byte, 6)
	cdb[0] = OpcodeWrite6
	cdb[4] = 0x02 // count=2

	err := handleWrite6(cdb, make([]byte, 512), dev) // only 1 sector
	assert.Error(t, err)
}

// --- WRITE(10) tests ---

func TestHandleWrite10(t *testing.T) {
	dev := newMockDevice(256)
	cdb := make([]byte, 10)
	cdb[0] = OpcodeWrite10
	binary.BigEndian.PutUint32(cdb[2:6], 20)  // LBA=20
	binary.BigEndian.PutUint16(cdb[7:9], 2)   // count=2

	writeData := bytes.Repeat([]byte{0x77}, 2*512)
	err := handleWrite10(cdb, writeData, dev)
	require.NoError(t, err)
	assert.Equal(t, byte(0x77), dev.data[20*512])
	assert.Equal(t, byte(0x77), dev.data[21*512])
}

func TestHandleWrite10ZeroCount(t *testing.T) {
	dev := newMockDevice(10)
	cdb := make([]byte, 10)
	cdb[0] = OpcodeWrite10
	// count=0 -> no-op

	err := handleWrite10(cdb, []byte{}, dev)
	require.NoError(t, err)
}

// --- WRITE(16) tests ---

func TestHandleWrite16(t *testing.T) {
	dev := newMockDevice(256)
	cdb := make([]byte, 16)
	cdb[0] = OpcodeWrite16
	binary.BigEndian.PutUint64(cdb[2:10], 50)  // LBA=50
	binary.BigEndian.PutUint32(cdb[10:14], 1)  // count=1

	writeData := bytes.Repeat([]byte{0x99}, 512)
	err := handleWrite16(cdb, writeData, dev)
	require.NoError(t, err)
	assert.Equal(t, byte(0x99), dev.data[50*512])
}

// --- Write then Read roundtrip ---

func TestWriteReadRoundtrip(t *testing.T) {
	dev := newMockDevice(256)

	// WRITE(10) at LBA 30
	writeCDB := make([]byte, 10)
	writeCDB[0] = OpcodeWrite10
	binary.BigEndian.PutUint32(writeCDB[2:6], 30)
	binary.BigEndian.PutUint16(writeCDB[7:9], 3)

	pattern := make([]byte, 3*512)
	for i := range pattern {
		pattern[i] = byte(i % 251)
	}
	err := handleWrite10(writeCDB, pattern, dev)
	require.NoError(t, err)

	// READ(10) at LBA 30
	readCDB := make([]byte, 10)
	readCDB[0] = OpcodeRead10
	binary.BigEndian.PutUint32(readCDB[2:6], 30)
	binary.BigEndian.PutUint16(readCDB[7:9], 3)

	data, err := handleRead10(readCDB, dev)
	require.NoError(t, err)
	assert.Equal(t, pattern, data)
}
