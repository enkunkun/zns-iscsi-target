package api

// HealthResponse is the response for the health endpoint.
type HealthResponse struct {
	Status string `json:"status"`
}

// ZoneDetail represents the state of a single zone for the API.
type ZoneDetail struct {
	ID              uint32  `json:"id"`
	State           string  `json:"state"`
	Type            string  `json:"type"`
	WritePointerLBA uint64  `json:"write_pointer_lba"`
	ZoneStartLBA    uint64  `json:"zone_start_lba"`
	ZoneLengthLBA   uint64  `json:"zone_length_lba"`
	ValidSegments   uint32  `json:"valid_segments"`
	TotalSegments   uint32  `json:"total_segments"`
	GCScore         float64 `json:"gc_score"`
}

// ZoneListResponse is the response for the list zones endpoint.
type ZoneListResponse struct {
	Zones []ZoneDetail `json:"zones"`
	Total int          `json:"total"`
}

// ISCSIStats holds iSCSI session and I/O statistics.
type ISCSIStats struct {
	ActiveSessions  int     `json:"active_sessions"`
	ReadBytesTotal  uint64  `json:"read_bytes_total"`
	WriteBytesTotal uint64  `json:"write_bytes_total"`
	ReadOpsTotal    uint64  `json:"read_ops_total"`
	WriteOpsTotal   uint64  `json:"write_ops_total"`
	ReadLatencyP99  float64 `json:"read_latency_p99_ms"`
	WriteLatencyP99 float64 `json:"write_latency_p99_ms"`
}

// GCStatsResponse holds garbage collection statistics.
type GCStatsResponse struct {
	Running        bool   `json:"running"`
	ZonesReclaimed uint64 `json:"zones_reclaimed"`
	BytesMigrated  uint64 `json:"bytes_migrated"`
	RunCount       uint64 `json:"run_count"`
}

// BufferStats holds write buffer statistics.
type BufferStats struct {
	DirtyBytes     uint64 `json:"dirty_bytes"`
	MaxBytes       uint64 `json:"max_bytes"`
	PendingFlushes int    `json:"pending_flushes"`
}

// JournalStats holds WAL journal statistics.
type JournalStats struct {
	CurrentLSN    uint64 `json:"current_lsn"`
	CheckpointLSN uint64 `json:"checkpoint_lsn"`
	SizeBytes     uint64 `json:"size_bytes"`
}

// DeviceStatsResponse holds device backend statistics.
type DeviceStatsResponse struct {
	Backend       string `json:"backend"`
	Path          string `json:"path"`
	ZoneCount     int    `json:"zone_count"`
	ZoneSizeMB    int    `json:"zone_size_mb"`
	TotalCapacity uint64 `json:"total_capacity_bytes"`
	MaxOpenZones  int    `json:"max_open_zones"`
}

// StatsResponse aggregates all statistics.
type StatsResponse struct {
	ISCSI   ISCSIStats          `json:"iscsi"`
	GC      GCStatsResponse     `json:"gc"`
	Buffer  BufferStats         `json:"buffer"`
	Journal JournalStats        `json:"journal"`
	Device  DeviceStatsResponse `json:"device"`
}

// ConfigTargetResponse is the API-safe representation of target config.
type ConfigTargetResponse struct {
	IQN         string `json:"iqn"`
	Portal      string `json:"portal"`
	MaxSessions int    `json:"max_sessions"`
	AuthEnabled bool   `json:"auth_enabled"`
	CHAPUser    string `json:"chap_user,omitempty"`
	// CHAPSecret is intentionally omitted (redacted)
}

// ConfigDeviceResponse is the API-safe representation of device config.
type ConfigDeviceResponse struct {
	Backend      string `json:"backend"`
	Path         string `json:"path,omitempty"`
	ZoneCount    int    `json:"zone_count"`
	ZoneSizeMB   int    `json:"zone_size_mb"`
	MaxOpenZones int    `json:"max_open_zones"`
}

// ConfigZTLResponse is the API-safe representation of ZTL config.
type ConfigZTLResponse struct {
	SegmentSizeKB        int     `json:"segment_size_kb"`
	BufferSizeMB         int     `json:"buffer_size_mb"`
	BufferFlushAgeSec    int     `json:"buffer_flush_age_sec"`
	GCTriggerFreeRatio   float64 `json:"gc_trigger_free_ratio"`
	GCEmergencyFreeZones int     `json:"gc_emergency_free_zones"`
}

// ConfigResponse is the API response for the config endpoint.
// CHAP secrets are redacted.
type ConfigResponse struct {
	Target  ConfigTargetResponse `json:"target"`
	Device  ConfigDeviceResponse `json:"device"`
	ZTL     ConfigZTLResponse    `json:"ztl"`
	APIListen string             `json:"api_listen"`
}

// ErrorResponse is returned on error.
type ErrorResponse struct {
	Error string `json:"error"`
}
