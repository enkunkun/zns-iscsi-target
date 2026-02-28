package ztl

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestP2LMapSetGet(t *testing.T) {
	m := NewP2LMap()

	phys := EncodePhysAddr(5, 1000)
	m.Set(phys, 42)

	got, ok := m.Get(phys)
	assert.True(t, ok)
	assert.Equal(t, SegmentID(42), got)
}

func TestP2LMapMiss(t *testing.T) {
	m := NewP2LMap()

	phys := EncodePhysAddr(1, 0)
	_, ok := m.Get(phys)
	assert.False(t, ok)
}

func TestP2LMapDelete(t *testing.T) {
	m := NewP2LMap()

	phys := EncodePhysAddr(1, 500)
	m.Set(phys, 99)

	_, ok := m.Get(phys)
	assert.True(t, ok)

	m.Delete(phys)

	_, ok = m.Get(phys)
	assert.False(t, ok)
}

func TestP2LMapIterateZone(t *testing.T) {
	m := NewP2LMap()

	// Add entries in zone 3
	m.Set(EncodePhysAddr(3, 0), 10)
	m.Set(EncodePhysAddr(3, 16), 20)
	m.Set(EncodePhysAddr(3, 32), 30)

	// Add entries in zone 5 (should not appear in zone 3 iteration)
	m.Set(EncodePhysAddr(5, 0), 40)

	var offsets []uint64
	var segIDs []SegmentID

	m.IterateZone(3, func(offsetSectors uint64, segID SegmentID) {
		offsets = append(offsets, offsetSectors)
		segIDs = append(segIDs, segID)
	})

	assert.Len(t, offsets, 3)

	// Sort for deterministic comparison
	sort.Slice(segIDs, func(i, j int) bool { return segIDs[i] < segIDs[j] })
	assert.Equal(t, []SegmentID{10, 20, 30}, segIDs)
}

func TestP2LMapIterateZoneEmpty(t *testing.T) {
	m := NewP2LMap()
	m.Set(EncodePhysAddr(5, 0), 1)

	var count int
	m.IterateZone(3, func(_ uint64, _ SegmentID) {
		count++
	})
	assert.Equal(t, 0, count)
}

func TestP2LMapLen(t *testing.T) {
	m := NewP2LMap()
	assert.Equal(t, 0, m.Len())

	m.Set(EncodePhysAddr(1, 0), 1)
	m.Set(EncodePhysAddr(2, 0), 2)
	assert.Equal(t, 2, m.Len())

	m.Delete(EncodePhysAddr(1, 0))
	assert.Equal(t, 1, m.Len())
}

func TestP2LMapOverwrite(t *testing.T) {
	m := NewP2LMap()

	phys := EncodePhysAddr(1, 0)
	m.Set(phys, 100)
	m.Set(phys, 200) // overwrite

	got, ok := m.Get(phys)
	assert.True(t, ok)
	assert.Equal(t, SegmentID(200), got)
	assert.Equal(t, 1, m.Len()) // still just one entry
}
