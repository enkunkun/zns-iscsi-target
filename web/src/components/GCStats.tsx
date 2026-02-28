import { usePolling } from '../hooks/usePolling'
import { fetchStats } from '../api/client'

function formatBytes(bytes: number): string {
  if (bytes >= 1_099_511_627_776) {
    return `${(bytes / 1_099_511_627_776).toFixed(2)} TB`
  }
  if (bytes >= 1_073_741_824) {
    return `${(bytes / 1_073_741_824).toFixed(2)} GB`
  }
  if (bytes >= 1_048_576) {
    return `${(bytes / 1_048_576).toFixed(2)} MB`
  }
  if (bytes >= 1024) {
    return `${(bytes / 1024).toFixed(2)} KB`
  }
  return `${bytes} B`
}

export function GCStats() {
  const { data, error, loading } = usePolling(fetchStats, 1000)

  return (
    <div className="panel">
      <div className="panel-header">
        <h2 className="panel-title">Garbage Collection</h2>
        {data && (
          <span
            className={`status-badge ${data.gc.running ? 'status-badge--running' : 'status-badge--idle'}`}
          >
            {data.gc.running ? 'Running' : 'Idle'}
          </span>
        )}
      </div>

      {loading && <div className="panel-loading">Loading...</div>}
      {error && <div className="panel-error">Error: {error.message}</div>}

      {data && (
        <div className="stat-grid">
          <div className="stat-item">
            <span className="stat-label">GC Status</span>
            <span className={`stat-value ${data.gc.running ? 'stat-value--running' : ''}`}>
              {data.gc.running ? 'Running' : 'Idle'}
            </span>
          </div>
          <div className="stat-item">
            <span className="stat-label">Zones Reclaimed</span>
            <span className="stat-value">{data.gc.zones_reclaimed}</span>
          </div>
          <div className="stat-item">
            <span className="stat-label">Bytes Migrated</span>
            <span className="stat-value">{formatBytes(data.gc.bytes_migrated)}</span>
          </div>
          <div className="stat-item">
            <span className="stat-label">Run Count</span>
            <span className="stat-value">{data.gc.run_count}</span>
          </div>
          {data.device.zone_count > 0 && (
            <div className="stat-item">
              <span className="stat-label">Free Zone Ratio</span>
              <span className="stat-value">
                {((data.gc.zones_reclaimed / data.device.zone_count) * 100).toFixed(1)}%
              </span>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
