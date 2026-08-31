// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'

// vi.mock factories are hoisted above the module body, so the spies live in a
// hoisted block rather than as plain top-level consts.
const mocks = vi.hoisted(() => ({
  navigate: vi.fn(),
  orpcSetup: vi.fn(),
  orpcVerifySetup: vi.fn(),
}))

// Navigation is the router's job, not this page's; stub it so the component can
// render outside a router.
vi.mock('@tanstack/react-router', () => ({
  useNavigate: () => mocks.navigate,
}))

// The oRPC client authenticates with a session, which is exactly what this flow
// does not have. Stubbing it keeps the test on the enrolment transport and
// proves the session path is never taken.
vi.mock('#/lib/orpc', () => ({
  client: { auth: { mfa: { setup: mocks.orpcSetup, verifySetup: mocks.orpcVerifySetup } } },
  orpc: { auth: { me: { key: () => ['auth', 'me'] } } },
}))

import { LoginPage } from './LoginPage'

const ENROLMENT_HEADER = 'X-MFA-Enrolment-Token'

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

let fetchMock: ReturnType<typeof vi.fn>

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(
    <QueryClientProvider client={qc}>
      <LoginPage />
    </QueryClientProvider>,
  )
}

function type(el: HTMLElement, value: string) {
  fireEvent.change(el, { target: { value } })
}

function signIn() {
  type(screen.getByPlaceholderText('you@company.dev'), 'u@x.io')
  type(screen.getByPlaceholderText('Enter your password'), 'pw')
  fireEvent.click(screen.getByRole('button', { name: /^sign in$/i }))
}

beforeEach(() => {
  mocks.navigate.mockClear()
  mocks.orpcSetup.mockClear()
  mocks.orpcVerifySetup.mockClear()
  fetchMock = vi.fn()
  vi.stubGlobal('fetch', fetchMock)
})

afterEach(() => {
  // Auto-cleanup only runs when vitest globals are enabled; this project does
  // not enable them, so unmount explicitly or the next render finds two pages.
  cleanup()
  vi.unstubAllGlobals()
})

describe('sign-in with a role that requires MFA', () => {
  // The whole point of the enrolment token: a user who must enrol can, instead
  // of being refused a session they need in order to enrol.
  it('runs the enrolment wizard on the token, not a session', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/auth/login') {
        return Promise.resolve(json({ mfaSetupRequired: true, enrolmentToken: 'enrol-tok' }))
      }
      if (url === '/auth/mfa/setup') {
        return Promise.resolve(
          json({ secret: 'JBSWY3DPEHPK3PXP', qrCodeURL: 'otpauth://x', recoveryCodes: ['AAAA-BBBB-CCCC-DDDD'] }),
        )
      }
      if (url === '/auth/mfa/setup/verify') {
        return Promise.resolve(json({ status: 'mfa_enabled' }))
      }
      throw new Error(`unexpected fetch: ${url}`)
    })

    renderPage()
    signIn()

    // The page switched into enrolment, and the wizard started against the API
    // directly with the token — not through the session-authenticated client.
    await waitFor(() => {
      expect(screen.getByText('Set Up Two-Factor Authentication')).toBeTruthy()
    })
    await waitFor(() => {
      const setupCall = fetchMock.mock.calls.find((c) => c[0] === '/auth/mfa/setup')
      expect(setupCall).toBeTruthy()
      expect(setupCall![1].headers[ENROLMENT_HEADER]).toBe('enrol-tok')
    })
    expect(mocks.orpcSetup).not.toHaveBeenCalled()

    // Scan → verify.
    fireEvent.click(screen.getByRole('button', { name: /continue/i }))
    type(screen.getByPlaceholderText('000000'), '123456')
    fireEvent.click(screen.getByRole('button', { name: /verify & enable mfa/i }))

    await waitFor(() => {
      const verifyCall = fetchMock.mock.calls.find((c) => c[0] === '/auth/mfa/setup/verify')
      expect(verifyCall).toBeTruthy()
      expect(verifyCall![1].headers[ENROLMENT_HEADER]).toBe('enrol-tok')
      expect(JSON.parse(verifyCall![1].body)).toEqual({ code: '123456' })
    })
    expect(mocks.orpcVerifySetup).not.toHaveBeenCalled()

    // Recovery codes, then the hand-off back to sign-in. Enrolment must NOT end
    // in a session: the token proved a password, not a second factor.
    await waitFor(() => {
      expect(screen.getByText('AAAA-BBBB-CCCC-DDDD')).toBeTruthy()
    })
    fireEvent.click(screen.getByRole('button', { name: /saved my recovery codes/i }))

    await waitFor(() => {
      expect(screen.getByText('Two-Factor Authentication Enabled')).toBeTruthy()
    })
    expect(mocks.navigate).not.toHaveBeenCalled()
  })

  // A user who has already enrolled takes the ordinary challenge, unchanged.
  it('still shows the normal challenge when MFA is set up', async () => {
    fetchMock.mockResolvedValue(json({ mfaRequired: true, mfaToken: 'verify-tok' }))

    renderPage()
    signIn()

    await waitFor(() => {
      expect(screen.getByText('Two-Factor Authentication')).toBeTruthy()
    })
    expect(screen.getByPlaceholderText('000000')).toBeTruthy()
    expect(fetchMock.mock.calls.every((c) => c[0] !== '/auth/mfa/setup')).toBe(true)
  })

  // An expired or wrong-purpose token has to surface the server's reason rather
  // than leaving the wizard spinning.
  it('surfaces a rejected enrolment token', async () => {
    fetchMock.mockImplementation((url: string) => {
      if (url === '/auth/login') {
        return Promise.resolve(json({ mfaSetupRequired: true, enrolmentToken: 'stale' }))
      }
      return Promise.resolve(
        json({ error: { message: 'invalid or expired enrolment token' } }, 401),
      )
    })

    renderPage()
    signIn()

    await waitFor(() => {
      expect(screen.getByText('invalid or expired enrolment token')).toBeTruthy()
    })
  })
})
