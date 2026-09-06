package server

import (
	"fmt"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/pquerna/otp/totp"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/platform/auth"
	"github.com/NerdMeNot/flint/internal/testutil"
	"github.com/NerdMeNot/flint/pkg/secret"
)

func testMasterKey(t *testing.T) []byte {
	t.Helper()
	k, err := testutil.TestConfig().Encryption.DecodeMasterKey()
	require.NoError(t, err)
	return k
}

func postJSON(srv *Server, path string, body map[string]any, t *testing.T) *ut.ResponseRecorder {
	t.Helper()
	return ut.PerformRequest(srv.Engine(), "POST", path, jsonBody(t, body),
		ut.Header{Key: "Content-Type", Value: "application/json"})
}

// The stored TOTP secret must be ciphertext, not the raw base32 value the
// column name (totp_secret_enc) always implied. Read access to the database was
// otherwise a permanent, undetectable MFA bypass for every user.
func TestMFASetup_StoresTheSecretEncrypted(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("GetUserByEmail", mock.Anything, mock.Anything).
		Return(db.GetUserByEmailRow{ID: "u1", Email: testUserEmail}, nil)

	var stored []byte
	m.Querier.On("SetUserTOTPSecret", mock.Anything, mock.MatchedBy(
		func(p db.SetUserTOTPSecretParams) bool {
			stored = p.TotpSecretEnc
			return true
		})).Return(nil)
	m.Querier.On("SetUserRecoveryCodes", mock.Anything, mock.Anything).Return(nil)

	w := ut.PerformRequest(srv.Engine(), "POST", "/auth/mfa/setup", nil,
		ut.Header{Key: "Authorization", Value: "Bearer test-jwt-token"})
	require.Equal(t, 200, w.Code)

	resp := parseJSON(t, w)
	plainSecret, _ := resp["secret"].(string)
	require.NotEmpty(t, plainSecret)

	require.NotEqual(t, plainSecret, string(stored), "the secret must not be stored in the clear")
	decrypted, _, err := secret.Decrypt(stored, testMasterKey(t))
	require.NoError(t, err, "stored value must be envelope ciphertext")
	require.Equal(t, plainSecret, string(decrypted))
}

// A secret that cannot be decrypted must fail closed rather than fall back to
// treating the stored bytes as plaintext — that fallback would preserve the
// original hole for every row written before the fix.
func TestMFAVerify_PlaintextSecretIsNotAccepted(t *testing.T) {
	srv, m := testServer(t)

	key, err := totp.Generate(totp.GenerateOpts{Issuer: "Flint", AccountName: "u@x.io"})
	require.NoError(t, err)
	code, err := totp.GenerateCode(key.Secret(), time.Now())
	require.NoError(t, err)

	m.Querier.On("GetMFAPendingToken", mock.Anything, "tok").Return(db.GetMFAPendingTokenRow{
		UserID: "u1", Email: "u@x.io", OrgID: testOrgID, Purpose: mfaPurposeVerify,
	}, nil)
	m.Querier.On("GetUserForAuth", mock.Anything, mock.Anything).Return(db.GetUserForAuthRow{
		ID: "u1", Email: "u@x.io", OrgID: testOrgID,
		TotpSecretEnc: []byte(key.Secret()), // legacy plaintext row
	}, nil)
	m.Querier.On("RecordMFAAttemptFailure", mock.Anything, "tok").Return(int32(1), nil)

	w := postJSON(srv, "/auth/mfa/verify", map[string]any{"mfaToken": "tok", "code": code}, t)
	require.Equal(t, 401, w.Code, "an undecryptable secret must not authenticate")
}

// Wrong codes are counted, and the token is destroyed once guessing is evident.
// Without this the endpoint was an unbounded oracle over a 10^6 code space.
func TestMFAVerify_AttemptsAreCappedPerToken(t *testing.T) {
	srv, m := testServer(t)

	m.Querier.On("GetMFAPendingToken", mock.Anything, "tok").Return(db.GetMFAPendingTokenRow{
		UserID: "u1", Email: "u@x.io", OrgID: testOrgID, Purpose: mfaPurposeVerify,
	}, nil)
	enc, err := secret.Encrypt([]byte("JBSWY3DPEHPK3PXP"), testMasterKey(t), 1)
	require.NoError(t, err)
	m.Querier.On("GetUserForAuth", mock.Anything, mock.Anything).Return(db.GetUserForAuthRow{
		ID: "u1", Email: "u@x.io", OrgID: testOrgID, TotpSecretEnc: enc,
	}, nil)

	// The final permitted failure trips the cap.
	m.Querier.On("RecordMFAAttemptFailure", mock.Anything, "tok").
		Return(int32(maxMFAAttempts), nil)
	m.Querier.On("DeleteMFAPendingToken", mock.Anything, "tok").Return(nil)
	m.Querier.On("GetOrg", mock.Anything).Return(db.GetOrgRow{ID: testOrgID}, nil).Maybe()
	m.Querier.On("InsertAuditEntry", mock.Anything, mock.Anything).Return(nil).Maybe()
	m.Querier.On("InsertAuditEntryWithMeta", mock.Anything, mock.Anything).Return(nil).Maybe()

	w := postJSON(srv, "/auth/mfa/verify", map[string]any{"mfaToken": "tok", "code": "000000"}, t)

	require.Equal(t, 401, w.Code)
	require.Contains(t, w.Body.String(), "too many incorrect codes")
	m.Querier.AssertCalled(t, "DeleteMFAPendingToken", mock.Anything, "tok")
}

// An enrolment token proves a password, not a second factor. Redeeming one for
// a session would hand a full login to someone who never presented MFA.
func TestMFAVerify_EnrolmentTokenCannotBuyASession(t *testing.T) {
	srv, m := testServer(t)

	m.Querier.On("GetMFAPendingToken", mock.Anything, "enrol-tok").Return(db.GetMFAPendingTokenRow{
		UserID: "u1", Email: "u@x.io", OrgID: testOrgID, Purpose: mfaPurposeEnrol,
	}, nil)

	w := postJSON(srv, "/auth/mfa/verify", map[string]any{"mfaToken": "enrol-tok", "code": "123456"}, t)

	require.Equal(t, 401, w.Code)
	require.Contains(t, w.Body.String(), "enrolment")
	m.Querier.AssertNotCalled(t, "GetUserForAuth", mock.Anything, mock.Anything)
}

// Recovery codes are the credential of last resort and were the weakest way in:
// 32 bits, submitted through the same unthrottled endpoint as TOTP.
func TestRecoveryCodes_AreWideEnough(t *testing.T) {
	raw, hashed, err := auth.GenerateRecoveryCodes(8)
	require.NoError(t, err)
	require.Len(t, raw, 8)
	require.Len(t, hashed, 8)

	seen := map[string]bool{}
	for _, c := range raw {
		require.Len(t, c, 19, "16 hex chars in four dash-separated groups = 64 bits")
		require.False(t, seen[c], "codes must not repeat")
		seen[c] = true
	}
}

// A role that requires MFA must be satisfiable. Refusing outright was a dead
// end: enrolling needs a session, and login is the only place one is issued.
func TestLogin_RoleRequiringMFAIssuesAnEnrolmentToken(t *testing.T) {
	srv, m := testServer(t)

	pw := "correct-horse-battery"
	hash, err := auth.HashPassword(pw)
	require.NoError(t, err)

	m.Querier.On("CountRecentFailuresByEmail", mock.Anything, "u@x.io").Return(int64(0), nil)
	m.Querier.On("CountRecentFailuresByIP", mock.Anything, mock.Anything).Return(int64(0), nil)
	m.Querier.On("GetOrg", mock.Anything).Return(db.GetOrgRow{ID: testOrgID}, nil)
	m.Querier.On("GetUserForAuth", mock.Anything, mock.Anything).Return(db.GetUserForAuthRow{
		ID: "u1", Email: "u@x.io", OrgID: testOrgID, ExternalID: "u@x.io",
		PasswordHash: &hash, IsActive: true, TotpVerified: false,
	}, nil)
	m.Querier.On("RecordLoginAttempt", mock.Anything, mock.Anything).Return(nil)
	m.Querier.On("CheckMFARequiredForUser", mock.Anything, "u@x.io").Return(true, nil)

	var purpose string
	m.Querier.On("InsertMFAPendingToken", mock.Anything, mock.MatchedBy(
		func(p db.InsertMFAPendingTokenParams) bool {
			purpose = p.Purpose
			return true
		})).Return(nil)

	w := postJSON(srv, "/auth/login", map[string]any{"email": "u@x.io", "password": pw}, t)

	require.Equal(t, 200, w.Code, "a user who must enrol has to be able to")
	resp := parseJSON(t, w)
	require.Equal(t, true, resp["mfaSetupRequired"])
	require.NotEmpty(t, resp["enrolmentToken"])
	require.Equal(t, mfaPurposeEnrol, purpose)
}

// The enrolment token is accepted by the setup endpoint, which is the whole
// point of issuing one.
func TestMFASetup_AcceptsAnEnrolmentToken(t *testing.T) {
	srv, m := testServer(t)

	m.Querier.On("GetMFAPendingToken", mock.Anything, "enrol-tok").Return(db.GetMFAPendingTokenRow{
		UserID: "u1", Email: "u@x.io", OrgID: testOrgID, Purpose: mfaPurposeEnrol,
	}, nil)
	m.Querier.On("GetUserByEmail", mock.Anything, mock.Anything).
		Return(db.GetUserByEmailRow{ID: "u1", Email: "u@x.io"}, nil)
	m.Querier.On("SetUserTOTPSecret", mock.Anything, mock.Anything).Return(nil)
	m.Querier.On("SetUserRecoveryCodes", mock.Anything, mock.Anything).Return(nil)

	w := ut.PerformRequest(srv.Engine(), "POST", "/auth/mfa/setup", nil,
		ut.Header{Key: "X-MFA-Enrolment-Token", Value: "enrol-tok"})

	require.Equal(t, 200, w.Code)
}

// ...but only an enrolment token. A verify-purpose token must not be reusable
// to re-enrol a second factor.
func TestMFASetup_RejectsAVerifyToken(t *testing.T) {
	srv, m := testServer(t)

	m.Querier.On("GetMFAPendingToken", mock.Anything, "verify-tok").Return(db.GetMFAPendingTokenRow{
		UserID: "u1", Email: "u@x.io", OrgID: testOrgID, Purpose: mfaPurposeVerify,
	}, nil)

	w := ut.PerformRequest(srv.Engine(), "POST", "/auth/mfa/setup", nil,
		ut.Header{Key: "X-MFA-Enrolment-Token", Value: "verify-tok"})

	require.Equal(t, 401, w.Code)
	m.Querier.AssertNotCalled(t, "SetUserTOTPSecret", mock.Anything, mock.Anything)
}

// With no credentials at all, enrolment is still closed.
func TestMFASetup_RejectsAnonymous(t *testing.T) {
	srv, _ := testServer(t)

	w := ut.PerformRequest(srv.Engine(), "POST", "/auth/mfa/setup", nil)

	require.Equal(t, 401, w.Code)
}

// Password spraying — one guess each against many accounts — trips no per-email
// counter, so the IP budget is what has to catch it.
func TestLogin_IsRateLimitedByIP(t *testing.T) {
	srv, m := testServer(t)

	m.Querier.On("CountRecentFailuresByEmail", mock.Anything, mock.Anything).Return(int64(0), nil)
	m.Querier.On("CountRecentFailuresByIP", mock.Anything, mock.Anything).
		Return(int64(maxFailuresPerIP), nil)

	w := postJSON(srv, "/auth/login", map[string]any{"email": "someone@x.io", "password": "x"}, t)

	require.Equal(t, 429, w.Code)
	require.Contains(t, w.Body.String(), "RATE_LIMITED")
	m.Querier.AssertNotCalled(t, "GetUserForAuth", mock.Anything, mock.Anything)
}

// An unknown account must not answer faster than a known one. The miss path
// pays the same Argon2id cost, so response time stops being an oracle.
func TestLogin_UnknownUserPaysHashingCost(t *testing.T) {
	srv, m := testServer(t)

	m.Querier.On("CountRecentFailuresByEmail", mock.Anything, mock.Anything).Return(int64(0), nil)
	m.Querier.On("CountRecentFailuresByIP", mock.Anything, mock.Anything).Return(int64(0), nil)
	m.Querier.On("GetOrg", mock.Anything).Return(db.GetOrgRow{ID: testOrgID}, nil)
	m.Querier.On("GetUserForAuth", mock.Anything, mock.Anything).
		Return(db.GetUserForAuthRow{}, fmt.Errorf("no such user"))
	m.Querier.On("RecordLoginAttempt", mock.Anything, mock.Anything).Return(nil)

	start := time.Now()
	w := postJSON(srv, "/auth/login", map[string]any{"email": "nobody@x.io", "password": "x"}, t)
	elapsed := time.Since(start)

	require.Equal(t, 401, w.Code)
	// Argon2id at 64MB/3 iterations is tens of milliseconds; an early return was
	// sub-millisecond. The bound is loose because the point is the order of
	// magnitude, not a precise duration.
	require.Greater(t, elapsed, 10*time.Millisecond,
		"the miss path must do the same hashing work as a hit")
}
