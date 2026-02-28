package ztl

import "sync"

// p2lKey is used as map key for P2L lookups.
type p2lKey struct {
	zoneID uint32
	offset uint64
}

// P2LMap is the Physical-to-Logical reverse mapping.
// It maps (zoneID, offsetSectors) -> SegmentID.
// This is used by the GC to enumerate valid segments in a victim zone.
type P2LMap struct {
	mu      sync.RWMutex
	entries map[p2lKey]SegmentID
}

// NewP2LMap creates a new P2LMap.
func NewP2LMap() *P2LMap {
	return &P2LMap{
		entries: make(map[p2lKey]SegmentID),
	}
}

// Set records a physical address -> segment ID mapping.
func (m *P2LMap) Set(phys PhysAddr, segID SegmentID) {
	key := p2lKey{zoneID: phys.ZoneID(), offset: phys.OffsetSectors()}
	m.mu.Lock()
	m.entries[key] = segID
	m.mu.Unlock()
}

// Get returns the segment ID for the given physical address, and whether it was found.
func (m *P2LMap) Get(phys PhysAddr) (SegmentID, bool) {
	key := p2lKey{zoneID: phys.ZoneID(), offset: phys.OffsetSectors()}
	m.mu.RLock()
	segID, ok := m.entries[key]
	m.mu.RUnlock()
	return segID, ok
}

// Delete removes a physical address from the map.
func (m *P2LMap) Delete(phys PhysAddr) {
	key := p2lKey{zoneID: phys.ZoneID(), offset: phys.OffsetSectors()}
	m.mu.Lock()
	delete(m.entries, key)
	m.mu.Unlock()
}

// IterateZone calls fn for each segment in the given zone.
// fn receives the sector offset within the zone and the segment ID.
// The iteration holds the read lock, so fn must not call P2L methods.
func (m *P2LMap) IterateZone(zoneID uint32, fn func(offsetSectors uint64, segID SegmentID)) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	for key, segID := range m.entries {
		if key.zoneID == zoneID {
			fn(key.offset, segID)
		}
	}
}

// Len returns the total number of entries in the map.
func (m *P2LMap) Len() int {
	m.mu.RLock()
	n := len(m.entries)
	m.mu.RUnlock()
	return n
}
