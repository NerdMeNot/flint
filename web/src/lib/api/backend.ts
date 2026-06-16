// Backend HTTP client for calling the Flint Go server's REST API.
// Used by oRPC handlers (server-side) to proxy requests to the real backend.
//
// The client always talks to the Go server; mock vs live data is a server-side
// concern (`flint server --mock`). Errors are surfaced, never masked.

import { currentAuthHeader } from './server-token'

const BACKEND_URL = process.env.FLINT_BACKEND_URL || 'http://localhost:5000'

// Build request headers, forwarding the browser's Authorization (carried via the
// /api/rpc route's AsyncLocalStorage) to the Go API.
function authHeaders(): Record<string, string> {
  const h: Record<string, string> = { 'Content-Type': 'application/json' }
  const auth = currentAuthHeader()
  if (auth) h.Authorization = auth
  return h
}

async function doFetch<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, init)
  if (!res.ok) {
    const body = await res.text().catch(() => '')
    throw new Error(`Backend ${res.status}: ${body}`)
  }
  return res.json()
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
    headers: authHeaders(),
  })
}

export async function backendPost<T>(path: string, body?: unknown): Promise<T> {
  return doFetch<T>(`${BACKEND_URL}/api/v1${path}`, {
    method: 'POST',
    headers: authHeaders(),
    body: body ? JSON.stringify(body) : undefined,
  })
}

export async function backendPut<T>(path: string, body?: unknown): Promise<T> {
  return doFetch<T>(`${BACKEND_URL}/api/v1${path}`, {
    method: 'PUT',
    headers: authHeaders(),
    body: body ? JSON.stringify(body) : undefined,
  })
}

export async function backendPatch<T>(path: string, body?: unknown): Promise<T> {
  return doFetch<T>(`${BACKEND_URL}/api/v1${path}`, {
    method: 'PATCH',
    headers: authHeaders(),
    body: body ? JSON.stringify(body) : undefined,
  })
}

export async function backendDelete<T>(path: string): Promise<T> {
  return doFetch<T>(`${BACKEND_URL}/api/v1${path}`, {
    method: 'DELETE',
    headers: authHeaders(),
  })
}
