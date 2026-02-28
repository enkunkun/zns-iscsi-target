package ztl

import (
	"container/list"
	"fmt"
	"sync"

	"github.com/enkunkun/zns-iscsi-target/internal/backend"
	"github.com/enkunkun/zns-iscsi-target/pkg/zbc"
)

// ZoneInfo holds the current state of a zone as tracked by the ZoneManager.
type ZoneInfo struct {
	ID           uint32
	State        zbc.ZoneState
	WritePointer uint64 // absolute LBA of the write pointer
	StartLBA     uint64
	SizeSectors  uint64
	ValidSegs    uint32 // live segments with L2P mappings (GC scoring)
	TotalSegs    uint32 // total segments ever written (GC scoring)
	Frozen       bool   // if true, GC has claimed this zone; no new writes allowed
}

// GCScore returns a value between 0 and 1 representing the fraction of invalid segments.
// Higher score = better GC candidate.
func (z *ZoneInfo) GCScore() float64 {
	if z.TotalSegs == 0 {
		return 0
	}
	invalid := z.TotalSegs - z.ValidSegs
	return float64(invalid) / float64(z.TotalSegs)
}

// ZoneManager manages zone states, open zone tracking, and zone allocation.
type ZoneManager struct {
	mu           sync.Mutex
	zones        []ZoneInfo
	freeList     []uint32      // zone IDs that are EMPTY and available
	openList     *list.List    // LRU list of open zone IDs
	openSet      map[uint32]*list.Element
	frozenSet    map[uint32]struct{}
	maxOpenZones int
	device       backend.ZonedDevice
}

// NewZoneManager creates a ZoneManager but does not initialize from device yet.
func NewZoneManager(maxOpenZones int, device backend.ZonedDevice) *ZoneManager {
	return &ZoneManager{
		openList:     list.New(),
		openSet:      make(map[uint32]*list.Element),
		frozenSet:    make(map[uint32]struct{}),
		maxOpenZones: maxOpenZones,
		device:       device,
	}
}

// Initialize populates zone information from the device's ReportZones.
func (zm *ZoneManager) Initialize() error {
	zm.mu.Lock()
	defer zm.mu.Unlock()

	descs, err := zm.device.ReportZones(0, 0)
	if err != nil {
		return fmt.Errorf("ZoneManager.Initialize: %w", err)
	}

	zm.zones = make([]ZoneInfo, len(descs))
	zm.freeList = make([]uint32, 0, len(descs))

	for i, d := range descs {
		zm.zones[i] = ZoneInfo{
			ID:           uint32(i),
			State:        d.ZoneState,
			WritePointer: d.WritePointer,
			StartLBA:     d.ZoneStartLBA,
			SizeSectors:  d.ZoneLength,
		}

		switch d.ZoneState {
		case zbc.ZoneStateEmpty:
			zm.freeList = append(zm.freeList, uint32(i))
		case zbc.ZoneStateImplicitOpen, zbc.ZoneStateExplicitOpen:
			el := zm.openList.PushBack(uint32(i))
			zm.openSet[uint32(i)] = el
		}
	}

	return nil
}

// AllocateFree removes an EMPTY zone from the free list and opens it on the device.
// Returns the zone ID.
func (zm *ZoneManager) AllocateFree() (uint32, error) {
	zm.mu.Lock()
	defer zm.mu.Unlock()

	if len(zm.freeList) == 0 {
		return 0, fmt.Errorf("no free zones available")
	}

	// Evict an open zone if at the limit
	if len(zm.openSet) >= zm.maxOpenZones {
		if err := zm.evictOldestLocked(); err != nil {
			return 0, fmt.Errorf("AllocateFree: evict: %w", err)
		}
	}

	zoneID := zm.freeList[len(zm.freeList)-1]
	zm.freeList = zm.freeList[:len(zm.freeList)-1]

	z := &zm.zones[zoneID]
	// Open zone on device
	if err := zm.device.OpenZone(z.StartLBA); err != nil {
		// Put back in free list
		zm.freeList = append(zm.freeList, zoneID)
		return 0, fmt.Errorf("opening zone %d: %w", zoneID, err)
	}

	z.State = zbc.ZoneStateExplicitOpen
	el := zm.openList.PushBack(zoneID)
	zm.openSet[zoneID] = el

	return zoneID, nil
}

// GetOrOpen ensures a zone is open. If it's closed, it will be opened.
// Evicts LRU open zone if at the limit.
func (zm *ZoneManager) GetOrOpen(zoneID uint32) error {
	zm.mu.Lock()
	defer zm.mu.Unlock()

	if int(zoneID) >= len(zm.zones) {
		return fmt.Errorf("zone ID %d out of range", zoneID)
	}
	z := &zm.zones[zoneID]

	// Move to front of LRU if already open
	if el, ok := zm.openSet[zoneID]; ok {
		zm.openList.MoveToBack(el)
		return nil
	}

	// Zone is not open; check if we need to evict
	if len(zm.openSet) >= zm.maxOpenZones {
		if err := zm.evictOldestLocked(); err != nil {
			return err
		}
	}

	// Open the zone
	if err := zm.device.OpenZone(z.StartLBA); err != nil {
		return fmt.Errorf("opening zone %d: %w", zoneID, err)
	}

	z.State = zbc.ZoneStateExplicitOpen
	el := zm.openList.PushBack(zoneID)
	zm.openSet[zoneID] = el
	return nil
}

// evictOldestLocked closes the least-recently-used open zone.
// Must be called with zm.mu held.
func (zm *ZoneManager) evictOldestLocked() error {
	front := zm.openList.Front()
	if front == nil {
		return nil
	}
	zoneID := front.Value.(uint32)

	z := &zm.zones[zoneID]
	if err := zm.device.CloseZone(z.StartLBA); err != nil {
		return fmt.Errorf("closing zone %d for eviction: %w", zoneID, err)
	}

	z.State = zbc.ZoneStateClosed
	zm.openList.Remove(front)
	delete(zm.openSet, zoneID)
	return nil
}

// MarkFull marks a zone as FULL and finishes it on the device.
func (zm *ZoneManager) MarkFull(zoneID uint32) error {
	zm.mu.Lock()
	defer zm.mu.Unlock()

	if int(zoneID) >= len(zm.zones) {
		return fmt.Errorf("zone ID %d out of range", zoneID)
	}
	z := &zm.zones[zoneID]

	if err := zm.device.FinishZone(z.StartLBA); err != nil {
		return fmt.Errorf("finishing zone %d: %w", zoneID, err)
	}

	z.State = zbc.ZoneStateFull
	z.WritePointer = z.StartLBA + z.SizeSectors

	if el, ok := zm.openSet[zoneID]; ok {
		zm.openList.Remove(el)
		delete(zm.openSet, zoneID)
	}
	return nil
}

// MarkEmpty returns a zone to the free list (after zone reset).
func (zm *ZoneManager) MarkEmpty(zoneID uint32) error {
	zm.mu.Lock()
	defer zm.mu.Unlock()

	if int(zoneID) >= len(zm.zones) {
		return fmt.Errorf("zone ID %d out of range", zoneID)
	}
	z := &zm.zones[zoneID]
	z.State = zbc.ZoneStateEmpty
	z.WritePointer = z.StartLBA
	z.ValidSegs = 0
	z.TotalSegs = 0

	if el, ok := zm.openSet[zoneID]; ok {
		zm.openList.Remove(el)
		delete(zm.openSet, zoneID)
	}

	delete(zm.frozenSet, zoneID)
	zm.freeList = append(zm.freeList, zoneID)
	return nil
}

// UpdateWritePointer updates the zone's tracked write pointer.
func (zm *ZoneManager) UpdateWritePointer(zoneID uint32, newWP uint64) {
	zm.mu.Lock()
	if int(zoneID) < len(zm.zones) {
		zm.zones[zoneID].WritePointer = newWP
	}
	zm.mu.Unlock()
}

// IncrementValidSegs increments the valid segment count for a zone.
func (zm *ZoneManager) IncrementValidSegs(zoneID uint32) {
	zm.mu.Lock()
	if int(zoneID) < len(zm.zones) {
		zm.zones[zoneID].ValidSegs++
		zm.zones[zoneID].TotalSegs++
	}
	zm.mu.Unlock()
}

// DecrementValidSegs decrements the valid segment count (on GC or unmap).
func (zm *ZoneManager) DecrementValidSegs(zoneID uint32) {
	zm.mu.Lock()
	if int(zoneID) < len(zm.zones) && zm.zones[zoneID].ValidSegs > 0 {
		zm.zones[zoneID].ValidSegs--
	}
	zm.mu.Unlock()
}

// Freeze prevents new allocations from using this zone (GC is reclaiming it).
func (zm *ZoneManager) Freeze(zoneID uint32) {
	zm.mu.Lock()
	zm.frozenSet[zoneID] = struct{}{}
	zm.mu.Unlock()
}

// Unfreeze allows the zone to be allocated again.
func (zm *ZoneManager) Unfreeze(zoneID uint32) {
	zm.mu.Lock()
	delete(zm.frozenSet, zoneID)
	zm.mu.Unlock()
}

// FreeZoneCount returns the number of free (EMPTY) zones.
func (zm *ZoneManager) FreeZoneCount() int {
	zm.mu.Lock()
	n := len(zm.freeList)
	zm.mu.Unlock()
	return n
}

// OpenZoneCount returns the number of currently open zones.
func (zm *ZoneManager) OpenZoneCount() int {
	zm.mu.Lock()
	n := len(zm.openSet)
	zm.mu.Unlock()
	return n
}

// TotalZoneCount returns the total number of zones.
func (zm *ZoneManager) TotalZoneCount() int {
	zm.mu.Lock()
	n := len(zm.zones)
	zm.mu.Unlock()
	return n
}

// ZoneInfo returns a copy of the ZoneInfo for the given zone ID.
func (zm *ZoneManager) ZoneInfo(zoneID uint32) (ZoneInfo, bool) {
	zm.mu.Lock()
	defer zm.mu.Unlock()
	if int(zoneID) >= len(zm.zones) {
		return ZoneInfo{}, false
	}
	return zm.zones[zoneID], true
}

// AllZones returns a copy of all zone infos.
func (zm *ZoneManager) AllZones() []ZoneInfo {
	zm.mu.Lock()
	defer zm.mu.Unlock()
	result := make([]ZoneInfo, len(zm.zones))
	copy(result, zm.zones)
	return result
}

// SelectGCVictim returns the zone ID with the highest GC score (most invalid data).
// Returns (0, false) if no suitable victim exists.
func (zm *ZoneManager) SelectGCVictim() (uint32, bool) {
	zm.mu.Lock()
	defer zm.mu.Unlock()

	var bestID uint32
	bestScore := -1.0
	found := false

	for i := range zm.zones {
		z := &zm.zones[i]
		if _, frozen := zm.frozenSet[z.ID]; frozen {
			continue
		}
		if z.State != zbc.ZoneStateFull && z.State != zbc.ZoneStateClosed {
			continue
		}
		if z.TotalSegs == 0 {
			continue
		}
		score := z.GCScore()
		if score > bestScore {
			bestScore = score
			bestID = z.ID
			found = true
		}
	}

	return bestID, found
}

// WritePointer returns the write pointer for a zone.
func (zm *ZoneManager) WritePointer(zoneID uint32) uint64 {
	zm.mu.Lock()
	defer zm.mu.Unlock()
	if int(zoneID) >= len(zm.zones) {
		return 0
	}
	return zm.zones[zoneID].WritePointer
}

// ZoneStartLBA returns the start LBA for a zone.
func (zm *ZoneManager) ZoneStartLBA(zoneID uint32) uint64 {
	zm.mu.Lock()
	defer zm.mu.Unlock()
	if int(zoneID) >= len(zm.zones) {
		return 0
	}
	return zm.zones[zoneID].StartLBA
}

// ReconcileFromDevice updates zone states from device's ReportZones response.
// Used during crash recovery.
func (zm *ZoneManager) ReconcileFromDevice() error {
	descs, err := zm.device.ReportZones(0, 0)
	if err != nil {
		return fmt.Errorf("ReconcileFromDevice: %w", err)
	}

	zm.mu.Lock()
	defer zm.mu.Unlock()

	for i, d := range descs {
		if i >= len(zm.zones) {
			break
		}
		zm.zones[i].State = d.ZoneState
		zm.zones[i].WritePointer = d.WritePointer
	}
	return nil
}
