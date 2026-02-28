package ztl

import (
	"context"
	"fmt"
	"sync"

	"github.com/enkunkun/zns-iscsi-target/internal/backend"
	"github.com/enkunkun/zns-iscsi-target/internal/config"
)

// ZTL is the Zone Translation Layer orchestrator.
// It owns L2P, P2L, WriteBuffer, ZoneManager, and GCEngine.
type ZTL struct {
	mu              sync.RWMutex
	l2p             *L2PTable
	p2l             *P2LMap
	buf             *WriteBuffer
	zm              *ZoneManager
	gc              *GCEngine
	gcStats         *GCStats
	device          backend.ZonedDevice
	journal         Journal
	sectorsPerSeg   uint64
	zoneSectors     uint64
	cancelCtx       context.CancelFunc
	emergencyFreeZones int

	// Zone allocation: tracks which zone is currently accepting writes.
	// Maps a logical "stream" to a zone. For simplicity, we use a single
	// write zone that we fill sequentially.
	currentWriteZone uint32
	currentZoneOpen  bool
}

// New creates a new ZTL, initializing all sub-components.
func New(cfg *config.Config, device backend.ZonedDevice, journal Journal) (*ZTL, error) {
	sectorsPerSeg := cfg.SectorsPerSegment()
	zoneSectors := uint64(device.ZoneSize())

	if sectorsPerSeg == 0 {
		return nil, fmt.Errorf("sectors per segment must be positive")
	}
	if zoneSectors == 0 {
		return nil, fmt.Errorf("zone size must be positive")
	}

	totalSectors := device.Capacity()
	totalSegments := totalSectors / sectorsPerSeg
	if totalSegments == 0 {
		return nil, fmt.Errorf("device capacity too small for segment size")
	}

	// Initialize sub-components
	l2p := NewL2PTable(totalSegments)
	p2l := NewP2LMap()
	zm := NewZoneManager(device.MaxOpenZones(), device)
	if err := zm.Initialize(); err != nil {
		return nil, fmt.Errorf("initializing zone manager: %w", err)
	}

	buf := NewWriteBuffer(zoneSectors, cfg.ZTL.BufferFlushAgeSec)
	gcStats := &GCStats{}

	gc := NewGCEngine(l2p, p2l, zm, device, journal, gcStats, GCConfig{
		FreeTriggerRatio:   cfg.ZTL.GCTriggerFreeRatio,
		EmergencyFreeZones: cfg.ZTL.GCEmergencyFreeZones,
		SectorsPerSegment:  sectorsPerSeg,
	})

	ctx, cancel := context.WithCancel(context.Background())

	ztl := &ZTL{
		l2p:                l2p,
		p2l:                p2l,
		buf:                buf,
		zm:                 zm,
		gc:                 gc,
		gcStats:            gcStats,
		device:             device,
		journal:            journal,
		sectorsPerSeg:      sectorsPerSeg,
		zoneSectors:        zoneSectors,
		cancelCtx:          cancel,
		emergencyFreeZones: cfg.ZTL.GCEmergencyFreeZones,
	}

	// Start GC engine and buffer flush loop
	gc.Start(ctx)
	buf.StartFlushLoop(ctx, device, zm)

	return ztl, nil
}

// Read reads data from logical address lba, for count sectors.
func (ztl *ZTL) Read(lba uint64, count uint32) ([]byte, error) {
	if count == 0 {
		return []byte{}, nil
	}

	result := make([]byte, int(count)*512)
	startSeg := LBAToSegmentID(lba, ztl.sectorsPerSeg)
	endLBA := lba + uint64(count)
	endSeg := LBAToSegmentID(endLBA-1, ztl.sectorsPerSeg)

	resultOffset := 0

	for segID := startSeg; segID <= endSeg; segID++ {
		segStartLBA, segEndLBA := SegmentToLBARange(segID, ztl.sectorsPerSeg)

		// Calculate the portion of this segment that overlaps with the request
		readStart := segStartLBA
		if lba > readStart {
			readStart = lba
		}
		readEnd := segEndLBA
		if endLBA < readEnd {
			readEnd = endLBA
		}
		readCount := uint32(readEnd - readStart)

		// Look up physical address
		physAddr := ztl.l2p.Get(segID)

		if physAddr.IsUnmapped() {
			// Return zeros for unmapped segments
			resultOffset += int(readCount) * 512
			continue
		}

		zoneInfo, ok := ztl.zm.ZoneInfo(physAddr.ZoneID())
		if !ok {
			return nil, fmt.Errorf("zone %d not found in zone manager", physAddr.ZoneID())
		}

		// Physical LBA of this segment's start on the device
		physSegStartLBA := zoneInfo.StartLBA + physAddr.OffsetSectors()
		// Intra-segment offset for partial reads
		intraSegOffset := readStart - segStartLBA
		physReadLBA := physSegStartLBA + intraSegOffset

		// Check write buffer first (may have unflushed data) using physical LBA
		bufData, bufHit := ztl.buf.Lookup(physAddr.ZoneID(), physReadLBA, readCount)
		if bufHit {
			copy(result[resultOffset:], bufData)
			resultOffset += len(bufData)
			continue
		}

		// Read from device
		data, err := ztl.device.ReadSectors(physReadLBA, readCount)
		if err != nil {
			return nil, fmt.Errorf("reading lba=%d count=%d: %w", physReadLBA, readCount, err)
		}
		copy(result[resultOffset:], data)
		resultOffset += len(data)
	}

	return result, nil
}

// Write writes data to logical address lba.
func (ztl *ZTL) Write(lba uint64, data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if len(data)%512 != 0 {
		return fmt.Errorf("write data length %d not a multiple of sector size", len(data))
	}

	// Emergency GC if critically low on free zones
	if ztl.zm.FreeZoneCount() < ztl.emergencyFreeZones {
		if err := ztl.gc.RunEmergencyGC(); err != nil {
			return fmt.Errorf("emergency GC: %w", err)
		}
	}

	startSeg := LBAToSegmentID(lba, ztl.sectorsPerSeg)
	endLBA := lba + uint64(len(data))/512
	endSeg := LBAToSegmentID(endLBA-1, ztl.sectorsPerSeg)

	dataOffset := 0

	for segID := startSeg; segID <= endSeg; segID++ {
		segStartLBA, segEndLBA := SegmentToLBARange(segID, ztl.sectorsPerSeg)

		// Calculate portion of data for this segment
		writeStart := segStartLBA
		if lba > writeStart {
			writeStart = lba
		}
		writeEnd := segEndLBA
		if endLBA < writeEnd {
			writeEnd = endLBA
		}
		writeCount := int(writeEnd-writeStart) * 512
		segData := data[dataOffset : dataOffset+writeCount]
		dataOffset += writeCount

		if err := ztl.writeSegment(segID, segData, writeStart); err != nil {
			return fmt.Errorf("writing segment %d: %w", segID, err)
		}
	}

	// Group commit to WAL
	if ztl.journal != nil {
		if err := ztl.journal.GroupCommit(); err != nil {
			return fmt.Errorf("WAL group commit: %w", err)
		}
	}

	return nil
}

// writeSegment writes one segment's worth of data to the ZTL.
func (ztl *ZTL) writeSegment(segID SegmentID, data []byte, startLBA uint64) error {
	ztl.mu.Lock()
	defer ztl.mu.Unlock()

	// Get or allocate a zone for this segment
	zoneID, err := ztl.getWriteZone(segID)
	if err != nil {
		return err
	}

	zoneInfo, ok := ztl.zm.ZoneInfo(zoneID)
	if !ok {
		return fmt.Errorf("zone %d not found", zoneID)
	}

	// Calculate where in the zone this segment goes
	currentWP := ztl.zm.WritePointer(zoneID)
	offsetInZone := currentWP - zoneInfo.StartLBA
	newPhys := EncodePhysAddr(zoneID, offsetInZone)

	// Get old physical mapping (for WAL)
	oldPhys := ztl.l2p.Get(segID)

	// Log to WAL before updating L2P
	if ztl.journal != nil {
		if _, err := ztl.journal.LogL2PUpdate(segID, oldPhys, newPhys); err != nil {
			return fmt.Errorf("WAL log L2P update: %w", err)
		}
	}

	// Update L2P atomically
	if err := ztl.l2p.Set(segID, newPhys); err != nil {
		return fmt.Errorf("L2P set segment %d: %w", segID, err)
	}

	// Update P2L: remove old mapping, add new
	if !oldPhys.IsUnmapped() {
		ztl.p2l.Delete(oldPhys)
		ztl.zm.DecrementValidSegs(oldPhys.ZoneID())
	}
	ztl.p2l.Set(newPhys, segID)
	ztl.zm.IncrementValidSegs(zoneID)

	// Update write pointer tracking
	newWP := currentWP + uint64(len(data))/512
	ztl.zm.UpdateWritePointer(zoneID, newWP)

	// Add to write buffer
	if err := ztl.buf.Add(zoneID, data, currentWP); err != nil {
		return fmt.Errorf("write buffer add: %w", err)
	}

	// Check if zone is full (write pointer at end)
	if newWP >= zoneInfo.StartLBA+zoneInfo.SizeSectors {
		// Flush the buffer and mark zone full
		// Note: we must release the lock for flush since it calls device
		ztl.mu.Unlock()
		flushErr := ztl.buf.Flush(zoneID, ztl.device, ztl.zm)
		ztl.mu.Lock()
		if flushErr != nil {
			return fmt.Errorf("flush full zone %d: %w", zoneID, flushErr)
		}
		// Zone will be marked full by the emulator; update ZM
		ztl.currentZoneOpen = false
	}

	return nil
}

// getWriteZone returns the current zone ID for writing, allocating a new one if needed.
// Must be called with ztl.mu held.
func (ztl *ZTL) getWriteZone(segID SegmentID) (uint32, error) {
	// If we already have an open write zone, check if there's room
	if ztl.currentZoneOpen {
		zoneInfo, ok := ztl.zm.ZoneInfo(ztl.currentWriteZone)
		if ok {
			currentWP := ztl.zm.WritePointer(ztl.currentWriteZone)
			remainingSectors := zoneInfo.StartLBA + zoneInfo.SizeSectors - currentWP
			if remainingSectors >= ztl.sectorsPerSeg {
				return ztl.currentWriteZone, nil
			}
		}
		ztl.currentZoneOpen = false
	}

	// Allocate a new zone
	zoneID, err := ztl.zm.AllocateFree()
	if err != nil {
		return 0, fmt.Errorf("allocating write zone: %w", err)
	}

	if ztl.journal != nil {
		if err := ztl.journal.LogZoneOpen(zoneID); err != nil {
			// Non-fatal: log error but continue
			_ = err
		}
	}

	ztl.currentWriteZone = zoneID
	ztl.currentZoneOpen = true
	return zoneID, nil
}

// Flush flushes all write buffers to the device.
func (ztl *ZTL) Flush() error {
	if err := ztl.buf.FlushAll(ztl.device, ztl.zm); err != nil {
		return fmt.Errorf("ZTL.Flush: %w", err)
	}
	if ztl.journal != nil {
		return ztl.journal.GroupCommit()
	}
	return nil
}

// Unmap marks the given LBA range as unmapped (logically deleted).
func (ztl *ZTL) Unmap(lba uint64, count uint32) error {
	startSeg := LBAToSegmentID(lba, ztl.sectorsPerSeg)
	endLBA := lba + uint64(count)
	endSeg := LBAToSegmentID(endLBA-1, ztl.sectorsPerSeg)

	ztl.mu.Lock()
	defer ztl.mu.Unlock()

	for segID := startSeg; segID <= endSeg; segID++ {
		oldPhys := ztl.l2p.Get(segID)
		if oldPhys.IsUnmapped() {
			continue
		}

		// Log to WAL
		if ztl.journal != nil {
			if _, err := ztl.journal.LogL2PUpdate(segID, oldPhys, 0); err != nil {
				return fmt.Errorf("WAL log unmap: %w", err)
			}
		}

		// Invalidate L2P entry
		if err := ztl.l2p.Set(segID, 0); err != nil {
			return err
		}

		// Remove from P2L
		ztl.p2l.Delete(oldPhys)
		ztl.zm.DecrementValidSegs(oldPhys.ZoneID())
	}

	return nil
}

// Close stops background goroutines and releases resources.
func (ztl *ZTL) Close() error {
	// Stop background goroutines
	ztl.cancelCtx()
	ztl.gc.Stop()

	// Flush all write buffers
	if err := ztl.buf.FlushAll(ztl.device, ztl.zm); err != nil {
		return fmt.Errorf("ZTL.Close flush: %w", err)
	}

	return nil
}

// GCStats returns the GC statistics.
func (ztl *ZTL) GCStats() *GCStats {
	return ztl.gcStats
}

// ZoneManager returns the zone manager (for API access).
func (ztl *ZTL) ZoneManager() *ZoneManager {
	return ztl.zm
}

// TriggerGC triggers a manual GC cycle.
func (ztl *ZTL) TriggerGC() {
	ztl.gc.TriggerManual()
}

// L2PGet returns the PhysAddr for the given segment ID (for testing/inspection).
func (ztl *ZTL) L2PGet(segID SegmentID) PhysAddr {
	return ztl.l2p.Get(segID)
}

// WriteBuffer returns the write buffer (for stats access).
func (ztl *ZTL) WriteBuffer() *WriteBuffer {
	return ztl.buf
}

// BlockSize returns the logical block size in bytes (always 512).
// This satisfies the scsi.BlockDevice interface.
func (ztl *ZTL) BlockSize() uint32 {
	return ztl.device.BlockSize()
}

// Capacity returns the total device capacity in logical blocks (sectors).
// This satisfies the scsi.BlockDevice interface.
func (ztl *ZTL) Capacity() uint64 {
	return ztl.device.Capacity()
}

// L2PTable returns the L2P mapping table (used during crash recovery).
func (ztl *ZTL) L2PTable() *L2PTable {
	return ztl.l2p
}

// Device returns the underlying ZonedDevice backend (used during crash recovery).
func (ztl *ZTL) Device() backend.ZonedDevice {
	return ztl.device
}
