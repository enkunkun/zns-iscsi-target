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

export function DeviceInfo() {
  const { data, error, loading } = usePolling(fetchStats, 1000)

  return (
    <div className="panel">
      <div className="panel-header">
        <h2 className="panel-title">Device Info</h2>
      </div>

      {loading && <div className="panel-loading">Loading...</div>}
      {error && <div className="panel-error">Error: {error.message}</div>}

      {data && (
        <div className="stat-grid">
          <div className="stat-item">
            <span className="stat-label">Backend</span>
            <span className="stat-value stat-value--highlight">{data.device.backend}</span>
          </div>
          <div className="stat-item">
            <span className="stat-label">Zone Count</span>
            <span className="stat-value">{data.device.zone_count}</span>
          </div>
          <div className="stat-item">
            <span className="stat-label">Zone Size</span>
            <span className="stat-value">{data.device.zone_size_mb} MB</span>
          </div>
          <div className="stat-item">
            <span className="stat-label">Total Capacity</span>
            <span className="stat-value">{formatBytes(data.device.total_capacity_bytes)}</span>
          </div>
          <div className="stat-item">
            <span className="stat-label">Max Open Zones</span>
            <span className="stat-value">{data.device.max_open_zones}</span>
          </div>
          {data.device.path && (
            <div className="stat-item stat-item--full">
              <span className="stat-label">Path</span>
              <span className="stat-value stat-value--mono">{data.device.path}</span>
            </div>
          )}
        </div>
      )}
    </div>
  )
}
