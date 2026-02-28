package ztl

import (
	"testing"

	"github.com/enkunkun/zns-iscsi-target/internal/backend/emulator"
	"github.com/enkunkun/zns-iscsi-target/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Device.Emulator.ZoneCount = 8
	cfg.Device.Emulator.ZoneSizeMB = 1
	cfg.Device.Emulator.MaxOpenZones = 4
	cfg.ZTL.SegmentSizeKB = 4 // 8 sectors per segment
	cfg.ZTL.BufferFlushAgeSec = 300 // disable age-based flush
	cfg.ZTL.GCTriggerFreeRatio = 0.10
	cfg.ZTL.GCEmergencyFreeZones = 1
	return cfg
}

func newTestZTL(t *testing.T) (*ZTL, *emulator.Emulator) {
	t.Helper()
	cfg := newTestConfig()

	dev, err := emulator.New(emulator.Config{
		ZoneCount:    cfg.Device.Emulator.ZoneCount,
		ZoneSizeMB:   cfg.Device.Emulator.ZoneSizeMB,
		MaxOpenZones: cfg.Device.Emulator.MaxOpenZones,
	})
	require.NoError(t, err)

	ztl, err := New(cfg, dev, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = ztl.Close() })

	return ztl, dev
}

func TestZTLWriteRead(t *testing.T) {
	ztl, _ := newTestZTL(t)

	data := make([]byte, 512*8)
	for i := range data {
		data[i] = byte(i % 256)
	}

	err := ztl.Write(0, data)
	require.NoError(t, err)

	result, err := ztl.Read(0, 8)
	require.NoError(t, err)
	assert.Equal(t, data, result)
}

func TestZTLWriteReadUnmapped(t *testing.T) {
	ztl, _ := newTestZTL(t)

	// Read unmapped area - should return zeros
	result, err := ztl.Read(1000, 8)
	require.NoError(t, err)
	assert.Len(t, result, 8*512)
	for _, b := range result {
		assert.Equal(t, byte(0), b)
	}
}

func TestZTLOverwrite(t *testing.T) {
	ztl, _ := newTestZTL(t)

	data1 := make([]byte, 512*8)
	for i := range data1 {
		data1[i] = 0xAA
	}

	data2 := make([]byte, 512*8)
	for i := range data2 {
		data2[i] = 0xBB
	}

	// Write initial data
	require.NoError(t, ztl.Write(0, data1))

	// Overwrite
	require.NoError(t, ztl.Write(0, data2))

	// Read back should return overwritten data
	result, err := ztl.Read(0, 8)
	require.NoError(t, err)
	assert.Equal(t, data2, result)
}

func TestZTLCrossZoneWrite(t *testing.T) {
	ztl, _ := newTestZTL(t)

	// Write across zone boundary
	// Zone size is 1MB = 2048 sectors. Segment size is 4KB = 8 sectors.
	// Write starting near the end of zone 0 and crossing to zone 1
	const sectorsPerSeg = 8 // 4KB / 512
	// Fill most of zone 0 first
	zoneSize := int(ztl.zoneSectors)
	writeSize := (zoneSize - sectorsPerSeg) * 512 // fill all but last segment

	bigData := make([]byte, writeSize)
	for i := range bigData {
		bigData[i] = 0xCC
	}
	require.NoError(t, ztl.Write(0, bigData))

	// Now write 2 segments that cross zone boundary
	crossData := make([]byte, 2*sectorsPerSeg*512)
	for i := range crossData {
		crossData[i] = 0xDD
	}
	startLBA := uint64(zoneSize - sectorsPerSeg) // last segment of zone 0
	require.NoError(t, ztl.Write(startLBA, crossData))

	// Read back the cross-zone data
	result, err := ztl.Read(startLBA, uint32(2*sectorsPerSeg))
	require.NoError(t, err)
	assert.Equal(t, crossData, result)
}

func TestZTLUnmap(t *testing.T) {
	ztl, _ := newTestZTL(t)

	data := make([]byte, 512*8)
	for i := range data {
		data[i] = 0xEE
	}

	require.NoError(t, ztl.Write(0, data))

	// Verify data is readable
	result, err := ztl.Read(0, 8)
	require.NoError(t, err)
	assert.Equal(t, data, result)

	// Unmap the data
	require.NoError(t, ztl.Unmap(0, 8))

	// Reading unmapped data should return zeros
	result, err = ztl.Read(0, 8)
	require.NoError(t, err)
	for _, b := range result {
		assert.Equal(t, byte(0), b, "unmapped data should be zeros")
	}
}

func TestZTLFlush(t *testing.T) {
	ztl, dev := newTestZTL(t)

	data := make([]byte, 512*8)
	for i := range data {
		data[i] = byte(i)
	}

	require.NoError(t, ztl.Write(0, data))

	// Flush to device
	require.NoError(t, ztl.Flush())

	// Buffer should be empty
	assert.Equal(t, int64(0), ztl.buf.DirtyBytes())

	// Data should be on device
	// Find where the data was written via L2P
	segID := LBAToSegmentID(0, ztl.sectorsPerSeg)
	phys := ztl.l2p.Get(segID)
	require.False(t, phys.IsUnmapped())

	zoneInfo, ok := ztl.zm.ZoneInfo(phys.ZoneID())
	require.True(t, ok)
	deviceLBA := zoneInfo.StartLBA + phys.OffsetSectors()

	devData, err := dev.ReadSectors(deviceLBA, 8)
	require.NoError(t, err)
	assert.Equal(t, data, devData)
}

func TestZTLReadFromDevice(t *testing.T) {
	ztl, _ := newTestZTL(t)

	data := make([]byte, 512*8)
	for i := range data {
		data[i] = byte(i * 2)
	}

	require.NoError(t, ztl.Write(0, data))
	require.NoError(t, ztl.Flush())

	// Clear the buffer to force device read
	ztl.buf.mu.Lock()
	ztl.buf.buffers = make(map[uint32]*zoneBuffer)
	ztl.buf.mu.Unlock()

	// Read from device
	result, err := ztl.Read(0, 8)
	require.NoError(t, err)
	assert.Equal(t, data, result)
}

func TestZTLWriteAmplification(t *testing.T) {
	ztl, _ := newTestZTL(t)

	// Write 16 segments; they should all go to a single zone write zone
	const numSegs = 16
	const sectorsPerSeg = 8

	data := make([]byte, sectorsPerSeg*512)
	for i := 0; i < numSegs; i++ {
		for j := range data {
			data[j] = byte(i)
		}
		err := ztl.Write(uint64(i)*sectorsPerSeg, data)
		require.NoError(t, err, "segment %d", i)
	}

	// Verify all segments are mapped
	for i := 0; i < numSegs; i++ {
		phys := ztl.l2p.Get(SegmentID(i))
		assert.False(t, phys.IsUnmapped(), "segment %d should be mapped", i)
	}
}
