package scsi

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestHandler() *Handler {
	dev := newMockDevice(1024) // 1024 sectors = 512 KB
	return NewHandler(dev, "TESTSN001", [16]byte{0x12, 0x34, 0x56, 0x78})
}

func TestHandlerTestUnitReady(t *testing.T) {
	h := newTestHandler()
	cdb := []byte{OpcodeTestUnitReady, 0, 0, 0, 0, 0}
	data, status, sense, err := h.Execute(cdb, nil)
	require.NoError(t, err)
	assert.Equal(t, StatusGood, status)
	assert.Nil(t, sense)
	assert.Len(t, data, 0)
}

func TestHandlerInquiryStandard(t *testing.T) {
	h := newTestHandler()
	cdb := make([]byte, 6)
	cdb[0] = OpcodeInquiry
	data, status, sense, err := h.Execute(cdb, nil)
	require.NoError(t, err)
	assert.Equal(t, StatusGood, status)
	assert.Nil(t, sense)
	require.GreaterOrEqual(t, len(data), 36)
	assert.Equal(t, byte(0x00), data[0]) // disk type
	assert.Equal(t, byte(0x05), data[2]) // SPC-3
}

func TestHandlerInquiryVPD(t *testing.T) {
	h := newTestHandler()

	t.Run("VPD page 0x00", func(t *testing.T) {
		cdb := make([]byte, 6)
		cdb[0] = OpcodeInquiry
		cdb[1] = 0x01
		cdb[2] = VPDPageSupportedPages
		data, status, _, err := h.Execute(cdb, nil)
		require.NoError(t, err)
		assert.Equal(t, StatusGood, status)
		assert.NotEmpty(t, data)
	})

	t.Run("VPD page 0x80", func(t *testing.T) {
		cdb := make([]byte, 6)
		cdb[0] = OpcodeInquiry
		cdb[1] = 0x01
		cdb[2] = VPDPageUnitSerialNumber
		data, status, _, err := h.Execute(cdb, nil)
		require.NoError(t, err)
		assert.Equal(t, StatusGood, status)
		assert.NotEmpty(t, data)
	})

	t.Run("VPD page 0x83", func(t *testing.T) {
		cdb := make([]byte, 6)
		cdb[0] = OpcodeInquiry
		cdb[1] = 0x01
		cdb[2] = VPDPageDeviceIdentifiers
		data, status, _, err := h.Execute(cdb, nil)
		require.NoError(t, err)
		assert.Equal(t, StatusGood, status)
		assert.NotEmpty(t, data)
	})

	t.Run("unsupported VPD page returns CHECK CONDITION", func(t *testing.T) {
		cdb := make([]byte, 6)
		cdb[0] = OpcodeInquiry
		cdb[1] = 0x01
		cdb[2] = 0xFF // unsupported page
		_, status, sense, err := h.Execute(cdb, nil)
		require.NoError(t, err)
		assert.Equal(t, StatusCheckCondition, status)
		require.NotNil(t, sense)
		assert.Equal(t, SenseKeyIllegalRequest, sense[2]&0x0F)
	})
}

func TestHandlerReadCapacity10(t *testing.T) {
	h := newTestHandler()
	cdb := make([]byte, 10)
	cdb[0] = OpcodeReadCapacity10
	data, status, sense, err := h.Execute(cdb, nil)
	require.NoError(t, err)
	assert.Equal(t, StatusGood, status)
	assert.Nil(t, sense)
	require.Len(t, data, 8)

	lastLBA := binary.BigEndian.Uint32(data[0:4])
	blockSize := binary.BigEndian.Uint32(data[4:8])
	assert.Equal(t, uint32(1023), lastLBA)       // 1024 sectors - 1
	assert.Equal(t, uint32(512), blockSize)
}

func TestHandlerReadCapacity16(t *testing.T) {
	h := newTestHandler()
	cdb := make([]byte, 16)
	cdb[0] = OpcodeReadCapacity16
	cdb[1] = ServiceActionReadCap16
	data, status, sense, err := h.Execute(cdb, nil)
	require.NoError(t, err)
	assert.Equal(t, StatusGood, status)
	assert.Nil(t, sense)
	require.Len(t, data, 32)

	lastLBA := binary.BigEndian.Uint64(data[0:8])
	blockSize := binary.BigEndian.Uint32(data[8:12])
	assert.Equal(t, uint64(1023), lastLBA)
	assert.Equal(t, uint32(512), blockSize)
}

func TestHandlerReadCapacity16WrongServiceAction(t *testing.T) {
	h := newTestHandler()
	cdb := make([]byte, 16)
	cdb[0] = OpcodeReadCapacity16
	cdb[1] = 0x00 // wrong service action
	_, status, sense, err := h.Execute(cdb, nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCheckCondition, status)
	require.NotNil(t, sense)
}

func TestHandlerRead10(t *testing.T) {
	dev := newMockDevice(1024)
	copy(dev.data[50*512:], bytes.Repeat([]byte{0xBB}, 512))
	h := NewHandler(dev, "SN", [16]byte{})

	cdb := make([]byte, 10)
	cdb[0] = OpcodeRead10
	binary.BigEndian.PutUint32(cdb[2:6], 50)
	binary.BigEndian.PutUint16(cdb[7:9], 1)

	data, status, sense, err := h.Execute(cdb, nil)
	require.NoError(t, err)
	assert.Equal(t, StatusGood, status)
	assert.Nil(t, sense)
	require.Len(t, data, 512)
	assert.Equal(t, byte(0xBB), data[0])
}

func TestHandlerWrite10(t *testing.T) {
	dev := newMockDevice(1024)
	h := NewHandler(dev, "SN", [16]byte{})

	cdb := make([]byte, 10)
	cdb[0] = OpcodeWrite10
	binary.BigEndian.PutUint32(cdb[2:6], 100)
	binary.BigEndian.PutUint16(cdb[7:9], 1)

	writeData := bytes.Repeat([]byte{0xCC}, 512)
	data, status, sense, err := h.Execute(cdb, writeData)
	require.NoError(t, err)
	assert.Equal(t, StatusGood, status)
	assert.Nil(t, sense)
	assert.Len(t, data, 0)
	assert.Equal(t, byte(0xCC), dev.data[100*512])
}

func TestHandlerWrite10ThenRead10(t *testing.T) {
	dev := newMockDevice(1024)
	h := NewHandler(dev, "SN", [16]byte{})

	lba := uint32(200)
	count := uint16(2)
	pattern := make([]byte, int(count)*512)
	for i := range pattern {
		pattern[i] = byte(i % 127)
	}

	// Write
	writeCDB := make([]byte, 10)
	writeCDB[0] = OpcodeWrite10
	binary.BigEndian.PutUint32(writeCDB[2:6], lba)
	binary.BigEndian.PutUint16(writeCDB[7:9], count)
	_, wStatus, _, werr := h.Execute(writeCDB, pattern)
	require.NoError(t, werr)
	assert.Equal(t, StatusGood, wStatus)

	// Read
	readCDB := make([]byte, 10)
	readCDB[0] = OpcodeRead10
	binary.BigEndian.PutUint32(readCDB[2:6], lba)
	binary.BigEndian.PutUint16(readCDB[7:9], count)
	readData, rStatus, _, rerr := h.Execute(readCDB, nil)
	require.NoError(t, rerr)
	assert.Equal(t, StatusGood, rStatus)
	assert.Equal(t, pattern, readData)
}

func TestHandlerSyncCache(t *testing.T) {
	dev := newMockDevice(1024)
	h := NewHandler(dev, "SN", [16]byte{})

	cdb := make([]byte, 10)
	cdb[0] = OpcodeSyncCache10
	_, status, sense, err := h.Execute(cdb, nil)
	require.NoError(t, err)
	assert.Equal(t, StatusGood, status)
	assert.Nil(t, sense)
	assert.True(t, dev.flushed)
}

func TestHandlerReportLUNs(t *testing.T) {
	h := newTestHandler()
	cdb := make([]byte, 12)
	cdb[0] = OpcodeReportLUNs
	data, status, sense, err := h.Execute(cdb, nil)
	require.NoError(t, err)
	assert.Equal(t, StatusGood, status)
	assert.Nil(t, sense)
	require.GreaterOrEqual(t, len(data), 16)
}

func TestHandlerModeSense10(t *testing.T) {
	h := newTestHandler()
	cdb := make([]byte, 10)
	cdb[0] = OpcodeModeSense10
	data, status, sense, err := h.Execute(cdb, nil)
	require.NoError(t, err)
	assert.Equal(t, StatusGood, status)
	assert.Nil(t, sense)
	require.GreaterOrEqual(t, len(data), 8)
}

func TestHandlerModeSense6(t *testing.T) {
	h := newTestHandler()
	cdb := make([]byte, 6)
	cdb[0] = OpcodeModeSense6
	data, status, sense, err := h.Execute(cdb, nil)
	require.NoError(t, err)
	assert.Equal(t, StatusGood, status)
	assert.Nil(t, sense)
	require.GreaterOrEqual(t, len(data), 4)
}

func TestHandlerPersistentReserveIn(t *testing.T) {
	h := newTestHandler()
	cdb := make([]byte, 10)
	cdb[0] = OpcodePersistentReserveIn
	_, status, sense, err := h.Execute(cdb, nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCheckCondition, status)
	require.NotNil(t, sense)
	assert.Equal(t, SenseKeyIllegalRequest, sense[2]&0x0F)
}

func TestHandlerPersistentReserveOut(t *testing.T) {
	h := newTestHandler()
	cdb := make([]byte, 10)
	cdb[0] = OpcodePersistentReserveOut
	_, status, sense, err := h.Execute(cdb, nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCheckCondition, status)
	require.NotNil(t, sense)
}

func TestHandlerUnknownOpcode(t *testing.T) {
	h := newTestHandler()
	cdb := []byte{0xFE, 0, 0, 0, 0, 0} // unknown opcode
	data, status, sense, err := h.Execute(cdb, nil)
	require.NoError(t, err)
	assert.Nil(t, data)
	assert.Equal(t, StatusCheckCondition, status)
	require.NotNil(t, sense)
	assert.Equal(t, SenseKeyIllegalRequest, sense[2]&0x0F)
	assert.Equal(t, ASCInvalidCommandOpCode, sense[12])
}

func TestHandlerEmptyCDB(t *testing.T) {
	h := newTestHandler()
	_, status, sense, err := h.Execute([]byte{}, nil)
	require.NoError(t, err)
	assert.Equal(t, StatusCheckCondition, status)
	require.NotNil(t, sense)
}

func TestHandlerUnmapOpcode(t *testing.T) {
	dev := newMockDevice(1024)
	h := NewHandler(dev, "SN", [16]byte{})

	// Write data at LBA 10-11
	for i := 10 * 512; i < 12*512; i++ {
		dev.data[i] = 0xFF
	}

	// Build UNMAP parameter list
	paramList := make([]byte, 24)
	binary.BigEndian.PutUint16(paramList[0:2], 22)
	binary.BigEndian.PutUint16(paramList[2:4], 16)
	binary.BigEndian.PutUint64(paramList[8:16], 10)
	binary.BigEndian.PutUint32(paramList[16:20], 2)

	cdb := make([]byte, 10)
	cdb[0] = OpcodeUnmap

	_, status, sense, err := h.Execute(cdb, paramList)
	require.NoError(t, err)
	assert.Equal(t, StatusGood, status)
	assert.Nil(t, sense)
	assert.Len(t, dev.unmapped, 1)
}

func TestHandlerRead6AndWrite6(t *testing.T) {
	dev := newMockDevice(1024)
	h := NewHandler(dev, "SN", [16]byte{})

	// WRITE(6) at LBA 0, 1 sector
	writeCDB := make([]byte, 6)
	writeCDB[0] = OpcodeWrite6
	writeCDB[4] = 1
	writeData := bytes.Repeat([]byte{0xAA}, 512)
	_, wStatus, _, werr := h.Execute(writeCDB, writeData)
	require.NoError(t, werr)
	assert.Equal(t, StatusGood, wStatus)

	// READ(6) at LBA 0, 1 sector
	readCDB := make([]byte, 6)
	readCDB[0] = OpcodeRead6
	readCDB[4] = 1
	rData, rStatus, _, rerr := h.Execute(readCDB, nil)
	require.NoError(t, rerr)
	assert.Equal(t, StatusGood, rStatus)
	assert.Equal(t, writeData, rData)
}

func TestHandlerRead16AndWrite16(t *testing.T) {
	dev := newMockDevice(1024)
	h := NewHandler(dev, "SN", [16]byte{})

	lba := uint64(500)
	count := uint32(3)
	pattern := bytes.Repeat([]byte{0x33}, int(count)*512)

	// WRITE(16)
	writeCDB := make([]byte, 16)
	writeCDB[0] = OpcodeWrite16
	binary.BigEndian.PutUint64(writeCDB[2:10], lba)
	binary.BigEndian.PutUint32(writeCDB[10:14], count)
	_, wStatus, _, werr := h.Execute(writeCDB, pattern)
	require.NoError(t, werr)
	assert.Equal(t, StatusGood, wStatus)

	// READ(16)
	readCDB := make([]byte, 16)
	readCDB[0] = OpcodeRead16
	binary.BigEndian.PutUint64(readCDB[2:10], lba)
	binary.BigEndian.PutUint32(readCDB[10:14], count)
	rData, rStatus, _, rerr := h.Execute(readCDB, nil)
	require.NoError(t, rerr)
	assert.Equal(t, StatusGood, rStatus)
	assert.Equal(t, pattern, rData)
}
