// Backend HTTP client for calling the Flint Go server's REST API.
// Used by oRPC handlers (server-side) to proxy requests to the real backend.
//
// When the backend is unreachable, throws BackendUnavailableError.
// The router catches this and falls back to mock data.

import { apiMode } from './mode'

const BACKEND_URL = process.env.FLINT_BACKEND_URL || 'http://localhost:5000'

// Whether the most recent backend attempt found it unreachable. Only meaningful
// in 'auto' mode (in 'mock' we never try; in 'live' we never fall back). Exposed
// via the meta endpoint so the UI can show a "showing demo data" banner.
let backendUnreachable = false
export function isBackendUnreachable(): boolean {
  return backendUnreachable
}

export class BackendUnavailableError extends Error {
  constructor() {
    super('Backend unavailable')
    this.name = 'BackendUnavailableError'
  }
}

async function doFetch<T>(url: string, init?: RequestInit): Promise<T> {
  // Mock mode never touches the network — the router serves the mock store.
  if (apiMode() === 'mock') {
    throw new BackendUnavailableError()
  }

  try {
    const res = await fetch(url, init)
    if (!res.ok) {
      const body = await res.text().catch(() => '')
      // 401/403 are auth problems, not unreachability — surface them (they must
      // not masquerade as "backend down" and silently flip to demo data).
      throw new Error(`Backend ${res.status}: ${body}`)
    }
    backendUnreachable = false
    return res.json()
  } catch (err: any) {
    if (err instanceof BackendUnavailableError) throw err
    if (
      err.cause?.code === 'ECONNREFUSED' ||
      err.message?.includes('fetch failed') ||
      err.message?.includes('ECONNREFUSED') ||
      err.message?.includes('ENOTFOUND')
    ) {
      // Genuine network unreachability. In 'auto' the router falls back to mock
      // data; in 'live' the router rethrows so the UI shows a real error.
      backendUnreachable = true
      throw new BackendUnavailableError()
    }
    throw err
  }
}

export async function backendGet<T>(
  path: string,
  params?: Record<string, string | number | string[] | undefined>,
): Promise<T> {
  const url = new URL(`${BACKEND_URL}/api/v1${path}`)
  if (params) {
    for (const [k, v] of Object.entries(params)) {
      if (v === undefined || v === '') continue
      if (Array.isArray(v)) {
        for (const item of v) url.searchParams.append(k, String(item))
      } else {
        url.searchParams.set(k, String(v))
      }
    }
  }
  return doFetch<T>(url.toString(), {
    headers: { 'Content-Type': 'application/json' },
  })
}

export async function backendPost<T>(path: string, body?: unknown): Promise<T> {
  return doFetch<T>(`${BACKEND_URL}/api/v1${path}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined,
  })
}

export async function backendPut<T>(path: string, body?: unknown): Promise<T> {
  return doFetch<T>(`${BACKEND_URL}/api/v1${path}`, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined,
  })
}

export async function backendPatch<T>(path: string, body?: unknown): Promise<T> {
  return doFetch<T>(`${BACKEND_URL}/api/v1${path}`, {
    method: 'PATCH',
    headers: { 'Content-Type': 'application/json' },
    body: body ? JSON.stringify(body) : undefined,
  })
}

export async function backendDelete<T>(path: string): Promise<T> {
  return doFetch<T>(`${BACKEND_URL}/api/v1${path}`, {
    method: 'DELETE',
    headers: { 'Content-Type': 'application/json' },
  })
}
