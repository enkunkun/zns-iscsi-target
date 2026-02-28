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

function getGaugeColor(ratio: number): string {
  if (ratio >= 0.9) return '#EF4444'
  if (ratio >= 0.7) return '#F59E0B'
  return '#22C55E'
}

export function BufferGauge() {
  const { data, error, loading } = usePolling(fetchStats, 1000)

  const ratio =
    data && data.buffer.max_bytes > 0
      ? data.buffer.dirty_bytes / data.buffer.max_bytes
      : 0

  const pct = (ratio * 100).toFixed(1)
  const color = getGaugeColor(ratio)

  return (
    <div className="panel">
      <div className="panel-header">
        <h2 className="panel-title">Write Buffer</h2>
        {data && (
          <span className="panel-meta">{pct}% full</span>
        )}
      </div>

      {loading && <div className="panel-loading">Loading...</div>}
      {error && <div className="panel-error">Error: {error.message}</div>}

      {data && (
        <>
          <div className="gauge-container">
            <div className="gauge-track">
              <div
                className="gauge-fill"
                style={{
                  width: `${Math.min(ratio * 100, 100)}%`,
                  backgroundColor: color,
                }}
                role="progressbar"
                aria-valuenow={ratio * 100}
                aria-valuemin={0}
                aria-valuemax={100}
                aria-label="Buffer usage"
              />
            </div>
            <div className="gauge-labels">
              <span className="gauge-label-left">0</span>
              <span className="gauge-label-right">{formatBytes(data.buffer.max_bytes)}</span>
            </div>
          </div>

          <div className="stat-grid">
            <div className="stat-item">
              <span className="stat-label">Dirty</span>
              <span className="stat-value" style={{ color }}>
                {formatBytes(data.buffer.dirty_bytes)}
              </span>
            </div>
            <div className="stat-item">
              <span className="stat-label">Max Capacity</span>
              <span className="stat-value">{formatBytes(data.buffer.max_bytes)}</span>
            </div>
            <div className="stat-item">
              <span className="stat-label">Pending Flushes</span>
              <span className="stat-value">{data.buffer.pending_flushes}</span>
            </div>
          </div>
        </>
      )}
    </div>
  )
}
