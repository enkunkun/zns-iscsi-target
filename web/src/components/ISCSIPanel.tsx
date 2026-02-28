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

function formatOps(ops: number): string {
  if (ops >= 1_000_000) return `${(ops / 1_000_000).toFixed(1)}M`
  if (ops >= 1_000) return `${(ops / 1_000).toFixed(1)}K`
  return String(ops)
}

export function ISCSIPanel() {
  const { data, error, loading } = usePolling(fetchStats, 1000)

  return (
    <div className="panel">
      <div className="panel-header">
        <h2 className="panel-title">iSCSI Sessions</h2>
        {data && (
          <span
            className={`status-badge ${data.iscsi.active_sessions > 0 ? 'status-badge--active' : 'status-badge--idle'}`}
          >
            {data.iscsi.active_sessions > 0 ? 'Active' : 'Idle'}
          </span>
        )}
      </div>

      {loading && <div className="panel-loading">Loading...</div>}
      {error && <div className="panel-error">Error: {error.message}</div>}

      {data && (
        <div className="stat-grid">
          <div className="stat-item">
            <span className="stat-label">Active Sessions</span>
            <span className="stat-value stat-value--large">{data.iscsi.active_sessions}</span>
          </div>
          <div className="stat-item">
            <span className="stat-label">Read IOPS</span>
            <span className="stat-value">{formatOps(data.iscsi.read_ops_total)}</span>
          </div>
          <div className="stat-item">
            <span className="stat-label">Write IOPS</span>
            <span className="stat-value">{formatOps(data.iscsi.write_ops_total)}</span>
          </div>
          <div className="stat-item">
            <span className="stat-label">Read Throughput</span>
            <span className="stat-value">{formatBytes(data.iscsi.read_bytes_total)}</span>
          </div>
          <div className="stat-item">
            <span className="stat-label">Write Throughput</span>
            <span className="stat-value">{formatBytes(data.iscsi.write_bytes_total)}</span>
          </div>
          <div className="stat-item">
            <span className="stat-label">Read P99 Latency</span>
            <span className="stat-value">{data.iscsi.read_latency_p99_ms.toFixed(2)} ms</span>
          </div>
          <div className="stat-item">
            <span className="stat-label">Write P99 Latency</span>
            <span className="stat-value">{data.iscsi.write_latency_p99_ms.toFixed(2)} ms</span>
          </div>
        </div>
      )}
    </div>
  )
}
