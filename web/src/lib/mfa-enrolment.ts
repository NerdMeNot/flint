// Pre-session MFA enrolment.
//
// When a role requires MFA and the user has not enrolled, /auth/login returns an
// enrolment token instead of a session: enrolling needs a session, and login is
// the only place one is issued, so refusing outright was a dead end.
//
// These calls go straight to the Go API rather than through oRPC, because oRPC
// authenticates with a session token this user does not have yet. The token is
// narrow by construction — minted only after a correct password, valid for five
// minutes, consumed on completion, and refused by /auth/mfa/verify, so it can
// never be traded for a session on its own.

export interface MfaSetupResponse {
  secret: string
  qrCodeURL: string
  recoveryCodes: string[]
}

const ENROLMENT_HEADER = 'X-MFA-Enrolment-Token'

// The Go API returns { error: { code, message } }; surface the message so the
// user sees "invalid or expired enrolment token" rather than a status code.
async function readError(res: Response, fallback: string): Promise<string> {
  try {
    const data = (await res.json()) as { error?: { message?: string } | string }
    if (typeof data.error === 'string') return data.error
    if (data.error?.message) return data.error.message
  } catch {
    // Non-JSON body (e.g. a bare 429 from the rate limiter).
  }
  return fallback
}

export async function enrolmentSetup(token: string): Promise<MfaSetupResponse> {
  const res = await fetch('/auth/mfa/setup', {
    method: 'POST',
    headers: { [ENROLMENT_HEADER]: token },
  })
  if (!res.ok) {
    throw new Error(await readError(res, 'Could not start MFA setup'))
  }
  return (await res.json()) as MfaSetupResponse
}

export async function enrolmentVerifySetup(token: string, code: string): Promise<void> {
  const res = await fetch('/auth/mfa/setup/verify', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      [ENROLMENT_HEADER]: token,
    },
    body: JSON.stringify({ code }),
  })
  if (!res.ok) {
    throw new Error(await readError(res, 'Verification failed'))
  }
}
