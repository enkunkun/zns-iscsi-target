package ztl

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEncodeDecodePhysAddr(t *testing.T) {
	tests := []struct {
		zoneID        uint32
		offsetSectors uint64
	}{
		{0, 0},
		{1, 0},
		{0, 1},
		{1, 1000},
		{100, 524288},
		{0xFFFFFF, offsetMask}, // max values
		{0, 524288},
	}

	for _, tt := range tests {
		pa := EncodePhysAddr(tt.zoneID, tt.offsetSectors)
		assert.Equal(t, tt.zoneID, pa.ZoneID(), "ZoneID mismatch for zone=%d offset=%d", tt.zoneID, tt.offsetSectors)
		assert.Equal(t, tt.offsetSectors, pa.OffsetSectors(), "Offset mismatch for zone=%d offset=%d", tt.zoneID, tt.offsetSectors)
	}
}

func TestPhysAddrIsUnmapped(t *testing.T) {
	assert.True(t, PhysAddr(0).IsUnmapped())
	assert.False(t, PhysAddr(1).IsUnmapped())
	assert.False(t, EncodePhysAddr(1, 0).IsUnmapped())
}

func TestMaxZoneID(t *testing.T) {
	maxZoneID := uint32((1 << zoneIDBits) - 1) // 16M - 1
	pa := EncodePhysAddr(maxZoneID, 0)
	assert.Equal(t, maxZoneID, pa.ZoneID())
}

func TestMaxOffsetSectors(t *testing.T) {
	max := offsetMask
	pa := EncodePhysAddr(0, max)
	assert.Equal(t, max, pa.OffsetSectors())
}

func TestLBAToSegmentID(t *testing.T) {
	const sectorsPerSegment = 16 // 8KB segments

	tests := []struct {
		lba      uint64
		expected SegmentID
	}{
		{0, 0},
		{15, 0},
		{16, 1},
		{31, 1},
		{32, 2},
		{256, 16},
	}

	for _, tt := range tests {
		got := LBAToSegmentID(tt.lba, sectorsPerSegment)
		assert.Equal(t, tt.expected, got, "LBA=%d", tt.lba)
	}
}

func TestSegmentToLBARange(t *testing.T) {
	const sectorsPerSegment = 16

	start, end := SegmentToLBARange(0, sectorsPerSegment)
	assert.Equal(t, uint64(0), start)
	assert.Equal(t, uint64(16), end)

	start, end = SegmentToLBARange(5, sectorsPerSegment)
	assert.Equal(t, uint64(80), start)
	assert.Equal(t, uint64(96), end)
}

func TestSegmentOffsetSectors(t *testing.T) {
	const sectorsPerSegment = 16

	assert.Equal(t, uint64(0), SegmentOffsetSectors(0, sectorsPerSegment))
	assert.Equal(t, uint64(5), SegmentOffsetSectors(5, sectorsPerSegment))
	assert.Equal(t, uint64(0), SegmentOffsetSectors(16, sectorsPerSegment))
	assert.Equal(t, uint64(3), SegmentOffsetSectors(19, sectorsPerSegment))
}

func TestPhysAddrNoOverlap(t *testing.T) {
	// Verify that zone ID bits and offset bits don't overlap
	maxOffset := PhysAddr(offsetMask)
	zoneOne := EncodePhysAddr(1, 0)
	// No bits in common
	assert.Equal(t, uint64(0), uint64(maxOffset)&uint64(zoneOne))
}

func TestPhysAddrMaxValues(t *testing.T) {
	// Should not overflow
	maxZone := uint32(math.MaxUint32 & zoneIDMask) // 24-bit max
	maxOffset := offsetMask

	pa := EncodePhysAddr(maxZone, maxOffset)
	assert.Equal(t, maxZone, pa.ZoneID())
	assert.Equal(t, maxOffset, pa.OffsetSectors())
}
