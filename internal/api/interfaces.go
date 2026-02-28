package api

import (
	"github.com/enkunkun/zns-iscsi-target/internal/ztl"
)

// ZTLProvider provides zone and stats information for the API.
// The concrete *ztl.ZTL satisfies this interface.
type ZTLProvider interface {
	// ZoneManager returns the zone manager for zone listing.
	ZoneManager() *ztl.ZoneManager
	// GCStats returns the GC statistics.
	GCStats() *ztl.GCStats
	// WriteBuffer returns the write buffer for stats.
	WriteBuffer() *ztl.WriteBuffer
	// TriggerGC triggers a manual GC cycle.
	TriggerGC()
}

// ISCSIStatsProvider provides iSCSI session statistics.
type ISCSIStatsProvider interface {
	ActiveSessions() int
	ReadBytesTotal() uint64
	WriteBytesTotal() uint64
	ReadOpsTotal() uint64
	WriteOpsTotal() uint64
	ReadLatencyP99Ms() float64
	WriteLatencyP99Ms() float64
}

// JournalStatsProvider provides WAL journal statistics.
type JournalStatsProvider interface {
	CurrentLSN() uint64
	SizeBytes() uint64
	CheckpointLSN() uint64
}

// noopISCSIStats is a zero-value ISCSIStatsProvider used when no iSCSI server is available.
type noopISCSIStats struct{}

func (n *noopISCSIStats) ActiveSessions() int        { return 0 }
func (n *noopISCSIStats) ReadBytesTotal() uint64     { return 0 }
func (n *noopISCSIStats) WriteBytesTotal() uint64    { return 0 }
func (n *noopISCSIStats) ReadOpsTotal() uint64       { return 0 }
func (n *noopISCSIStats) WriteOpsTotal() uint64      { return 0 }
func (n *noopISCSIStats) ReadLatencyP99Ms() float64  { return 0 }
func (n *noopISCSIStats) WriteLatencyP99Ms() float64 { return 0 }

// noopJournalStats is a zero-value JournalStatsProvider.
type noopJournalStats struct{}

func (n *noopJournalStats) CurrentLSN() uint64    { return 0 }
func (n *noopJournalStats) SizeBytes() uint64     { return 0 }
func (n *noopJournalStats) CheckpointLSN() uint64 { return 0 }
