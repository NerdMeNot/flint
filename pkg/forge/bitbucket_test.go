package forge_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/forge"
)

func newTestBitbucket(t *testing.T) forge.ForgeProvider {
	t.Helper()
	b, err := forge.NewBitbucketWithToken("test-token")
	require.NoError(t, err)
	return b
}

// Bitbucket signs with plain "X-Hub-Signature" (not the -256 suffix GitHub uses).
func signBitbucket(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

const bitbucketPush = `{
  "push": {"changes": [{"new": {
    "name": "main", "type": "branch",
    "target": {"hash": "abc123", "message": "fix: resolve bug"}
  }}]},
  "repository": {"full_name": "acme/api"},
  "actor": {"nickname": "octocat"}
}`

// A configured secret must make the signature MANDATORY. Omitting the header
// used to skip verification entirely, which let anyone who could reach the
// endpoint forge an event for any repo.
func TestBitbucket_ParseWebhook_RejectsMissingSignatureWhenSecretConfigured(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Event-Key", "repo:push")
	// Deliberately no X-Hub-Signature.

	event, err := newTestBitbucket(t).ParseWebhook(headers, []byte(bitbucketPush), testSecret)

	require.Error(t, err, "an unsigned payload must never be accepted when a secret is configured")
	assert.ErrorIs(t, err, forge.ErrWebhookInvalid)
	assert.Nil(t, event)
}

func TestBitbucket_ParseWebhook_RejectsBadSignature(t *testing.T) {
	headers := http.Header{}
	headers.Set("X-Event-Key", "repo:push")
	headers.Set("X-Hub-Signature", signBitbucket([]byte(bitbucketPush), "the-wrong-secret"))

	_, err := newTestBitbucket(t).ParseWebhook(headers, []byte(bitbucketPush), testSecret)
	assert.ErrorIs(t, err, forge.ErrSignatureMismatch)
}

func TestBitbucket_ParseWebhook_Push(t *testing.T) {
	payload := []byte(bitbucketPush)
	headers := http.Header{}
	headers.Set("X-Event-Key", "repo:push")
	headers.Set("X-Hub-Signature", signBitbucket(payload, testSecret))

	event, err := newTestBitbucket(t).ParseWebhook(headers, payload, testSecret)
	require.NoError(t, err)

	assert.Equal(t, forge.EventPush, event.Kind)
	assert.Equal(t, "acme/api", event.Repo)
	assert.Equal(t, "main", event.Branch)
	assert.Equal(t, "abc123", event.CommitSHA)
	assert.Equal(t, "fix: resolve bug", event.Message)
	assert.Equal(t, "octocat", event.Sender)
	assert.Empty(t, event.Tag, "a branch push is not a tag event")
}

// Bitbucket models tag creation as a push whose change is typed "tag", so the
// adapter has to re-classify it rather than trust the event key.
func TestBitbucket_ParseWebhook_TagPushBecomesTagEvent(t *testing.T) {
	payload := []byte(`{
	  "push": {"changes": [{"new": {
	    "name": "v1.2.3", "type": "tag",
	    "target": {"hash": "def456", "message": "release"}
	  }}]},
	  "repository": {"full_name": "acme/api"},
	  "actor": {"nickname": "octocat"}
	}`)
	headers := http.Header{}
	headers.Set("X-Event-Key", "repo:push")
	headers.Set("X-Hub-Signature", signBitbucket(payload, testSecret))

	event, err := newTestBitbucket(t).ParseWebhook(headers, payload, testSecret)
	require.NoError(t, err)

	assert.Equal(t, forge.EventTag, event.Kind)
	assert.Equal(t, "v1.2.3", event.Tag)
	assert.Empty(t, event.Branch, "a tag push must not also report a branch")
}

// A push can carry several changes; the adapter documents that it reports the
// most recent one.
func TestBitbucket_ParseWebhook_UsesMostRecentChange(t *testing.T) {
	payload := []byte(`{
	  "push": {"changes": [
	    {"new": {"name": "old", "type": "branch", "target": {"hash": "111", "message": "first"}}},
	    {"new": {"name": "main", "type": "branch", "target": {"hash": "222", "message": "latest"}}}
	  ]},
	  "repository": {"full_name": "acme/api"},
	  "actor": {"nickname": "octocat"}
	}`)
	headers := http.Header{}
	headers.Set("X-Event-Key", "repo:push")
	headers.Set("X-Hub-Signature", signBitbucket(payload, testSecret))

	event, err := newTestBitbucket(t).ParseWebhook(headers, payload, testSecret)
	require.NoError(t, err)
	assert.Equal(t, "222", event.CommitSHA)
	assert.Equal(t, "main", event.Branch)
}

func TestBitbucket_ParseWebhook_EmptyChangesRejected(t *testing.T) {
	payload := []byte(`{"push": {"changes": []}, "repository": {"full_name": "acme/api"}}`)
	headers := http.Header{}
	headers.Set("X-Event-Key", "repo:push")
	headers.Set("X-Hub-Signature", signBitbucket(payload, testSecret))

	_, err := newTestBitbucket(t).ParseWebhook(headers, payload, testSecret)
	assert.ErrorIs(t, err, forge.ErrWebhookInvalid)
}

func TestBitbucket_ParseWebhook_UnsupportedEvent(t *testing.T) {
	payload := []byte(`{}`)
	headers := http.Header{}
	headers.Set("X-Event-Key", "issue:created")
	headers.Set("X-Hub-Signature", signBitbucket(payload, testSecret))

	_, err := newTestBitbucket(t).ParseWebhook(headers, payload, testSecret)
	assert.ErrorIs(t, err, forge.ErrUnsupportedEvent)
}

func TestBitbucket_Type(t *testing.T) {
	assert.Equal(t, "bitbucket", newTestBitbucket(t).Type())
}
