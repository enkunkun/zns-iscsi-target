import type { ZonesResponse, StatsResponse, HealthResponse } from '../types/api'

export class ApiError extends Error {
  readonly status: number

  constructor(status: number, message: string) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

async function fetchJson<T>(url: string): Promise<T> {
  const response = await fetch(url)
  if (!response.ok) {
    throw new ApiError(response.status, `HTTP ${response.status}: ${response.statusText}`)
  }
  return response.json() as Promise<T>
}

export async function fetchZones(): Promise<ZonesResponse> {
  return fetchJson<ZonesResponse>('/api/v1/zones')
}

export async function fetchStats(): Promise<StatsResponse> {
  return fetchJson<StatsResponse>('/api/v1/stats')
}

export async function fetchHealth(): Promise<HealthResponse> {
  return fetchJson<HealthResponse>('/api/v1/health')
}
