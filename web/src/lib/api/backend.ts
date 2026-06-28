// Backend HTTP client for calling the Flint Go server's REST API.
// Used by oRPC handlers (server-side) to proxy requests to the real backend.
//
// The client always talks to the Go server; mock vs live data is a server-side
// concern (`flint server --mock`). Errors are surfaced, never masked.

import { getCookie } from '@tanstack/react-start/server'
import { currentAuthHeader } from './server-token'
import { ACCESS_COOKIE } from '#/lib/auth-token'

const BACKEND_URL = process.env.FLINT_BACKEND_URL || 'http://localhost:5000'

// This module runs server-side only: the browser oRPC link calls /api/rpc, whose
// handler code (this file) executes on the server, as does the SSR render. So
// reading the request cookie here is safe.
//
// Auth precedence: the Authorization header relayed by /api/rpc (set by the
// browser link from localStorage) wins; on the initial SSR document request
// there is no such header, so fall back to the session cookie.
function authHeaders(): Record<string, string> {
  const h: Record<string, string> = { 'Content-Type': 'application/json' }
  let auth = currentAuthHeader()
  if (!auth) {
    try {
      const token = getCookie(ACCESS_COOKIE)
      if (token) auth = `Bearer ${token}`
    } catch {
      // no request context (non-request server call) — leave unauthenticated
    }
  }
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

// Root-path variants for endpoints the Go server mounts OUTSIDE /api/v1 — namely
// the session/auth routes (/auth/me, /auth/profile, /auth/sessions, /auth/mfa/*,
// /auth/change-password). Note: /auth/providers and /auth/provider ARE under
// /api/v1, so those keep using the functions above.
export async function backendGetRoot<T>(path: string): Promise<T> {
  return doFetch<T>(`${BACKEND_URL}${path}`, { headers: authHeaders() })
}

export async function backendPostRoot<T>(path: string, body?: unknown): Promise<T> {
  return doFetch<T>(`${BACKEND_URL}${path}`, {
    method: 'POST',
    headers: authHeaders(),
    body: body ? JSON.stringify(body) : undefined,
  })
}

export async function backendPutRoot<T>(path: string, body?: unknown): Promise<T> {
  return doFetch<T>(`${BACKEND_URL}${path}`, {
    method: 'PUT',
    headers: authHeaders(),
    body: body ? JSON.stringify(body) : undefined,
  })
}

export async function backendDeleteRoot<T>(path: string): Promise<T> {
  return doFetch<T>(`${BACKEND_URL}${path}`, {
    method: 'DELETE',
    headers: authHeaders(),
  })
}
