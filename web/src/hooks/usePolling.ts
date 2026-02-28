import { useState, useEffect, useCallback, useRef } from 'react'

export interface PollingState<T> {
  data: T | null
  error: Error | null
  loading: boolean
}

export function usePolling<T>(
  fetcher: () => Promise<T>,
  intervalMs: number,
): PollingState<T> {
  const [state, setState] = useState<PollingState<T>>({
    data: null,
    error: null,
    loading: true,
  })

  const fetcherRef = useRef(fetcher)
  fetcherRef.current = fetcher

  const poll = useCallback(async () => {
    try {
      const data = await fetcherRef.current()
      setState({ data, error: null, loading: false })
    } catch (err) {
      setState(prev => ({
        ...prev,
        error: err instanceof Error ? err : new Error(String(err)),
        loading: false,
      }))
    }
  }, [])

  useEffect(() => {
    let cancelled = false

    const run = async () => {
      if (!cancelled) {
        await poll()
      }
    }

    run()

    const timer = setInterval(() => {
      if (!cancelled) {
        poll()
      }
    }, intervalMs)

    return () => {
      cancelled = true
      clearInterval(timer)
    }
  }, [poll, intervalMs])

  return state
}
