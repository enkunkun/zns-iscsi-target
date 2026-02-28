export type ZoneState =
  | 'EMPTY'
  | 'IMPLICIT_OPEN'
  | 'EXPLICIT_OPEN'
  | 'CLOSED'
  | 'FULL'
  | 'READ_ONLY'
  | 'OFFLINE'

export type ZoneType = 'SEQ_WRITE_REQUIRED' | 'SEQ_WRITE_PREFERRED' | 'CONVENTIONAL'

export interface Zone {
  id: number
  state: ZoneState
  type: ZoneType
  write_pointer_lba: number
  zone_start_lba: number
  zone_length_lba: number
  valid_segments: number
  total_segments: number
  gc_score: number
}

export interface ZonesResponse {
  zones: Zone[]
  total: number
}

export interface ISCSIStats {
  active_sessions: number
  read_bytes_total: number
  write_bytes_total: number
  read_ops_total: number
  write_ops_total: number
  read_latency_p99_ms: number
  write_latency_p99_ms: number
}

export interface GCStats {
  running: boolean
  zones_reclaimed: number
  bytes_migrated: number
  run_count: number
}

export interface BufferStats {
  dirty_bytes: number
  max_bytes: number
  pending_flushes: number
}

export interface JournalStats {
  current_lsn: number
  checkpoint_lsn: number
  size_bytes: number
}

export interface DeviceStats {
  backend: string
  path: string
  zone_count: number
  zone_size_mb: number
  total_capacity_bytes: number
  max_open_zones: number
}

export interface StatsResponse {
  iscsi: ISCSIStats
  gc: GCStats
  buffer: BufferStats
  journal: JournalStats
  device: DeviceStats
}

export interface HealthResponse {
  status: string
}
