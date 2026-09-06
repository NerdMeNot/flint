package auth

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// A CSPRNG failure must not yield a fixed password: this generates the
// bootstrap admin credential on a fresh install.
func TestGenerateRandomPassword_IsRandomAndReported(t *testing.T) {
	a, err := GenerateRandomPassword()
	require.NoError(t, err)
	b, err := GenerateRandomPassword()
	require.NoError(t, err)

	require.NotEqual(t, a, b)
	require.Len(t, a, 32, "16 bytes hex-encoded")
}

// The dummy-hash path has to survive a rand failure and still cost a full
// derivation, or the timing defence it exists for disappears exactly when
// something is already wrong.
func TestFallbackDummyHash_IsWellFormed(t *testing.T) {
	h := fallbackDummyHash()
	require.False(t, VerifyPassword("anything", h), "nothing may verify against it")
	require.False(t, VerifyPassword("", h))
}

func TestVerifyAgainstDummyHash_DoesNotPanic(t *testing.T) {
	VerifyAgainstDummyHash("some-password")
}
