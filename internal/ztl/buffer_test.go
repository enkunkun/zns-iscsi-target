package ztl

import (
	"testing"

	"github.com/enkunkun/zns-iscsi-target/internal/backend/emulator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestWriteBuffer(t *testing.T) *WriteBuffer {
	t.Helper()
	return NewWriteBuffer(2048, 5) // 1MB zone size, 5s flush age
}

func TestWriteBufferAdd(t *testing.T) {
	wb := newTestWriteBuffer(t)

	data := make([]byte, 512*4)
	for i := range data {
		data[i] = 0xAB
	}

	err := wb.Add(0, data, 0)
	require.NoError(t, err)
	assert.Equal(t, int64(512*4), wb.DirtyBytes())
	assert.Equal(t, 1, wb.ZoneCount())
}

func TestWriteBufferLookupHit(t *testing.T) {
	wb := newTestWriteBuffer(t)

	// Buffer starts at LBA 0, contains 4 sectors
	data := make([]byte, 512*4)
	for i := range data {
		data[i] = byte(i % 256)
	}
	require.NoError(t, wb.Add(0, data, 0))

	// Lookup sector 0, count 4
	result, ok := wb.Lookup(0, 0, 4)
	assert.True(t, ok)
	assert.Equal(t, data, result)
}

func TestWriteBufferLookupPartialMiss(t *testing.T) {
	wb := newTestWriteBuffer(t)

	// Buffer has sectors 5-8
	data := make([]byte, 512*4)
	require.NoError(t, wb.Add(0, data, 5))

	// Request for sectors 4-7 (partial overlap - not fully covered)
	_, ok := wb.Lookup(0, 4, 4)
	assert.False(t, ok)
}

func TestWriteBufferLookupMiss(t *testing.T) {
	wb := newTestWriteBuffer(t)

	// No data in zone 0
	_, ok := wb.Lookup(0, 0, 1)
	assert.False(t, ok)

	// Zone exists but different LBA range
	data := make([]byte, 512)
	require.NoError(t, wb.Add(0, data, 10))
	_, ok = wb.Lookup(0, 0, 1)
	assert.False(t, ok)
}

func TestWriteBufferLookupWrongZone(t *testing.T) {
	wb := newTestWriteBuffer(t)

	data := make([]byte, 512)
	require.NoError(t, wb.Add(1, data, 0))

	_, ok := wb.Lookup(0, 0, 1) // zone 0 not buffered
	assert.False(t, ok)
}

func TestWriteBufferFlushWritesToDevice(t *testing.T) {
	dev, err := emulator.New(emulator.Config{
		ZoneCount:    4,
		ZoneSizeMB:   1,
		MaxOpenZones: 3,
	})
	require.NoError(t, err)

	zm := NewZoneManager(3, dev)
	require.NoError(t, zm.Initialize())

	wb := newTestWriteBuffer(t)

	// Allocate zone 0
	zoneID, err := zm.AllocateFree()
	require.NoError(t, err)

	data := make([]byte, 512*4)
	for i := range data {
		data[i] = byte(i)
	}

	// Get zone start LBA
	info, ok := zm.ZoneInfo(zoneID)
	require.True(t, ok)

	require.NoError(t, wb.Add(zoneID, data, info.StartLBA))

	// Flush to device
	require.NoError(t, wb.Flush(zoneID, dev, zm))

	// Buffer should be empty after flush
	assert.Equal(t, int64(0), wb.DirtyBytes())
	assert.Equal(t, 0, wb.ZoneCount())

	// Lookup should miss (data is on device now)
	_, ok = wb.Lookup(zoneID, info.StartLBA, 4)
	assert.False(t, ok)

	// Data should be on device
	readData, err := dev.ReadSectors(info.StartLBA, 4)
	require.NoError(t, err)
	assert.Equal(t, data, readData)
}

func TestWriteBufferFlushAll(t *testing.T) {
	dev, err := emulator.New(emulator.Config{
		ZoneCount:    4,
		ZoneSizeMB:   1,
		MaxOpenZones: 3,
	})
	require.NoError(t, err)

	zm := NewZoneManager(3, dev)
	require.NoError(t, zm.Initialize())

	wb := newTestWriteBuffer(t)

	// Add data to two zones
	zone1, _ := zm.AllocateFree()
	zone2, _ := zm.AllocateFree()

	info1, _ := zm.ZoneInfo(zone1)
	info2, _ := zm.ZoneInfo(zone2)

	data1 := make([]byte, 512)
	data2 := make([]byte, 512)
	for i := range data1 {
		data1[i] = 0xAA
		data2[i] = 0xBB
	}

	require.NoError(t, wb.Add(zone1, data1, info1.StartLBA))
	require.NoError(t, wb.Add(zone2, data2, info2.StartLBA))

	assert.Equal(t, 2, wb.ZoneCount())

	// Flush all
	require.NoError(t, wb.FlushAll(dev, zm))
	assert.Equal(t, 0, wb.ZoneCount())
}

func TestWriteBufferAppend(t *testing.T) {
	wb := newTestWriteBuffer(t)

	// Multiple appends to same zone
	data1 := make([]byte, 512)
	data2 := make([]byte, 512)
	for i := range data1 {
		data1[i] = 0x11
		data2[i] = 0x22
	}

	require.NoError(t, wb.Add(0, data1, 100))
	require.NoError(t, wb.Add(0, data2, 100)) // second append after first

	assert.Equal(t, int64(1024), wb.DirtyBytes())

	// Lookup the second sector
	result, ok := wb.Lookup(0, 101, 1)
	assert.True(t, ok)
	assert.Equal(t, data2, result)
}
