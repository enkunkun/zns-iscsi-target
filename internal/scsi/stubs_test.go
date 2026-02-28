package scsi

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleTestUnitReady(t *testing.T) {
	data, status, sense := handleTestUnitReady()
	assert.Equal(t, StatusGood, status)
	assert.Nil(t, sense)
	assert.NotNil(t, data)
	assert.Len(t, data, 0)
}

func TestHandleReportLUNs(t *testing.T) {
	data := handleReportLUNs()
	require.NotNil(t, data)
	require.GreaterOrEqual(t, len(data), 16)

	// LUN list length must cover at least one 8-byte LUN entry
	lunListLen := binary.BigEndian.Uint32(data[0:4])
	assert.GreaterOrEqual(t, lunListLen, uint32(8))

	// First LUN entry starts at offset 8; LUN 0 is all zeros
	lun0 := data[8:16]
	for i, b := range lun0 {
		assert.Equal(t, byte(0), b, "LUN 0 byte %d should be 0", i)
	}
}

func TestHandleModeSense10(t *testing.T) {
	data := handleModeSense10()
	require.NotNil(t, data)
	require.GreaterOrEqual(t, len(data), 8)

	// Mode Data Length covers bytes after the first 2 bytes
	modeDataLen := binary.BigEndian.Uint16(data[0:2])
	assert.Equal(t, uint16(len(data)-2), modeDataLen)

	// Block Descriptor Length should be 0
	bdLen := binary.BigEndian.Uint16(data[6:8])
	assert.Equal(t, uint16(0), bdLen)
}

func TestHandleModeSense6(t *testing.T) {
	data := handleModeSense6()
	require.NotNil(t, data)
	require.GreaterOrEqual(t, len(data), 4)
	// Mode Data Length = total length - 1
	assert.Equal(t, byte(len(data)-1), data[0])
	// Block Descriptor Length = 0
	assert.Equal(t, byte(0), data[3])
}

func TestHandlePersistentReserveIn(t *testing.T) {
	data, status, sense := handlePersistentReserveIn()
	assert.Nil(t, data)
	assert.Equal(t, StatusCheckCondition, status)
	require.NotNil(t, sense)
	assert.Equal(t, SenseKeyIllegalRequest, sense[2]&0x0F)
}

func TestHandlePersistentReserveOut(t *testing.T) {
	data, status, sense := handlePersistentReserveOut()
	assert.Nil(t, data)
	assert.Equal(t, StatusCheckCondition, status)
	require.NotNil(t, sense)
	assert.Equal(t, SenseKeyIllegalRequest, sense[2]&0x0F)
}

func TestHandleUnmap(t *testing.T) {
	dev := newMockDevice(256)

	// Write some data
	copy(dev.data[10*512:], make([]byte, 2*512))
	for i := 10 * 512; i < 12*512; i++ {
		dev.data[i] = 0xFF
	}

	// Build UNMAP parameter list: header (8 bytes) + one descriptor (16 bytes)
	paramList := make([]byte, 24)
	// UNMAP Data Length (bytes 0-1) = total - 2 = 22
	binary.BigEndian.PutUint16(paramList[0:2], 22)
	// UNMAP Block Descriptor Data Length (bytes 2-3) = 16
	binary.BigEndian.PutUint16(paramList[2:4], 16)
	// bytes 4-7: reserved
	// Descriptor 0: LBA=10, count=2
	binary.BigEndian.PutUint64(paramList[8:16], 10)  // LBA
	binary.BigEndian.PutUint32(paramList[16:20], 2) // count

	cdb := make([]byte, 10)
	cdb[0] = OpcodeUnmap

	err := handleUnmap(cdb, paramList, dev)
	require.NoError(t, err)

	assert.Len(t, dev.unmapped, 1)
	assert.Equal(t, uint64(10), dev.unmapped[0][0])
	assert.Equal(t, uint64(2), dev.unmapped[0][1])
}

func TestHandleUnmapEmptyParamList(t *testing.T) {
	dev := newMockDevice(10)
	cdb := make([]byte, 10)
	cdb[0] = OpcodeUnmap

	err := handleUnmap(cdb, []byte{}, dev)
	require.NoError(t, err)
	assert.Len(t, dev.unmapped, 0)
}

func TestHandleSyncCache10(t *testing.T) {
	dev := newMockDevice(10)
	cdb := make([]byte, 10)
	cdb[0] = OpcodeSyncCache10

	err := handleSyncCache10(cdb, dev)
	require.NoError(t, err)
	assert.True(t, dev.flushed)
}

func TestHandleRequestSense(t *testing.T) {
	data := handleRequestSense()
	require.Len(t, data, 18)
	assert.Equal(t, byte(0x70), data[0])
	assert.Equal(t, SenseKeyNoSense, data[2]&0x0F)
}

func TestHandleStartStopUnit(t *testing.T) {
	data, status, sense := handleStartStopUnit()
	assert.Equal(t, StatusGood, status)
	assert.Nil(t, sense)
	assert.Len(t, data, 0)
}
