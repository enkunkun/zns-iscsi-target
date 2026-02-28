import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { render, screen, waitFor } from '@testing-library/react'
import { ZoneMap } from '../ZoneMap'
import * as client from '../../api/client'
import type { ZonesResponse } from '../../types/api'

vi.mock('../../api/client')

const mockZonesResponse: ZonesResponse = {
  zones: [
    {
      id: 0,
      state: 'EMPTY',
      type: 'SEQ_WRITE_REQUIRED',
      write_pointer_lba: 0,
      zone_start_lba: 0,
      zone_length_lba: 524288,
      valid_segments: 0,
      total_segments: 32768,
      gc_score: 0.0,
    },
    {
      id: 1,
      state: 'FULL',
      type: 'SEQ_WRITE_REQUIRED',
      write_pointer_lba: 1048576,
      zone_start_lba: 524288,
      zone_length_lba: 524288,
      valid_segments: 32768,
      total_segments: 32768,
      gc_score: 0.0,
    },
    {
      id: 2,
      state: 'IMPLICIT_OPEN',
      type: 'SEQ_WRITE_REQUIRED',
      write_pointer_lba: 1310720,
      zone_start_lba: 1048576,
      zone_length_lba: 524288,
      valid_segments: 16384,
      total_segments: 32768,
      gc_score: 0.1,
    },
    {
      id: 3,
      state: 'CLOSED',
      type: 'SEQ_WRITE_REQUIRED',
      write_pointer_lba: 1835008,
      zone_start_lba: 1572864,
      zone_length_lba: 524288,
      valid_segments: 10000,
      total_segments: 32768,
      gc_score: 0.2,
    },
    {
      id: 4,
      state: 'READ_ONLY',
      type: 'SEQ_WRITE_REQUIRED',
      write_pointer_lba: 2097152,
      zone_start_lba: 2097152,
      zone_length_lba: 524288,
      valid_segments: 32768,
      total_segments: 32768,
      gc_score: 0.0,
    },
    {
      id: 5,
      state: 'OFFLINE',
      type: 'SEQ_WRITE_REQUIRED',
      write_pointer_lba: 0,
      zone_start_lba: 2621440,
      zone_length_lba: 524288,
      valid_segments: 0,
      total_segments: 32768,
      gc_score: 0.0,
    },
  ],
  total: 6,
}

describe('ZoneMap', () => {
  beforeEach(() => {
    vi.mocked(client.fetchZones).mockResolvedValue(mockZonesResponse)
  })

  afterEach(() => {
    vi.restoreAllMocks()
    vi.clearAllMocks()
  })

  it('renders panel title', () => {
    render(<ZoneMap />)
    expect(screen.getByText('Zone Map')).toBeInTheDocument()
  })

  it('shows loading state initially', () => {
    render(<ZoneMap />)
    expect(screen.getByText('Loading zones...')).toBeInTheDocument()
  })

  it('renders all zones after data loads', async () => {
    render(<ZoneMap />)

    await waitFor(() => {
      const zoneCells = document.querySelectorAll('.zone-cell')
      expect(zoneCells).toHaveLength(6)
    })
  })

  it('renders zone total count', async () => {
    render(<ZoneMap />)

    await waitFor(() => {
      expect(screen.getByText('6 zones')).toBeInTheDocument()
    })
  })

  it('applies correct aria-label to zone cells', async () => {
    render(<ZoneMap />)

    await waitFor(() => {
      expect(screen.getByLabelText('Zone 0: EMPTY')).toBeInTheDocument()
    })

    expect(screen.getByLabelText('Zone 1: FULL')).toBeInTheDocument()
    expect(screen.getByLabelText('Zone 2: IMPLICIT_OPEN')).toBeInTheDocument()
    expect(screen.getByLabelText('Zone 3: CLOSED')).toBeInTheDocument()
    expect(screen.getByLabelText('Zone 4: READ_ONLY')).toBeInTheDocument()
    expect(screen.getByLabelText('Zone 5: OFFLINE')).toBeInTheDocument()
  })

  it('applies correct background colors based on zone state', async () => {
    render(<ZoneMap />)

    await waitFor(() => {
      expect(screen.getByLabelText('Zone 0: EMPTY')).toBeInTheDocument()
    })

    expect(screen.getByLabelText('Zone 0: EMPTY')).toHaveStyle({ backgroundColor: '#374151' })
    expect(screen.getByLabelText('Zone 1: FULL')).toHaveStyle({ backgroundColor: '#EF4444' })
    expect(screen.getByLabelText('Zone 2: IMPLICIT_OPEN')).toHaveStyle({ backgroundColor: '#3B82F6' })
    expect(screen.getByLabelText('Zone 3: CLOSED')).toHaveStyle({ backgroundColor: '#F59E0B' })
    expect(screen.getByLabelText('Zone 4: READ_ONLY')).toHaveStyle({ backgroundColor: '#8B5CF6' })
    expect(screen.getByLabelText('Zone 5: OFFLINE')).toHaveStyle({ backgroundColor: '#1F2937' })
  })

  it('applies minimum opacity (0.3) to empty zone with no valid segments', async () => {
    render(<ZoneMap />)

    await waitFor(() => {
      expect(screen.getByLabelText('Zone 0: EMPTY')).toBeInTheDocument()
    })

    const emptyCell = screen.getByLabelText('Zone 0: EMPTY')
    // Zone 0 has 0 valid_segments / 32768 total = 0.3 opacity
    expect(emptyCell).toHaveStyle({ opacity: '0.3' })
  })

  it('applies maximum opacity (1.0) to full zone', async () => {
    render(<ZoneMap />)

    await waitFor(() => {
      expect(screen.getByLabelText('Zone 1: FULL')).toBeInTheDocument()
    })

    const fullCell = screen.getByLabelText('Zone 1: FULL')
    // Zone 1 has 32768 valid_segments / 32768 total = 1.0 opacity
    expect(fullCell).toHaveStyle({ opacity: '1' })
  })

  it('renders all legend items', async () => {
    render(<ZoneMap />)

    await waitFor(() => {
      expect(screen.getByText('Empty')).toBeInTheDocument()
    })

    expect(screen.getByText('Implicit Open')).toBeInTheDocument()
    expect(screen.getByText('Explicit Open')).toBeInTheDocument()
    expect(screen.getByText('Closed')).toBeInTheDocument()
    expect(screen.getByText('Full')).toBeInTheDocument()
    expect(screen.getByText('Read Only')).toBeInTheDocument()
    expect(screen.getByText('Offline')).toBeInTheDocument()
  })

  it('shows tooltip on zone hover', async () => {
    render(<ZoneMap />)

    await waitFor(() => {
      expect(screen.getByLabelText('Zone 0: EMPTY')).toBeInTheDocument()
    })

    const cell = screen.getByLabelText('Zone 0: EMPTY')

    cell.dispatchEvent(
      new MouseEvent('mousemove', {
        bubbles: true,
        clientX: 100,
        clientY: 100,
      }),
    )

    await waitFor(() => {
      // Tooltip should appear with zone info
      const tooltipRows = document.querySelectorAll('.tooltip-row')
      expect(tooltipRows.length).toBeGreaterThan(0)
    })
  })

  it('shows error message when fetch fails', async () => {
    vi.mocked(client.fetchZones).mockRejectedValue(new Error('Connection refused'))
    render(<ZoneMap />)

    await waitFor(() => {
      expect(screen.getByText(/Connection refused/)).toBeInTheDocument()
    })
  })

  it('calls fetchZones on mount', async () => {
    render(<ZoneMap />)

    await waitFor(() => {
      expect(client.fetchZones).toHaveBeenCalledTimes(1)
    })
  })
})
