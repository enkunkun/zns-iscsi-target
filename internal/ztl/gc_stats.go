package ztl

import "sync/atomic"

// GCStats holds atomic counters for garbage collection metrics.
type GCStats struct {
	ZonesReclaimed atomic.Int64
	BytesMigrated  atomic.Int64
	RunCount       atomic.Int64
	Running        atomic.Int32 // 1 if GC is running, 0 otherwise
}

// GCStatsSnapshot is a non-atomic snapshot of GCStats for API response serialization.
type GCStatsSnapshot struct {
	ZonesReclaimed int64 `json:"zones_reclaimed"`
	BytesMigrated  int64 `json:"bytes_migrated"`
	RunCount       int64 `json:"run_count"`
	Running        bool  `json:"running"`
}

// Snapshot returns a point-in-time copy of GC statistics.
func (s *GCStats) Snapshot() GCStatsSnapshot {
	return GCStatsSnapshot{
		ZonesReclaimed: s.ZonesReclaimed.Load(),
		BytesMigrated:  s.BytesMigrated.Load(),
		RunCount:       s.RunCount.Load(),
		Running:        s.Running.Load() != 0,
	}
}
