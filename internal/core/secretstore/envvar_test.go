package secretstore

import (
	"bytes"
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/testutil/mocks"
	"github.com/NerdMeNot/flint/pkg/secret"
)

var testMasterKey = bytes.Repeat([]byte{0x2a}, 32)

func TestEnvVarStore_GetRoundTrip(t *testing.T) {
	q := mocks.NewQuerier(t)
	store, err := NewEnvVarStore(q, testMasterKey)
	require.NoError(t, err)

	// A value encrypted on write decrypts back through the store on read.
	blob, err := secret.Encrypt([]byte("s3cr3t-token"), testMasterKey, 1)
	require.NoError(t, err)
	assert.NotEqual(t, []byte("s3cr3t-token"), blob, "stored blob must be ciphertext, not plaintext")

	q.On("GetSecretEnvVarValue", mock.Anything, db.GetSecretEnvVarValueParams{
		OrgID: "org-1", Name: "API_KEY", EnvSlug: "production",
	}).Return(blob, nil)

	val, err := store.Get(context.Background(), secret.Ref{
		OrgID: "org-1", Name: "API_KEY", Environment: "production",
	})
	require.NoError(t, err)
	assert.Equal(t, "s3cr3t-token", val)
}

func TestEnvVarStore_GetNotFound(t *testing.T) {
	q := mocks.NewQuerier(t)
	store, err := NewEnvVarStore(q, testMasterKey)
	require.NoError(t, err)

	q.On("GetSecretEnvVarValue", mock.Anything, mock.Anything).
		Return([]byte(nil), pgx.ErrNoRows)

	_, err = store.Get(context.Background(), secret.Ref{OrgID: "org-1", Name: "MISSING"})
	require.Error(t, err)
}

func TestEnvVarStore_ReadOnly(t *testing.T) {
	store, err := NewEnvVarStore(mocks.NewQuerier(t), testMasterKey)
	require.NoError(t, err)

	assert.ErrorIs(t, store.Set(context.Background(), secret.Ref{}, "v"), secret.ErrReadOnly)
	assert.ErrorIs(t, store.Delete(context.Background(), secret.Ref{}), secret.ErrReadOnly)
}

func TestNewEnvVarStore_BadKey(t *testing.T) {
	_, err := NewEnvVarStore(mocks.NewQuerier(t), []byte("too-short"))
	require.Error(t, err)
}
