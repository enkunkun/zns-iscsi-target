import { describe, it, expect, vi, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { GCStats } from '../GCStats'
import * as client from '../../api/client'
import type { StatsResponse } from '../../types/api'

vi.mock('../../api/client')

const makeStats = (overrides: Partial<StatsResponse['gc']> = {}): StatsResponse => ({
  iscsi: {
    active_sessions: 0,
    read_bytes_total: 0,
    write_bytes_total: 0,
    read_ops_total: 0,
    write_ops_total: 0,
    read_latency_p99_ms: 0,
    write_latency_p99_ms: 0,
  },
  gc: {
    running: false,
    zones_reclaimed: 0,
    bytes_migrated: 0,
    run_count: 0,
    ...overrides,
  },
  buffer: {
    dirty_bytes: 0,
    max_bytes: 536870912,
    pending_flushes: 0,
  },
  journal: {
    current_lsn: 0,
    checkpoint_lsn: 0,
    size_bytes: 0,
  },
  device: {
    backend: 'emulator',
    path: '',
    zone_count: 64,
    zone_size_mb: 256,
    total_capacity_bytes: 17179869184,
    max_open_zones: 14,
  },
})

describe('GCStats', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.clearAllMocks()
  })

  it('renders panel title', () => {
    vi.mocked(client.fetchStats).mockResolvedValue(makeStats())
    render(<GCStats />)
    expect(screen.getByText('Garbage Collection')).toBeInTheDocument()
  })

  it('shows loading state initially', () => {
    vi.mocked(client.fetchStats).mockResolvedValue(makeStats())
    render(<GCStats />)
    expect(screen.getByText('Loading...')).toBeInTheDocument()
  })

  it('renders idle status when GC is not running', async () => {
    vi.mocked(client.fetchStats).mockResolvedValue(makeStats({ running: false }))
    render(<GCStats />)

    await waitFor(() => {
      const idleBadges = screen.getAllByText('Idle')
      expect(idleBadges.length).toBeGreaterThan(0)
    })
  })

  it('renders running status when GC is running', async () => {
    vi.mocked(client.fetchStats).mockResolvedValue(makeStats({ running: true }))
    render(<GCStats />)

    await waitFor(() => {
      const runningElements = screen.getAllByText('Running')
      expect(runningElements.length).toBeGreaterThan(0)
    })
  })

  it('renders zones reclaimed count', async () => {
    vi.mocked(client.fetchStats).mockResolvedValue(makeStats({ zones_reclaimed: 15 }))
    render(<GCStats />)

    await waitFor(() => {
      expect(screen.getByText('15')).toBeInTheDocument()
    })
  })

  it('renders bytes migrated formatted as GB', async () => {
    vi.mocked(client.fetchStats).mockResolvedValue(
      makeStats({ bytes_migrated: 1073741824 }),
    )
    render(<GCStats />)

    await waitFor(() => {
      expect(screen.getByText('1.00 GB')).toBeInTheDocument()
    })
  })

  it('renders run count', async () => {
    vi.mocked(client.fetchStats).mockResolvedValue(makeStats({ run_count: 42 }))
    render(<GCStats />)

    await waitFor(() => {
      expect(screen.getByText('42')).toBeInTheDocument()
    })
  })

  it('renders free zone ratio', async () => {
    // zones_reclaimed=16 out of zone_count=64 = 25%
    vi.mocked(client.fetchStats).mockResolvedValue(makeStats({ zones_reclaimed: 16 }))
    render(<GCStats />)

    await waitFor(() => {
      expect(screen.getByText('25.0%')).toBeInTheDocument()
    })
  })

  it('formats bytes migrated as MB for mid-range values', async () => {
    vi.mocked(client.fetchStats).mockResolvedValue(
      makeStats({ bytes_migrated: 52428800 }), // 50 MB
    )
    render(<GCStats />)

    await waitFor(() => {
      expect(screen.getByText('50.00 MB')).toBeInTheDocument()
    })
  })

  it('shows zero bytes migrated as bytes', async () => {
    vi.mocked(client.fetchStats).mockResolvedValue(makeStats({ bytes_migrated: 0 }))
    render(<GCStats />)

    await waitFor(() => {
      expect(screen.getByText('0 B')).toBeInTheDocument()
    })
  })

  it('shows error message when fetch fails', async () => {
    vi.mocked(client.fetchStats).mockRejectedValue(new Error('Server error'))
    render(<GCStats />)

    await waitFor(() => {
      expect(screen.getByText(/Server error/)).toBeInTheDocument()
    })
  })

  it('calls fetchStats on mount', async () => {
    vi.mocked(client.fetchStats).mockResolvedValue(makeStats())
    render(<GCStats />)

    await waitFor(() => {
      expect(client.fetchStats).toHaveBeenCalledTimes(1)
    })
  })
})
