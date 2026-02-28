import { describe, it, expect, vi, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { usePolling } from '../usePolling'

describe('usePolling', () => {
  afterEach(() => {
    vi.restoreAllMocks()
    vi.clearAllMocks()
  })

  it('starts in loading state', () => {
    const fetcher = vi.fn().mockReturnValue(new Promise(() => {})) // never resolves
    const { result } = renderHook(() => usePolling(fetcher, 100000))

    expect(result.current.loading).toBe(true)
    expect(result.current.data).toBeNull()
    expect(result.current.error).toBeNull()
  })

  it('sets data after successful fetch', async () => {
    const mockData = { value: 42 }
    const fetcher = vi.fn().mockResolvedValue(mockData)
    const { result } = renderHook(() => usePolling(fetcher, 100000))

    await waitFor(() => {
      expect(result.current.data).toEqual(mockData)
    })

    expect(result.current.loading).toBe(false)
    expect(result.current.error).toBeNull()
  })

  it('sets error on fetch failure', async () => {
    const fetchError = new Error('Fetch failed')
    const fetcher = vi.fn().mockRejectedValue(fetchError)
    const { result } = renderHook(() => usePolling(fetcher, 100000))

    await waitFor(() => {
      expect(result.current.error?.message).toBe('Fetch failed')
    })

    expect(result.current.loading).toBe(false)
  })

  it('calls fetcher on mount', async () => {
    const fetcher = vi.fn().mockResolvedValue({ count: 0 })
    renderHook(() => usePolling(fetcher, 100000))

    await waitFor(() => {
      expect(fetcher).toHaveBeenCalledTimes(1)
    })
  })

  it('polls at specified interval', async () => {
    vi.useFakeTimers()
    try {
      const mockData = { count: 0 }
      const fetcher = vi.fn().mockResolvedValue(mockData)
      renderHook(() => usePolling(fetcher, 1000))

      // Initial fetch
      await vi.advanceTimersByTimeAsync(0)
      expect(fetcher).toHaveBeenCalledTimes(1)

      // Advance by interval
      await vi.advanceTimersByTimeAsync(1000)
      expect(fetcher).toHaveBeenCalledTimes(2)

      await vi.advanceTimersByTimeAsync(1000)
      expect(fetcher).toHaveBeenCalledTimes(3)
    } finally {
      vi.useRealTimers()
    }
  })

  it('stops polling after unmount', async () => {
    vi.useFakeTimers()
    try {
      const fetcher = vi.fn().mockResolvedValue({ value: 1 })
      const { unmount } = renderHook(() => usePolling(fetcher, 1000))

      await vi.advanceTimersByTimeAsync(0)
      const callCountAfterInit = fetcher.mock.calls.length

      unmount()

      await vi.advanceTimersByTimeAsync(3000)

      // No additional calls after unmount
      expect(fetcher).toHaveBeenCalledTimes(callCountAfterInit)
    } finally {
      vi.useRealTimers()
    }
  })

  it('handles non-Error rejections', async () => {
    const fetcher = vi.fn().mockRejectedValue('string error')
    const { result } = renderHook(() => usePolling(fetcher, 100000))

    await waitFor(() => {
      expect(result.current.error?.message).toBe('string error')
    })
  })

  it('updates data on subsequent successful polls', async () => {
    let callCount = 0
    const fetcher = vi.fn().mockImplementation(() => {
      callCount++
      return Promise.resolve({ count: callCount })
    })

    const { result } = renderHook(() => usePolling(fetcher, 50))

    // Wait for initial fetch
    await waitFor(() => {
      expect(result.current.data).toEqual({ count: 1 })
    })

    // Wait for second poll (interval is 50ms)
    await waitFor(
      () => {
        expect(result.current.data).toEqual({ count: 2 })
      },
      { timeout: 500 },
    )
  })
})
