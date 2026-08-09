package server

import (
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/jackc/pgx/v5"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
)

func ptr[T any](v T) *T { return &v }

// ── Device token polling (DB-backed, atomic claim) ──────────

func TestHandleDeviceToken_SlowDownOnFastPoll(t *testing.T) {
	srv, m := testServer(t)

	recent := time.Now().Add(-1 * time.Second) // polled 1s ago, interval 5s
	m.Querier.On("GetDeviceCode", mock.Anything, "dc").Return(db.GetDeviceCodeRow{
		Completed:    false,
		ExpiresAt:    time.Now().Add(10 * time.Minute),
		LastPolledAt: &recent,
		IntervalSecs: 5,
	}, nil)

	w := ut.PerformRequest(srv.Engine(), "POST", "/auth/device/token",
		jsonBody(t, map[string]any{"deviceCode": "dc"}),
		ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "SLOW_DOWN")
}

func TestHandleDeviceToken_AtomicClaimReturnsTokens(t *testing.T) {
	srv, m := testServer(t)

	m.Querier.On("GetDeviceCode", mock.Anything, "dc").Return(db.GetDeviceCodeRow{
		Completed:    true,
		ExpiresAt:    time.Now().Add(10 * time.Minute),
		LastPolledAt: nil,
		IntervalSecs: 5,
	}, nil)
	m.Querier.On("TouchDeviceCodePoll", mock.Anything, "dc").Return(nil)
	m.Querier.On("ClaimCompletedDeviceCode", mock.Anything, "dc").Return(db.ClaimCompletedDeviceCodeRow{
		AccessToken:  ptr("access-xyz"),
		RefreshToken: ptr("refresh-xyz"),
	}, nil)

	w := ut.PerformRequest(srv.Engine(), "POST", "/auth/device/token",
		jsonBody(t, map[string]any{"deviceCode": "dc"}),
		ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.Equal(t, 200, w.Code)
	resp := parseJSON(t, w)
	assert.Equal(t, "access-xyz", resp["accessToken"])
	assert.Equal(t, "refresh-xyz", resp["refreshToken"])
}

func TestHandleDeviceToken_ClaimRaceLostStaysPending(t *testing.T) {
	srv, m := testServer(t)

	m.Querier.On("GetDeviceCode", mock.Anything, "dc").Return(db.GetDeviceCodeRow{
		Completed: true, ExpiresAt: time.Now().Add(10 * time.Minute), IntervalSecs: 5,
	}, nil)
	m.Querier.On("TouchDeviceCodePoll", mock.Anything, "dc").Return(nil)
	// Another poll already claimed+deleted the row → no rows.
	m.Querier.On("ClaimCompletedDeviceCode", mock.Anything, "dc").
		Return(db.ClaimCompletedDeviceCodeRow{}, pgx.ErrNoRows)

	w := ut.PerformRequest(srv.Engine(), "POST", "/auth/device/token",
		jsonBody(t, map[string]any{"deviceCode": "dc"}),
		ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.Equal(t, 400, w.Code)
	assert.Contains(t, w.Body.String(), "AUTHORIZATION_PENDING")
}

// ── TOTP replay protection ──────────────────────────────────

func TestHandleMFAVerify_TOTPReplayRejected(t *testing.T) {
	srv, m := testServer(t)

	// A real TOTP secret + a currently-valid code, so ValidateTOTPCode passes and
	// the only thing failing the request is the replay guard.
	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Flint", AccountName: "u@x.io"})
	require.NoError(t, err)
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	require.NoError(t, err)

	m.Querier.On("GetMFAPendingToken", mock.Anything, "mfa-tok").Return(db.GetMFAPendingTokenRow{
		UserID: "u1", Email: "u@x.io", OrgID: "org-1",
	}, nil)
	m.Querier.On("GetUserForAuth", mock.Anything, mock.Anything).Return(db.GetUserForAuthRow{
		ID: "u1", Email: "u@x.io", OrgID: "org-1", TotpSecretEnc: []byte(key.Secret()),
	}, nil)
	// RecordTOTPUse returns no row → this period was already used → replay.
	m.Querier.On("RecordTOTPUse", mock.Anything, mock.Anything).Return("", pgx.ErrNoRows)

	w := ut.PerformRequest(srv.Engine(), "POST", "/auth/mfa/verify",
		jsonBody(t, map[string]any{"mfaToken": "mfa-tok", "code": code}),
		ut.Header{Key: "Content-Type", Value: "application/json"})

	assert.Equal(t, 401, w.Code, "a replayed TOTP code must be rejected")
}
