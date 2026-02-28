import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { fetchZones, fetchStats, fetchHealth, ApiError } from '../client'
import type { ZonesResponse, StatsResponse, HealthResponse } from '../../types/api'

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
      write_pointer_lba: 524288,
      zone_start_lba: 524288,
      zone_length_lba: 524288,
      valid_segments: 32768,
      total_segments: 32768,
      gc_score: 0.5,
    },
  ],
  total: 64,
}

const mockStatsResponse: StatsResponse = {
  iscsi: {
    active_sessions: 2,
    read_bytes_total: 1024 * 1024,
    write_bytes_total: 2048 * 1024,
    read_ops_total: 100,
    write_ops_total: 200,
    read_latency_p99_ms: 1.5,
    write_latency_p99_ms: 2.3,
  },
  gc: {
    running: true,
    zones_reclaimed: 5,
    bytes_migrated: 1024 * 1024 * 100,
    run_count: 10,
  },
  buffer: {
    dirty_bytes: 1024 * 1024 * 50,
    max_bytes: 536870912,
    pending_flushes: 3,
  },
  journal: {
    current_lsn: 1000,
    checkpoint_lsn: 900,
    size_bytes: 1024 * 1024,
  },
  device: {
    backend: 'emulator',
    path: '',
    zone_count: 64,
    zone_size_mb: 256,
    total_capacity_bytes: 17179869184,
    max_open_zones: 14,
  },
}

describe('API Client', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  describe('fetchZones', () => {
    it('returns parsed zones response on success', async () => {
      vi.mocked(fetch).mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(mockZonesResponse),
      } as Response)

      const result = await fetchZones()
      expect(result.zones).toHaveLength(2)
      expect(result.total).toBe(64)
      expect(result.zones[0].state).toBe('EMPTY')
      expect(result.zones[1].state).toBe('FULL')
      expect(fetch).toHaveBeenCalledWith('/api/v1/zones')
    })

    it('throws ApiError on non-ok response', async () => {
      vi.mocked(fetch).mockResolvedValue({
        ok: false,
        status: 503,
        statusText: 'Service Unavailable',
      } as Response)

      try {
        await fetchZones()
        expect.fail('Expected ApiError to be thrown')
      } catch (e) {
        expect(e).toBeInstanceOf(ApiError)
        if (e instanceof ApiError) {
          expect(e.status).toBe(503)
        }
      }
    })

    it('propagates network errors', async () => {
      vi.mocked(fetch).mockRejectedValueOnce(new Error('Network error'))
      await expect(fetchZones()).rejects.toThrow('Network error')
    })
  })

  describe('fetchStats', () => {
    it('returns parsed stats response on success', async () => {
      vi.mocked(fetch).mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(mockStatsResponse),
      } as Response)

      const result = await fetchStats()
      expect(result.iscsi.active_sessions).toBe(2)
      expect(result.gc.running).toBe(true)
      expect(result.buffer.max_bytes).toBe(536870912)
      expect(result.device.backend).toBe('emulator')
      expect(fetch).toHaveBeenCalledWith('/api/v1/stats')
    })

    it('throws ApiError on 404', async () => {
      vi.mocked(fetch).mockResolvedValueOnce({
        ok: false,
        status: 404,
        statusText: 'Not Found',
      } as Response)

      await expect(fetchStats()).rejects.toBeInstanceOf(ApiError)
    })
  })

  describe('fetchHealth', () => {
    it('returns health response', async () => {
      const healthResponse: HealthResponse = { status: 'ok' }
      vi.mocked(fetch).mockResolvedValueOnce({
        ok: true,
        json: () => Promise.resolve(healthResponse),
      } as Response)

      const result = await fetchHealth()
      expect(result.status).toBe('ok')
      expect(fetch).toHaveBeenCalledWith('/api/v1/health')
    })
  })

  describe('ApiError', () => {
    it('has correct name and message', () => {
      const err = new ApiError(500, 'Internal Server Error')
      expect(err.name).toBe('ApiError')
      expect(err.status).toBe(500)
      expect(err.message).toBe('Internal Server Error')
      expect(err instanceof Error).toBe(true)
    })
  })
})
