package journal

import (
	"fmt"

	"github.com/enkunkun/zns-iscsi-target/internal/backend"
	"github.com/enkunkun/zns-iscsi-target/internal/ztl"
)

// ZoneManagerForRecovery is the interface Recovery needs from the zone manager.
type ZoneManagerForRecovery interface {
	MarkEmpty(zoneID uint32) error
	ReconcileFromDevice() error
}

// Recover performs crash recovery:
// 1. Load the most recent valid checkpoint from the journal.
// 2. Replay WAL entries with LSN > checkpoint LSN.
// 3. Call ReconcileFromDevice on the zone manager to sync write pointers.
func Recover(j *Journal, l2p *ztl.L2PTable, zm ZoneManagerForRecovery, device backend.ZonedDevice) error {
	// Step 1: Load latest checkpoint
	snap, checkpointLSN, err := ReadLatestCheckpoint(j.path)
	if err != nil {
		return fmt.Errorf("recovery: reading checkpoint: %w", err)
	}

	if snap != nil {
		// Restore L2P from checkpoint
		if err := l2p.LoadSnapshot(snap); err != nil {
			return fmt.Errorf("recovery: loading L2P snapshot: %w", err)
		}
	}

	// Step 2: Replay WAL records after the checkpoint LSN
	records, err := j.ReadAllRecords(checkpointLSN)
	if err != nil {
		return fmt.Errorf("recovery: reading WAL records: %w", err)
	}

	replayedCount := 0
	for _, rec := range records {
		switch rec.Type {
		case RecordTypeL2PUpdate:
			if err := l2p.Set(rec.SegmentID, rec.NewPhysAddr); err != nil {
				return fmt.Errorf("recovery: replaying L2P update segID=%d: %w", rec.SegmentID, err)
			}
			replayedCount++

		case RecordTypeZoneReset:
			zoneID := uint32(rec.SegmentID)
			if err := zm.MarkEmpty(zoneID); err != nil {
				return fmt.Errorf("recovery: marking zone %d empty after reset: %w", zoneID, err)
			}
			replayedCount++

		case RecordTypeZoneOpen, RecordTypeZoneClose:
			// These are informational; actual state comes from ReportZones
			replayedCount++

		case RecordTypeCheckpoint:
			// Skip intermediate checkpoints (already loaded the latest)
		}
	}

	// Step 3: Reconcile write pointers from device
	if err := zm.ReconcileFromDevice(); err != nil {
		return fmt.Errorf("recovery: reconciling from device: %w", err)
	}

	return nil
}

// RecoveryResult holds statistics from a recovery run.
type RecoveryResult struct {
	CheckpointLSN  uint64
	RecordsReplayed int
	L2PTableSize   uint64
}
