package journal

import (
	"os"
	"testing"

	"github.com/enkunkun/zns-iscsi-target/internal/ztl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckpointWriteRead(t *testing.T) {
	j, path := openTestJournal(t)

	// Create an L2P table with some entries
	l2p := ztl.NewL2PTable(100)
	for i := uint64(0); i < 100; i++ {
		phys := ztl.EncodePhysAddr(uint32(i%10), i*16)
		require.NoError(t, l2p.Set(i, phys))
	}

	// Write checkpoint
	require.NoError(t, WriteCheckpoint(j, l2p))

	// Read it back
	snap, lsn, err := ReadLatestCheckpoint(path)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), lsn)
	assert.Len(t, snap, 100)

	// Verify entries match
	for i := uint64(0); i < 100; i++ {
		expected := ztl.EncodePhysAddr(uint32(i%10), i*16)
		assert.Equal(t, expected, snap[i], "entry %d", i)
	}
}

func TestCheckpointLatestSelected(t *testing.T) {
	j, path := openTestJournal(t)

	// Write some L2P updates first
	l2p1 := ztl.NewL2PTable(10)
	for i := uint64(0); i < 10; i++ {
		require.NoError(t, l2p1.Set(i, ztl.EncodePhysAddr(1, i*16)))
	}

	// Log some records then write first checkpoint
	for i := 0; i < 5; i++ {
		_, err := j.LogL2PUpdate(uint64(i), ztl.PhysAddr(0), ztl.EncodePhysAddr(1, uint64(i)))
		require.NoError(t, err)
	}
	require.NoError(t, j.GroupCommit())
	require.NoError(t, WriteCheckpoint(j, l2p1))

	// Update L2P and write second checkpoint
	l2p2 := ztl.NewL2PTable(10)
	for i := uint64(0); i < 10; i++ {
		require.NoError(t, l2p2.Set(i, ztl.EncodePhysAddr(2, i*32)))
	}
	for i := 0; i < 5; i++ {
		_, err := j.LogL2PUpdate(uint64(i), ztl.PhysAddr(0), ztl.EncodePhysAddr(2, uint64(i)))
		require.NoError(t, err)
	}
	require.NoError(t, j.GroupCommit())
	require.NoError(t, WriteCheckpoint(j, l2p2))

	// Latest checkpoint should have zone ID 2 entries
	snap, lsn, err := ReadLatestCheckpoint(path)
	require.NoError(t, err)
	assert.Greater(t, lsn, uint64(0))
	require.Len(t, snap, 10)

	// Second checkpoint should have zone 2 entries
	for i := uint64(0); i < 10; i++ {
		assert.Equal(t, uint32(2), snap[i].ZoneID(), "entry %d", i)
	}
}

func TestCheckpointNoFile(t *testing.T) {
	snap, lsn, err := ReadLatestCheckpoint("/nonexistent/path.wal")
	require.NoError(t, err) // non-existent is OK
	assert.Nil(t, snap)
	assert.Equal(t, uint64(0), lsn)
}

func TestCheckpointNoCheckpointInFile(t *testing.T) {
	j, path := openTestJournal(t)

	// Write only L2P updates, no checkpoint
	_, err := j.LogL2PUpdate(1, ztl.PhysAddr(0), ztl.EncodePhysAddr(1, 0))
	require.NoError(t, err)
	require.NoError(t, j.GroupCommit())

	snap, lsn, err := ReadLatestCheckpoint(path)
	require.NoError(t, err)
	assert.Nil(t, snap)
	assert.Equal(t, uint64(0), lsn)
}

func TestCheckpointEmptyL2P(t *testing.T) {
	j, path := openTestJournal(t)
	l2p := ztl.NewL2PTable(5)

	require.NoError(t, WriteCheckpoint(j, l2p))

	snap, lsn, err := ReadLatestCheckpoint(path)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), lsn)
	assert.Len(t, snap, 5)
	// All entries should be zero (unmapped)
	for _, pa := range snap {
		assert.True(t, pa.IsUnmapped())
	}
}

func TestCheckpointFileEmpty(t *testing.T) {
	path := t.TempDir() + "/empty.wal"
	// Create empty file
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	snap, lsn, err := ReadLatestCheckpoint(path)
	require.NoError(t, err)
	assert.Nil(t, snap)
	assert.Equal(t, uint64(0), lsn)
}
