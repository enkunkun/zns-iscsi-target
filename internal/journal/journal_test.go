package journal

import (
	"os"
	"testing"

	"github.com/enkunkun/zns-iscsi-target/internal/ztl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestJournal(t *testing.T) (*Journal, string) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "test-journal*.wal")
	require.NoError(t, err)
	path := f.Name()
	require.NoError(t, f.Close())
	// Remove and let Open create it
	require.NoError(t, os.Remove(path))

	j, err := Open(path)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = j.Close()
		_ = os.Remove(path)
	})
	return j, path
}

func TestJournalOpenNew(t *testing.T) {
	j, _ := openTestJournal(t)
	assert.Equal(t, uint64(0), j.CurrentLSN())
}

func TestJournalLogL2PUpdate(t *testing.T) {
	j, _ := openTestJournal(t)

	phys := ztl.EncodePhysAddr(5, 1000)
	lsn, err := j.LogL2PUpdate(256, ztl.PhysAddr(0), phys)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), lsn)

	lsn2, err := j.LogL2PUpdate(257, ztl.PhysAddr(0), ztl.EncodePhysAddr(5, 1016))
	require.NoError(t, err)
	assert.Equal(t, uint64(2), lsn2)
}

func TestJournalGroupCommit(t *testing.T) {
	j, _ := openTestJournal(t)

	// Log 100 records
	for i := 0; i < 100; i++ {
		phys := ztl.EncodePhysAddr(uint32(i%10), uint64(i*16))
		_, err := j.LogL2PUpdate(uint64(i), ztl.PhysAddr(0), phys)
		require.NoError(t, err)
	}

	// GroupCommit should fsync all
	require.NoError(t, j.GroupCommit())

	// Read back
	records, err := j.ReadAllRecords(0)
	require.NoError(t, err)
	assert.Len(t, records, 100)

	// Verify order and content
	for i, rec := range records {
		assert.Equal(t, RecordTypeL2PUpdate, rec.Type)
		assert.Equal(t, uint64(i+1), rec.LSN)
		assert.Equal(t, uint64(i), rec.SegmentID)
	}
}

func TestJournalGroupCommitBatches(t *testing.T) {
	j, _ := openTestJournal(t)

	// Log 5 records but don't commit yet
	for i := 0; i < 5; i++ {
		_, err := j.LogL2PUpdate(uint64(i), ztl.PhysAddr(0), ztl.EncodePhysAddr(1, uint64(i*16)))
		require.NoError(t, err)
	}
	assert.Equal(t, 5, len(j.pending))

	// GroupCommit flushes all
	require.NoError(t, j.GroupCommit())
	assert.Equal(t, 0, len(j.pending))
}

func TestJournalZoneActions(t *testing.T) {
	j, _ := openTestJournal(t)

	require.NoError(t, j.LogZoneOpen(3))
	require.NoError(t, j.LogZoneClose(3))
	require.NoError(t, j.LogZoneReset(5))
	require.NoError(t, j.GroupCommit())

	records, err := j.ReadAllRecords(0)
	require.NoError(t, err)
	require.Len(t, records, 3)

	assert.Equal(t, RecordTypeZoneOpen, records[0].Type)
	assert.Equal(t, uint64(3), records[0].SegmentID)

	assert.Equal(t, RecordTypeZoneClose, records[1].Type)
	assert.Equal(t, uint64(3), records[1].SegmentID)

	assert.Equal(t, RecordTypeZoneReset, records[2].Type)
	assert.Equal(t, uint64(5), records[2].SegmentID)
}

func TestJournalReadAfterLSN(t *testing.T) {
	j, _ := openTestJournal(t)

	for i := 0; i < 10; i++ {
		_, err := j.LogL2PUpdate(uint64(i), ztl.PhysAddr(0), ztl.EncodePhysAddr(1, uint64(i)))
		require.NoError(t, err)
	}
	require.NoError(t, j.GroupCommit())

	// Read only records with LSN > 5
	records, err := j.ReadAllRecords(5)
	require.NoError(t, err)
	assert.Len(t, records, 5) // LSN 6-10

	for _, rec := range records {
		assert.Greater(t, rec.LSN, uint64(5))
	}
}

func TestJournalReopen(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/journal.wal"

	// Create and write to journal
	j1, err := Open(path)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		_, err := j1.LogL2PUpdate(uint64(i), ztl.PhysAddr(0), ztl.EncodePhysAddr(1, uint64(i)))
		require.NoError(t, err)
	}
	require.NoError(t, j1.Close())

	// Reopen journal - should resume from existing LSN
	j2, err := Open(path)
	require.NoError(t, err)
	defer j2.Close()

	// LSN should continue from where we left off
	assert.Equal(t, uint64(5), j2.CurrentLSN())

	// Write more records
	lsn, err := j2.LogL2PUpdate(100, ztl.PhysAddr(0), ztl.EncodePhysAddr(2, 0))
	require.NoError(t, err)
	assert.Equal(t, uint64(6), lsn)
}

func TestJournalEmptyGroupCommit(t *testing.T) {
	j, _ := openTestJournal(t)
	// GroupCommit with no pending records should be a no-op
	require.NoError(t, j.GroupCommit())
	require.NoError(t, j.GroupCommit())
}
