package ztl

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/enkunkun/zns-iscsi-target/internal/backend"
)

// GCEngine runs garbage collection in the background.
type GCEngine struct {
	mu              sync.Mutex
	l2p             *L2PTable
	p2l             *P2LMap
	zm              *ZoneManager
	device          backend.ZonedDevice
	journal         Journal
	stats           *GCStats
	sectorsPerSeg   uint64
	triggerCh       chan struct{}
	freeTriggerRatio float64
	emergencyFreeZones int
	stopCh          chan struct{}
	doneCh          chan struct{}
}

// GCConfig holds GC engine configuration.
type GCConfig struct {
	FreeTriggerRatio    float64
	EmergencyFreeZones  int
	SectorsPerSegment   uint64
}

// NewGCEngine creates a new GCEngine.
func NewGCEngine(
	l2p *L2PTable,
	p2l *P2LMap,
	zm *ZoneManager,
	device backend.ZonedDevice,
	journal Journal,
	stats *GCStats,
	cfg GCConfig,
) *GCEngine {
	return &GCEngine{
		l2p:                l2p,
		p2l:                p2l,
		zm:                 zm,
		device:             device,
		journal:            journal,
		stats:              stats,
		sectorsPerSeg:      cfg.SectorsPerSegment,
		triggerCh:          make(chan struct{}, 1),
		freeTriggerRatio:   cfg.FreeTriggerRatio,
		emergencyFreeZones: cfg.EmergencyFreeZones,
		stopCh:             make(chan struct{}),
		doneCh:             make(chan struct{}),
	}
}

// Start begins the GC background goroutine.
func (gc *GCEngine) Start(ctx context.Context) {
	go gc.run(ctx)
}

// Stop signals the GC goroutine to stop and waits for it to finish.
func (gc *GCEngine) Stop() {
	close(gc.stopCh)
	<-gc.doneCh
}

// TriggerManual sends a manual GC trigger signal.
func (gc *GCEngine) TriggerManual() {
	select {
	case gc.triggerCh <- struct{}{}:
	default:
		// Already triggered; no need to queue another
	}
}

// Stats returns the GC statistics.
func (gc *GCEngine) Stats() *GCStats {
	return gc.stats
}

// run is the main GC goroutine loop.
func (gc *GCEngine) run(ctx context.Context) {
	defer close(gc.doneCh)

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-gc.stopCh:
			return
		case <-gc.triggerCh:
			gc.runCycle()
		case <-ticker.C:
			if gc.shouldTrigger() {
				gc.runCycle()
			}
		}
	}
}

// shouldTrigger returns true if GC should be triggered based on free zone ratio.
func (gc *GCEngine) shouldTrigger() bool {
	total := gc.zm.TotalZoneCount()
	if total == 0 {
		return false
	}
	free := gc.zm.FreeZoneCount()
	ratio := float64(free) / float64(total)
	return ratio < gc.freeTriggerRatio
}

// runCycle performs one complete GC cycle.
func (gc *GCEngine) runCycle() {
	gc.stats.Running.Store(1)
	defer gc.stats.Running.Store(0)
	gc.stats.RunCount.Add(1)

	victimID, found := gc.zm.SelectGCVictim()
	if !found {
		return
	}

	if err := gc.collectZone(victimID); err != nil {
		// Log error but continue; GC failure should not crash the system
		_ = fmt.Sprintf("GC collectZone %d: %v", victimID, err)
	}
}

// collectZone migrates live data from a victim zone and resets it.
func (gc *GCEngine) collectZone(victimID uint32) error {
	// Freeze the victim zone to prevent new writes
	gc.zm.Freeze(victimID)
	defer gc.zm.Unfreeze(victimID)

	// Gather live segments from P2L
	type segInfo struct {
		segID         SegmentID
		offsetSectors uint64
	}
	var liveSegs []segInfo

	gc.p2l.IterateZone(victimID, func(offsetSectors uint64, segID SegmentID) {
		// Verify L2P still points to this zone (may have been updated concurrently)
		phys := gc.l2p.Get(segID)
		if !phys.IsUnmapped() && phys.ZoneID() == victimID && phys.OffsetSectors() == offsetSectors {
			liveSegs = append(liveSegs, segInfo{segID: segID, offsetSectors: offsetSectors})
		}
	})

	// Migrate each live segment
	for _, seg := range liveSegs {
		if err := gc.migrateSegment(victimID, seg.segID, seg.offsetSectors); err != nil {
			return fmt.Errorf("migrating segment %d: %w", seg.segID, err)
		}
	}

	// Journal the zone reset intent before performing it
	if gc.journal != nil {
		if err := gc.journal.LogZoneReset(victimID); err != nil {
			return fmt.Errorf("logging zone reset for victim %d: %w", victimID, err)
		}
		if err := gc.journal.GroupCommit(); err != nil {
			return fmt.Errorf("committing zone reset journal for victim %d: %w", victimID, err)
		}
	}

	// Reset the victim zone
	victimInfo, ok := gc.zm.ZoneInfo(victimID)
	if !ok {
		return fmt.Errorf("victim zone %d not found", victimID)
	}
	if err := gc.device.ResetZone(victimInfo.StartLBA); err != nil {
		return fmt.Errorf("resetting victim zone %d: %w", victimID, err)
	}

	// Mark zone as empty in the zone manager
	if err := gc.zm.MarkEmpty(victimID); err != nil {
		return fmt.Errorf("marking victim zone %d empty: %w", victimID, err)
	}

	gc.stats.ZonesReclaimed.Add(1)
	return nil
}

// migrateSegment reads a segment from the victim zone and writes it to a fresh zone.
func (gc *GCEngine) migrateSegment(victimID uint32, segID SegmentID, victimOffset uint64) error {
	// Re-read L2P to detect concurrent writes (foreground write wins)
	currentPhys := gc.l2p.Get(segID)
	if currentPhys.IsUnmapped() {
		// Segment was unmapped (unmap operation); nothing to migrate
		gc.p2l.Delete(currentPhys)
		return nil
	}
	if currentPhys.ZoneID() != victimID || currentPhys.OffsetSectors() != victimOffset {
		// Foreground write has already moved this segment; skip
		return nil
	}

	// Read the segment data from the victim zone
	victimInfo, ok := gc.zm.ZoneInfo(victimID)
	if !ok {
		return fmt.Errorf("victim zone %d not found", victimID)
	}
	readLBA := victimInfo.StartLBA + victimOffset
	data, err := gc.device.ReadSectors(readLBA, uint32(gc.sectorsPerSeg))
	if err != nil {
		return fmt.Errorf("reading segment from victim zone %d at LBA %d: %w", victimID, readLBA, err)
	}

	// Allocate a fresh zone for writing
	newZoneID, err := gc.zm.AllocateFree()
	if err != nil {
		return fmt.Errorf("allocating zone for GC migration: %w", err)
	}

	// Get the write pointer for the new zone
	newWP := gc.zm.WritePointer(newZoneID)
	newOffset := newWP - gc.zm.ZoneStartLBA(newZoneID)
	newPhys := EncodePhysAddr(newZoneID, newOffset)

	// Log the migration to WAL before making it visible
	if gc.journal != nil {
		if _, err := gc.journal.LogL2PUpdate(segID, currentPhys, newPhys); err != nil {
			return fmt.Errorf("logging L2P update for segment %d: %w", segID, err)
		}
	}

	// Write to the new zone
	if err := gc.device.WriteSectors(newWP, data); err != nil {
		return fmt.Errorf("writing migrated data to zone %d: %w", newZoneID, err)
	}

	// Update write pointer tracking
	gc.zm.UpdateWritePointer(newZoneID, newWP+gc.sectorsPerSeg)

	// Atomically update L2P; if CAS fails, foreground write won - discard our copy
	if !gc.l2p.CAS(segID, currentPhys, newPhys) {
		// CAS failed: foreground write happened after we read L2P.
		// Our migrated copy is stale. The zone we wrote to now has an orphaned
		// segment but we can't easily reclaim it. Increment the zone's total
		// (so it becomes a future GC candidate) but don't update P2L.
		// The orphaned segment will be collected in the next GC cycle.
		gc.zm.IncrementValidSegs(newZoneID) // count as orphan
		gc.zm.DecrementValidSegs(newZoneID) // immediately mark invalid
		return nil
	}

	// CAS succeeded: update P2L
	gc.p2l.Delete(currentPhys)
	gc.p2l.Set(newPhys, segID)

	// Update valid seg counts
	gc.zm.DecrementValidSegs(victimID)
	gc.zm.IncrementValidSegs(newZoneID)

	gc.stats.BytesMigrated.Add(int64(len(data)))

	return nil
}

// RunEmergencyGC runs GC synchronously if free zones are critically low.
func (gc *GCEngine) RunEmergencyGC() error {
	for gc.zm.FreeZoneCount() < gc.emergencyFreeZones {
		victimID, found := gc.zm.SelectGCVictim()
		if !found {
			return fmt.Errorf("emergency GC: no GC victim available")
		}
		if err := gc.collectZone(victimID); err != nil {
			return fmt.Errorf("emergency GC cycle: %w", err)
		}
	}
	return nil
}
