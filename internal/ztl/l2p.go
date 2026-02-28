package ztl

import (
	"fmt"
	"sync/atomic"
)

// L2PTable is a lock-free Logical-to-Physical mapping table.
// Each entry maps a SegmentID to a PhysAddr using atomic uint64 operations.
type L2PTable struct {
	entries []atomic.Uint64
	size    uint64
}

// NewL2PTable creates a new L2PTable with capacity for totalSegments entries.
func NewL2PTable(totalSegments uint64) *L2PTable {
	return &L2PTable{
		entries: make([]atomic.Uint64, totalSegments),
		size:    totalSegments,
	}
}

// Get returns the PhysAddr for the given segment ID.
// Returns PhysAddr(0) (unmapped) if the segment has no mapping.
func (t *L2PTable) Get(segID SegmentID) PhysAddr {
	if segID >= t.size {
		return 0
	}
	return PhysAddr(t.entries[segID].Load())
}

// Set atomically stores a PhysAddr for the given segment ID.
func (t *L2PTable) Set(segID SegmentID, phys PhysAddr) error {
	if segID >= t.size {
		return fmt.Errorf("segment ID %d out of range (max %d)", segID, t.size-1)
	}
	t.entries[segID].Store(uint64(phys))
	return nil
}

// CAS performs a compare-and-swap on the given segment's physical address.
// Returns true if the swap succeeded (old matched), false if a concurrent update occurred.
func (t *L2PTable) CAS(segID SegmentID, old, new PhysAddr) bool {
	if segID >= t.size {
		return false
	}
	return t.entries[segID].CompareAndSwap(uint64(old), uint64(new))
}

// Snapshot returns a point-in-time copy of all L2P entries.
// This is used for checkpointing; entries are loaded atomically one by one.
func (t *L2PTable) Snapshot() []PhysAddr {
	snap := make([]PhysAddr, t.size)
	for i := uint64(0); i < t.size; i++ {
		snap[i] = PhysAddr(t.entries[i].Load())
	}
	return snap
}

// LoadSnapshot restores the L2P table from a snapshot.
// Used during crash recovery.
func (t *L2PTable) LoadSnapshot(snap []PhysAddr) error {
	if uint64(len(snap)) != t.size {
		return fmt.Errorf("snapshot size %d does not match table size %d", len(snap), t.size)
	}
	for i, pa := range snap {
		t.entries[i].Store(uint64(pa))
	}
	return nil
}

// Size returns the number of entries in the table.
func (t *L2PTable) Size() uint64 {
	return t.size
}
