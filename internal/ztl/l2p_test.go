package ztl

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestL2PTableBasic(t *testing.T) {
	table := NewL2PTable(100)
	assert.Equal(t, uint64(100), table.Size())

	// All entries start unmapped
	assert.True(t, table.Get(0).IsUnmapped())
	assert.True(t, table.Get(99).IsUnmapped())
}

func TestL2PTableSetGet(t *testing.T) {
	table := NewL2PTable(100)

	phys := EncodePhysAddr(5, 1000)
	require.NoError(t, table.Set(10, phys))
	got := table.Get(10)
	assert.Equal(t, phys, got)
	assert.Equal(t, uint32(5), got.ZoneID())
	assert.Equal(t, uint64(1000), got.OffsetSectors())
}

func TestL2PTableOutOfRange(t *testing.T) {
	table := NewL2PTable(10)

	// Out of range
	assert.True(t, table.Get(10).IsUnmapped())
	assert.True(t, table.Get(100).IsUnmapped())

	err := table.Set(10, EncodePhysAddr(1, 0))
	assert.Error(t, err)

	// CAS on out-of-range returns false
	assert.False(t, table.CAS(10, 0, EncodePhysAddr(1, 0)))
}

func TestL2PTableCAS(t *testing.T) {
	table := NewL2PTable(10)

	phys1 := EncodePhysAddr(1, 0)
	phys2 := EncodePhysAddr(2, 0)

	// CAS on unmapped entry
	ok := table.CAS(0, PhysAddr(0), phys1)
	assert.True(t, ok)
	assert.Equal(t, phys1, table.Get(0))

	// CAS with wrong old value should fail
	ok = table.CAS(0, phys2, phys2)
	assert.False(t, ok)
	assert.Equal(t, phys1, table.Get(0)) // unchanged

	// CAS with correct old value
	ok = table.CAS(0, phys1, phys2)
	assert.True(t, ok)
	assert.Equal(t, phys2, table.Get(0))
}

func TestL2PTableSnapshot(t *testing.T) {
	table := NewL2PTable(5)

	phys := []PhysAddr{
		EncodePhysAddr(0, 0),
		EncodePhysAddr(1, 100),
		EncodePhysAddr(2, 200),
		EncodePhysAddr(3, 300),
		EncodePhysAddr(4, 400),
	}

	for i, p := range phys {
		require.NoError(t, table.Set(uint64(i), p))
	}

	snap := table.Snapshot()
	assert.Len(t, snap, 5)
	for i, p := range phys {
		assert.Equal(t, p, snap[i])
	}
}

func TestL2PTableLoadSnapshot(t *testing.T) {
	table := NewL2PTable(5)

	snap := []PhysAddr{
		EncodePhysAddr(10, 0),
		EncodePhysAddr(11, 50),
		EncodePhysAddr(12, 100),
		EncodePhysAddr(13, 150),
		EncodePhysAddr(14, 200),
	}

	require.NoError(t, table.LoadSnapshot(snap))

	for i, expected := range snap {
		got := table.Get(uint64(i))
		assert.Equal(t, expected, got, "segment %d", i)
	}
}

func TestL2PTableLoadSnapshotSizeMismatch(t *testing.T) {
	table := NewL2PTable(5)
	err := table.LoadSnapshot(make([]PhysAddr, 10))
	assert.Error(t, err)
}

func TestL2PTableConcurrentReads(t *testing.T) {
	const size = 1000
	table := NewL2PTable(size)

	// Pre-populate with known values
	for i := uint64(0); i < size; i++ {
		require.NoError(t, table.Set(i, EncodePhysAddr(uint32(i%100), i*16)))
	}

	var wg sync.WaitGroup
	const goroutines = 50

	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := uint64(0); i < size; i++ {
				// Just perform the load - verify it doesn't crash/race
				// The value could be the original or the updated value since
				// concurrent writes are happening; just check it's a valid PhysAddr
				_ = table.Get(i)
			}
		}()
	}

	// Concurrent writer
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := uint64(0); i < size; i++ {
			_ = table.Set(i, EncodePhysAddr(uint32(i%50), i*8))
		}
	}()

	wg.Wait()
	// After all goroutines, verify the table size is still correct
	assert.Equal(t, uint64(size), table.Size())
}

func TestL2PTableCASRaceSimulation(t *testing.T) {
	// Simulate GC vs foreground write race
	table := NewL2PTable(10)

	// Foreground write: set segment 0 to zone 1
	phys1 := EncodePhysAddr(1, 0)
	require.NoError(t, table.Set(0, phys1))

	// GC reads old value, prepares migration
	oldPhys := table.Get(0)
	assert.Equal(t, phys1, oldPhys)

	// Foreground write updates segment 0 AGAIN (concurrent)
	phys2 := EncodePhysAddr(2, 100)
	require.NoError(t, table.Set(0, phys2))

	// GC tries to CAS with stale old value - should FAIL
	newPhys := EncodePhysAddr(3, 200)
	ok := table.CAS(0, oldPhys, newPhys)
	assert.False(t, ok, "GC CAS should fail because foreground write changed the value")

	// Segment should still have foreground's value
	assert.Equal(t, phys2, table.Get(0))
}
