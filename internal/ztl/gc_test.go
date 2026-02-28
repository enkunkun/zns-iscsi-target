package ztl

import (
	"context"
	"testing"
	"time"

	"github.com/enkunkun/zns-iscsi-target/internal/backend/emulator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockJournal is a no-op journal implementation for testing.
type mockJournal struct {
	l2pUpdates []struct{ segID uint64; old, new PhysAddr }
	zoneResets []uint32
}

func (j *mockJournal) LogL2PUpdate(segID uint64, oldPhys, newPhys PhysAddr) (uint64, error) {
	j.l2pUpdates = append(j.l2pUpdates, struct{ segID uint64; old, new PhysAddr }{segID, oldPhys, newPhys})
	return uint64(len(j.l2pUpdates)), nil
}

func (j *mockJournal) LogZoneOpen(zoneID uint32) error  { return nil }
func (j *mockJournal) LogZoneClose(zoneID uint32) error { return nil }
func (j *mockJournal) LogZoneReset(zoneID uint32) error {
	j.zoneResets = append(j.zoneResets, zoneID)
	return nil
}
func (j *mockJournal) GroupCommit() error { return nil }

func newGCTestSetup(t *testing.T) (*GCEngine, *L2PTable, *P2LMap, *ZoneManager, *emulator.Emulator) {
	t.Helper()

	dev, err := emulator.New(emulator.Config{
		ZoneCount:    8,
		ZoneSizeMB:   1, // 2048 sectors per zone
		MaxOpenZones: 4,
	})
	require.NoError(t, err)

	const sectorsPerSeg = 16 // 8KB segments

	totalSectors := dev.Capacity()
	totalSegs := totalSectors / sectorsPerSeg

	l2p := NewL2PTable(totalSegs)
	p2l := NewP2LMap()
	zm := NewZoneManager(4, dev)
	require.NoError(t, zm.Initialize())
	stats := &GCStats{}

	jrn := &mockJournal{}
	gc := NewGCEngine(l2p, p2l, zm, dev, jrn, stats, GCConfig{
		FreeTriggerRatio:   0.20,
		EmergencyFreeZones: 2,
		SectorsPerSegment:  sectorsPerSeg,
	})

	return gc, l2p, p2l, zm, dev
}

func TestGCEngineStartStop(t *testing.T) {
	gc, _, _, _, _ := newGCTestSetup(t)

	ctx, cancel := context.WithCancel(context.Background())
	gc.Start(ctx)

	// Wait a bit to ensure it starts
	time.Sleep(10 * time.Millisecond)

	cancel()
	gc.Stop()
}

func TestGCEngineManualTrigger(t *testing.T) {
	gc, _, _, _, _ := newGCTestSetup(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gc.Start(ctx)

	// Trigger manual GC (no victims, so it's a no-op)
	gc.TriggerManual()
	time.Sleep(50 * time.Millisecond)

	// Should not crash
	assert.Equal(t, int64(0), gc.stats.ZonesReclaimed.Load())
}

func TestGCEngineCollectZone(t *testing.T) {
	gc, l2p, p2l, zm, dev := newGCTestSetup(t)

	const sectorsPerSeg = 16

	// Fill zone 0 with "used" segments, some invalidated
	zone0ID, err := zm.AllocateFree()
	require.NoError(t, err)
	zone0Info, _ := zm.ZoneInfo(zone0ID)

	// Write 4 segments to zone 0
	numSegs := 4
	for i := 0; i < numSegs; i++ {
		data := make([]byte, 512*sectorsPerSeg)
		for j := range data {
			data[j] = byte(i)
		}
		writeLBA := zone0Info.StartLBA + uint64(i)*sectorsPerSeg
		require.NoError(t, dev.WriteSectors(writeLBA, data))

		segID := SegmentID(uint64(i))
		phys := EncodePhysAddr(zone0ID, uint64(i)*sectorsPerSeg)
		require.NoError(t, l2p.Set(segID, phys))
		p2l.Set(phys, segID)
		zm.IncrementValidSegs(zone0ID)
	}

	// Invalidate 2 of the 4 segments (simulate overwrites)
	// Segments 0 and 1 have been moved to another zone
	for i := 0; i < 2; i++ {
		oldPhys := EncodePhysAddr(zone0ID, uint64(i)*sectorsPerSeg)

		zone1ID, _ := zm.AllocateFree()
		zone1Info, _ := zm.ZoneInfo(zone1ID)

		newPhys := EncodePhysAddr(zone1ID, 0)
		data := make([]byte, 512*sectorsPerSeg)
		require.NoError(t, dev.WriteSectors(zone1Info.StartLBA, data))
		require.NoError(t, l2p.Set(SegmentID(i), newPhys))
		p2l.Delete(oldPhys)
		p2l.Set(newPhys, SegmentID(i))
		zm.DecrementValidSegs(zone0ID)
		zm.IncrementValidSegs(zone1ID)
	}

	// Finish zone 0 so it becomes a GC candidate
	require.NoError(t, zm.MarkFull(zone0ID))

	// Verify GC score
	info0, _ := zm.ZoneInfo(zone0ID)
	assert.Equal(t, uint32(2), info0.ValidSegs)
	assert.Equal(t, uint32(4), info0.TotalSegs)

	// Run GC on victim zone
	err = gc.collectZone(zone0ID)
	require.NoError(t, err)

	// Zone 0 should now be empty
	info0After, _ := zm.ZoneInfo(zone0ID)
	assert.Equal(t, uint32(0), info0After.ValidSegs)
	assert.Equal(t, uint32(0), info0After.TotalSegs)

	// Remaining valid segments (2 and 3) should still be accessible via L2P
	// (they were migrated to new zones during GC)
	for i := 2; i < numSegs; i++ {
		phys := l2p.Get(SegmentID(i))
		assert.False(t, phys.IsUnmapped(), "segment %d should still be mapped", i)
		assert.NotEqual(t, zone0ID, phys.ZoneID(), "segment %d should have been migrated out of zone 0", i)
	}

	// GC stats should be updated
	assert.Equal(t, int64(1), gc.stats.ZonesReclaimed.Load())
}

func TestGCEngineCASRace(t *testing.T) {
	gc, l2p, p2l, zm, dev := newGCTestSetup(t)

	const sectorsPerSeg = 16

	// Set up a zone with one valid segment
	zone0ID, _ := zm.AllocateFree()
	zone0Info, _ := zm.ZoneInfo(zone0ID)

	data := make([]byte, 512*sectorsPerSeg)
	require.NoError(t, dev.WriteSectors(zone0Info.StartLBA, data))

	phys0 := EncodePhysAddr(zone0ID, 0)
	require.NoError(t, l2p.Set(0, phys0))
	p2l.Set(phys0, 0)
	zm.IncrementValidSegs(zone0ID)

	require.NoError(t, zm.MarkFull(zone0ID))

	// Simulate a foreground write updating segment 0 BEFORE GC's CAS
	zone1ID, _ := zm.AllocateFree()
	zone1Info, _ := zm.ZoneInfo(zone1ID)
	newPhysFromFG := EncodePhysAddr(zone1ID, 0)
	require.NoError(t, dev.WriteSectors(zone1Info.StartLBA, data))
	require.NoError(t, l2p.Set(0, newPhysFromFG)) // foreground write
	p2l.Delete(phys0)
	p2l.Set(newPhysFromFG, 0)
	zm.DecrementValidSegs(zone0ID)
	zm.IncrementValidSegs(zone1ID)

	// Now GC tries to migrate segment 0 (with stale old phys)
	// migrateSegment should detect that L2P no longer points to zone0
	err := gc.migrateSegment(zone0ID, 0, 0)
	require.NoError(t, err)

	// L2P should still have foreground write's value
	currentPhys := l2p.Get(0)
	assert.Equal(t, newPhysFromFG, currentPhys)
}

func TestGCShouldTrigger(t *testing.T) {
	gc, _, _, zm, _ := newGCTestSetup(t)

	// Initially 8 free zones out of 8 = 100% free ratio
	total := zm.TotalZoneCount()
	assert.Equal(t, 8, total)
	assert.False(t, gc.shouldTrigger(), "should not trigger with 100% free ratio")

	// The threshold is 0.20; fill zones until below threshold
	// Allocate all but 1 zone (12.5% free which is below 20%)
	for i := 0; i < 7; i++ {
		zid, err := zm.AllocateFree()
		require.NoError(t, err)
		require.NoError(t, zm.MarkFull(zid))
	}

	assert.True(t, gc.shouldTrigger(), "should trigger with 1/8 = 12.5% free zones")
}
