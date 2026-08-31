import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import { enrolmentSetup, enrolmentVerifySetup } from './mfa-enrolment'

const HEADER = 'X-MFA-Enrolment-Token'

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

let fetchMock: ReturnType<typeof vi.fn>

beforeEach(() => {
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('enrolmentSetup', () => {
  // The token is the only credential this request has — the user has no session
  // yet, which is the entire reason the enrolment flow exists. Sending it in the
  // Authorization header instead would be rejected as a malformed bearer token.
  it('sends the enrolment token in its own header', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(200, { secret: 'S', qrCodeURL: 'otpauth://x', recoveryCodes: ['A', 'B'] }),
    )

    const data = await enrolmentSetup('tok-123')

    expect(data.secret).toBe('S')
    expect(data.recoveryCodes).toEqual(['A', 'B'])

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/auth/mfa/setup')
    expect(init.method).toBe('POST')
    expect(init.headers[HEADER]).toBe('tok-123')
    expect(init.headers.Authorization).toBeUndefined()
  })

  // An expired or wrong-purpose token comes back as a structured API error; the
  // user needs to be told to sign in again, not shown a bare status code.
  it('surfaces the server message on rejection', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(401, { error: { code: 'UNAUTHORIZED', message: 'invalid or expired enrolment token' } }),
    )

    await expect(enrolmentSetup('stale')).rejects.toThrow('invalid or expired enrolment token')
  })

  // The rate limiter aborts with an empty body, so error extraction must not
  // assume JSON.
  it('falls back to a readable message when the body is not JSON', async () => {
    fetchMock.mockResolvedValue(new Response('', { status: 429 }))

    await expect(enrolmentSetup('tok')).rejects.toThrow('Could not start MFA setup')
  })
})

describe('enrolmentVerifySetup', () => {
  it('posts the code with the enrolment token', async () => {
    fetchMock.mockResolvedValue(jsonResponse(200, { status: 'mfa_enabled' }))

    await enrolmentVerifySetup('tok-123', '123456')

    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe('/auth/mfa/setup/verify')
    expect(init.headers[HEADER]).toBe('tok-123')
    expect(JSON.parse(init.body)).toEqual({ code: '123456' })
  })

  it('throws with the server message on a bad code', async () => {
    fetchMock.mockResolvedValue(
      jsonResponse(400, { error: { message: 'invalid code — scan the QR code and try again' } }),
    )

    await expect(enrolmentVerifySetup('tok', '000000')).rejects.toThrow('invalid code')
  })
})
