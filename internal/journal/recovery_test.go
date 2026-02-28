package journal

import (
	"os"
	"testing"

	"github.com/enkunkun/zns-iscsi-target/internal/backend/emulator"
	"github.com/enkunkun/zns-iscsi-target/internal/ztl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestDevice(t *testing.T) *emulator.Emulator {
	t.Helper()
	dev, err := emulator.New(emulator.Config{
		ZoneCount:    8,
		ZoneSizeMB:   1,
		MaxOpenZones: 4,
	})
	require.NoError(t, err)
	return dev
}

// simpleZoneManager is a minimal ZoneManager implementation for recovery tests.
type simpleZoneManager struct {
	emptyMarked []uint32
	dev         *emulator.Emulator
}

func (m *simpleZoneManager) MarkEmpty(zoneID uint32) error {
	m.emptyMarked = append(m.emptyMarked, zoneID)
	return nil
}

func (m *simpleZoneManager) ReconcileFromDevice() error {
	return nil
}

func TestRecoveryFromCheckpoint(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/journal.wal"

	// Phase 1: Write data, take checkpoint
	j1, err := Open(path)
	require.NoError(t, err)

	l2p1 := ztl.NewL2PTable(10)
	for i := uint64(0); i < 10; i++ {
		phys := ztl.EncodePhysAddr(uint32(i%3), i*16)
		require.NoError(t, l2p1.Set(i, phys))
		_, err := j1.LogL2PUpdate(i, ztl.PhysAddr(0), phys)
		require.NoError(t, err)
	}
	require.NoError(t, j1.GroupCommit())

	// Write checkpoint
	require.NoError(t, WriteCheckpoint(j1, l2p1))

	// Write a few more records after checkpoint
	phys10 := ztl.EncodePhysAddr(5, 1000)
	_, err = j1.LogL2PUpdate(0, l2p1.Get(0), phys10)
	require.NoError(t, err)
	require.NoError(t, j1.GroupCommit())
	require.NoError(t, j1.Close())

	// Phase 2: Recovery
	j2, err := Open(path)
	require.NoError(t, err)
	defer j2.Close()

	l2p2 := ztl.NewL2PTable(10)
	dev := newTestDevice(t)
	zm := &simpleZoneManager{dev: dev}

	err = Recover(j2, l2p2, zm, dev)
	require.NoError(t, err)

	// Segment 0 should have been updated by post-checkpoint record
	assert.Equal(t, phys10, l2p2.Get(0))

	// Segments 1-9 should have checkpoint values
	for i := uint64(1); i < 10; i++ {
		expected := ztl.EncodePhysAddr(uint32(i%3), i*16)
		assert.Equal(t, expected, l2p2.Get(i), "segment %d", i)
	}
}

func TestRecoveryNoCheckpoint(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/journal.wal"

	// Write only WAL records, no checkpoint
	j1, err := Open(path)
	require.NoError(t, err)

	phys1 := ztl.EncodePhysAddr(1, 0)
	phys2 := ztl.EncodePhysAddr(2, 0)
	_, err = j1.LogL2PUpdate(5, ztl.PhysAddr(0), phys1)
	require.NoError(t, err)
	_, err = j1.LogL2PUpdate(7, ztl.PhysAddr(0), phys2)
	require.NoError(t, err)
	require.NoError(t, j1.GroupCommit())
	require.NoError(t, j1.Close())

	// Recovery with no checkpoint
	j2, err := Open(path)
	require.NoError(t, err)
	defer j2.Close()

	l2p := ztl.NewL2PTable(10)
	dev := newTestDevice(t)
	zm := &simpleZoneManager{dev: dev}

	err = Recover(j2, l2p, zm, dev)
	require.NoError(t, err)

	// Records should have been replayed
	assert.Equal(t, phys1, l2p.Get(5))
	assert.Equal(t, phys2, l2p.Get(7))
}

func TestRecoveryZoneReset(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/journal.wal"

	j1, err := Open(path)
	require.NoError(t, err)

	// Log L2P updates for a zone
	for i := uint64(0); i < 5; i++ {
		phys := ztl.EncodePhysAddr(0, i*16) // zone 0
		_, err := j1.LogL2PUpdate(i, ztl.PhysAddr(0), phys)
		require.NoError(t, err)
	}
	require.NoError(t, j1.GroupCommit())

	// Log zone reset
	require.NoError(t, j1.LogZoneReset(0))
	require.NoError(t, j1.GroupCommit())
	require.NoError(t, j1.Close())

	// Recovery
	j2, err := Open(path)
	require.NoError(t, err)
	defer j2.Close()

	l2p := ztl.NewL2PTable(10)
	dev := newTestDevice(t)
	zm := &simpleZoneManager{dev: dev}

	err = Recover(j2, l2p, zm, dev)
	require.NoError(t, err)

	// ZoneReset should have been replayed
	assert.Contains(t, zm.emptyMarked, uint32(0))
}

func TestRecoveryCrashBeforeCheckpoint(t *testing.T) {
	// Simulate: crash right after writing WAL records but before checkpoint
	dir := t.TempDir()
	path := dir + "/journal.wal"

	// Write some WAL records (simulating crash before checkpoint)
	j1, err := Open(path)
	require.NoError(t, err)

	phys := ztl.EncodePhysAddr(1, 500)
	_, err = j1.LogL2PUpdate(42, ztl.PhysAddr(0), phys)
	require.NoError(t, err)
	require.NoError(t, j1.GroupCommit())

	// Crash (close without checkpoint)
	require.NoError(t, j1.file.Close())

	// Recovery should replay the WAL record
	j2, err := Open(path)
	require.NoError(t, err)
	defer j2.Close()

	l2p := ztl.NewL2PTable(100)
	dev := newTestDevice(t)
	zm := &simpleZoneManager{dev: dev}

	err = Recover(j2, l2p, zm, dev)
	require.NoError(t, err)

	assert.Equal(t, phys, l2p.Get(42))
}

func TestRecoveryEmptyJournal(t *testing.T) {
	path := t.TempDir() + "/empty.wal"
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	j, err := Open(path)
	require.NoError(t, err)
	defer j.Close()

	l2p := ztl.NewL2PTable(10)
	dev := newTestDevice(t)
	zm := &simpleZoneManager{dev: dev}

	err = Recover(j, l2p, zm, dev)
	require.NoError(t, err)

	// L2P should be all unmapped
	for i := uint64(0); i < 10; i++ {
		assert.True(t, l2p.Get(i).IsUnmapped())
	}
}

func TestRecoveryMultipleCheckpoints(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/journal.wal"

	j1, err := Open(path)
	require.NoError(t, err)

	l2p1 := ztl.NewL2PTable(5)
	for i := uint64(0); i < 5; i++ {
		require.NoError(t, l2p1.Set(i, ztl.EncodePhysAddr(1, i)))
	}
	require.NoError(t, WriteCheckpoint(j1, l2p1))

	// Write more and take another checkpoint
	_, err = j1.LogL2PUpdate(0, l2p1.Get(0), ztl.EncodePhysAddr(9, 999))
	require.NoError(t, err)
	require.NoError(t, j1.GroupCommit())

	l2p2 := ztl.NewL2PTable(5)
	for i := uint64(0); i < 5; i++ {
		require.NoError(t, l2p2.Set(i, ztl.EncodePhysAddr(2, i)))
	}
	require.NoError(t, WriteCheckpoint(j1, l2p2))
	require.NoError(t, j1.Close())

	// Recovery should use the latest checkpoint
	j2, err := Open(path)
	require.NoError(t, err)
	defer j2.Close()

	l2p := ztl.NewL2PTable(5)
	dev := newTestDevice(t)
	zm := &simpleZoneManager{dev: dev}

	err = Recover(j2, l2p, zm, dev)
	require.NoError(t, err)

	// Should have zone ID 2 entries (from second checkpoint)
	for i := uint64(0); i < 5; i++ {
		assert.Equal(t, uint32(2), l2p.Get(i).ZoneID(), "segment %d", i)
	}
}
