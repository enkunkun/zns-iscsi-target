import { useState } from 'react'
import { usePolling } from '../hooks/usePolling'
import { fetchZones } from '../api/client'
import type { Zone, ZoneState } from '../types/api'

const STATE_COLORS: Record<ZoneState, string> = {
  EMPTY: '#374151',
  IMPLICIT_OPEN: '#3B82F6',
  EXPLICIT_OPEN: '#2563EB',
  CLOSED: '#F59E0B',
  FULL: '#EF4444',
  READ_ONLY: '#8B5CF6',
  OFFLINE: '#1F2937',
}

function getZoneColor(zone: Zone): string {
  return STATE_COLORS[zone.state] ?? '#374151'
}

function getZoneOpacity(zone: Zone): number {
  if (zone.total_segments === 0) return 0.3
  const ratio = zone.valid_segments / zone.total_segments
  return 0.3 + ratio * 0.7
}

function formatLba(lba: number, zoneLengthLba: number): string {
  if (zoneLengthLba === 0) return '0%'
  const pct = ((lba / zoneLengthLba) * 100).toFixed(1)
  return `${pct}%`
}

interface TooltipProps {
  zone: Zone
  x: number
  y: number
}

function Tooltip({ zone, x, y }: TooltipProps) {
  const validPct =
    zone.total_segments > 0
      ? ((zone.valid_segments / zone.total_segments) * 100).toFixed(1)
      : '0.0'

  const wpRelative = zone.write_pointer_lba - zone.zone_start_lba
  const wpPct = formatLba(wpRelative, zone.zone_length_lba)

  return (
    <div
      className="zone-tooltip"
      style={{
        position: 'fixed',
        left: x + 12,
        top: y - 10,
        pointerEvents: 'none',
      }}
    >
      <div className="tooltip-row">
        <span className="tooltip-label">Zone</span>
        <span className="tooltip-value">{zone.id}</span>
      </div>
      <div className="tooltip-row">
        <span className="tooltip-label">State</span>
        <span className="tooltip-value">{zone.state}</span>
      </div>
      <div className="tooltip-row">
        <span className="tooltip-label">Write Ptr</span>
        <span className="tooltip-value">{wpPct}</span>
      </div>
      <div className="tooltip-row">
        <span className="tooltip-label">Valid</span>
        <span className="tooltip-value">{validPct}%</span>
      </div>
    </div>
  )
}

interface ZoneCellProps {
  zone: Zone
  onHover: (zone: Zone | null, x: number, y: number) => void
}

function ZoneCell({ zone, onHover }: ZoneCellProps) {
  const color = getZoneColor(zone)
  const opacity = getZoneOpacity(zone)

  return (
    <div
      className="zone-cell"
      style={{ backgroundColor: color, opacity }}
      onMouseMove={e => onHover(zone, e.clientX, e.clientY)}
      onMouseLeave={() => onHover(null, 0, 0)}
      aria-label={`Zone ${zone.id}: ${zone.state}`}
      title={`Zone ${zone.id}`}
    />
  )
}

interface LegendItem {
  state: ZoneState
  label: string
}

const LEGEND_ITEMS: LegendItem[] = [
  { state: 'EMPTY', label: 'Empty' },
  { state: 'IMPLICIT_OPEN', label: 'Implicit Open' },
  { state: 'EXPLICIT_OPEN', label: 'Explicit Open' },
  { state: 'CLOSED', label: 'Closed' },
  { state: 'FULL', label: 'Full' },
  { state: 'READ_ONLY', label: 'Read Only' },
  { state: 'OFFLINE', label: 'Offline' },
]

export function ZoneMap() {
  const { data, error, loading } = usePolling(fetchZones, 5000)
  const [tooltip, setTooltip] = useState<{ zone: Zone; x: number; y: number } | null>(null)

  const handleHover = (zone: Zone | null, x: number, y: number) => {
    if (zone) {
      setTooltip({ zone, x, y })
    } else {
      setTooltip(null)
    }
  }

  return (
    <div className="panel zone-map-panel">
      <div className="panel-header">
        <h2 className="panel-title">Zone Map</h2>
        {data && (
          <span className="panel-meta">
            {data.total} zones
          </span>
        )}
      </div>

      {loading && <div className="panel-loading">Loading zones...</div>}
      {error && (
        <div className="panel-error">Error: {error.message}</div>
      )}

      {data && (
        <>
          <div className="zone-grid">
            {data.zones.map(zone => (
              <ZoneCell key={zone.id} zone={zone} onHover={handleHover} />
            ))}
          </div>
          <div className="zone-legend">
            {LEGEND_ITEMS.map(item => (
              <div key={item.state} className="legend-item">
                <span
                  className="legend-swatch"
                  style={{ backgroundColor: STATE_COLORS[item.state] }}
                />
                <span className="legend-label">{item.label}</span>
              </div>
            ))}
          </div>
        </>
      )}

      {tooltip && (
        <Tooltip zone={tooltip.zone} x={tooltip.x} y={tooltip.y} />
      )}
    </div>
  )
}
