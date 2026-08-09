package server

import (
	"encoding/hex"
	"encoding/json"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/common/ut"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/testutil"
	"github.com/NerdMeNot/flint/pkg/secret"
)

// TestSetEnvVariableValue_SecretIsEncrypted proves a secret value is encrypted
// at rest: the bytes handed to the DB are ciphertext (not the plaintext) and
// decrypt back to the original with the server master key.
func TestSetEnvVariableValue_SecretIsEncrypted(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("IsEnvVariableSecret", mock.Anything, "var-1").Return(true, nil)

	var stored []byte
	m.Querier.On("UpsertSecretEnvVariableValue", mock.Anything,
		mock.MatchedBy(func(p db.UpsertSecretEnvVariableValueParams) bool {
			stored = p.ValueEnc
			return p.VariableID == "var-1"
		})).Return(nil)

	body := jsonBody(t, map[string]any{"variableId": "var-1", "value": "super-secret"})
	w := ut.PerformRequest(srv.Engine(), "PUT", "/api/v1/env-variables/var-1/values", body,
		ut.Header{Key: "Authorization", Value: "Bearer test-jwt-token"},
		ut.Header{Key: "Content-Type", Value: "application/json"})
	require.Equal(t, 200, w.Code)

	require.NotEmpty(t, stored)
	assert.NotEqual(t, []byte("super-secret"), stored, "value must be stored as ciphertext")

	key, err := hex.DecodeString(testutil.TestConfig().Encryption.MasterKey)
	require.NoError(t, err)
	plaintext, _, err := secret.Decrypt(stored, key)
	require.NoError(t, err)
	assert.Equal(t, "super-secret", string(plaintext))
}

// TestListEnvVariableValues_SecretMasked confirms secret values are masked, never
// returned, in the list response.
func TestListEnvVariableValues_SecretMasked(t *testing.T) {
	srv, m := testServer(t)
	setupAuth(m)

	m.Querier.On("ListEnvVariableValues", mock.Anything, "org-1").
		Return([]db.ListEnvVariableValuesRow{
			{VariableID: "var-1", EnvironmentID: "", Value: "", UpdatedAt: time.Now(), IsSecret: true},
			{VariableID: "var-2", EnvironmentID: "", Value: "plain", UpdatedAt: time.Now(), IsSecret: false},
		}, nil)

	w := ut.PerformRequest(srv.Engine(), "GET", "/api/v1/env-variables/values", nil,
		ut.Header{Key: "Authorization", Value: "Bearer test-jwt-token"})
	require.Equal(t, 200, w.Code)

	var resp struct {
		Items []struct {
			VariableID string `json:"variableId"`
			Value      string `json:"value"`
		} `json:"items"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 2)
	assert.Equal(t, "••••••••", resp.Items[0].Value, "secret value must be masked")
	assert.Equal(t, "plain", resp.Items[1].Value)
}
