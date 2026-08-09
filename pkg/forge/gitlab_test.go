package forge_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/NerdMeNot/flint/pkg/forge"
)

func newTestGitLab(t *testing.T) forge.ForgeProvider {
	t.Helper()
	g, err := forge.NewGitLab("test-token")
	require.NoError(t, err)
	return g
}

// GitLab authenticates webhooks with a shared secret in X-Gitlab-Token rather
// than an HMAC over the body, so the whole check is "header present and equal".
func gitlabHeaders(event, token string) http.Header {
	h := http.Header{}
	h.Set("X-Gitlab-Event", event)
	if token != "" {
		h.Set("X-Gitlab-Token", token)
	}
	return h
}

func TestGitLab_ParseWebhook_RejectsMissingToken(t *testing.T) {
	_, err := newTestGitLab(t).ParseWebhook(
		gitlabHeaders("Push Hook", ""), []byte(`{}`), testSecret)

	assert.ErrorIs(t, err, forge.ErrWebhookInvalid,
		"an unauthenticated payload must never be parsed")
}

func TestGitLab_ParseWebhook_RejectsWrongToken(t *testing.T) {
	_, err := newTestGitLab(t).ParseWebhook(
		gitlabHeaders("Push Hook", "the-wrong-secret"), []byte(`{}`), testSecret)

	assert.ErrorIs(t, err, forge.ErrSignatureMismatch)
}

func TestGitLab_ParseWebhook_Push(t *testing.T) {
	payload := []byte(`{
	  "object_kind": "push",
	  "ref": "refs/heads/main",
	  "after": "abc123def456",
	  "user_username": "octocat",
	  "project": {"path_with_namespace": "acme/api"},
	  "commits": [
	    {"message": "first commit"},
	    {"message": "fix: resolve bug"}
	  ]
	}`)

	event, err := newTestGitLab(t).ParseWebhook(
		gitlabHeaders("Push Hook", testSecret), payload, testSecret)
	require.NoError(t, err)

	assert.Equal(t, forge.EventPush, event.Kind)
	assert.Equal(t, "acme/api", event.Repo)
	assert.Equal(t, "main", event.Branch, "refs/heads/ prefix must be stripped")
	assert.Equal(t, "abc123def456", event.CommitSHA)
	assert.Equal(t, "octocat", event.Sender)
	assert.Equal(t, "fix: resolve bug", event.Message, "the LAST commit is the head commit")
}

// A push to a ref that isn't a branch (GitLab sends the same hook shape) must
// not be mangled into a bogus branch name by blind prefix stripping.
func TestGitLab_ParseWebhook_PushNonBranchRefLeavesBranchEmpty(t *testing.T) {
	payload := []byte(`{
	  "object_kind": "push",
	  "ref": "refs/merge-requests/7/head",
	  "after": "abc123",
	  "user_username": "octocat",
	  "project": {"path_with_namespace": "acme/api"}
	}`)

	event, err := newTestGitLab(t).ParseWebhook(
		gitlabHeaders("Push Hook", testSecret), payload, testSecret)
	require.NoError(t, err)
	assert.Empty(t, event.Branch)
}

func TestGitLab_ParseWebhook_Tag(t *testing.T) {
	payload := []byte(`{
	  "object_kind": "tag_push",
	  "ref": "refs/tags/v1.2.3",
	  "checkout_sha": "def456",
	  "user_username": "octocat",
	  "project": {"path_with_namespace": "acme/api"}
	}`)

	event, err := newTestGitLab(t).ParseWebhook(
		gitlabHeaders("Tag Push Hook", testSecret), payload, testSecret)
	require.NoError(t, err)

	assert.Equal(t, forge.EventTag, event.Kind)
	assert.Equal(t, "v1.2.3", event.Tag, "refs/tags/ prefix must be stripped")
	assert.Equal(t, "def456", event.CommitSHA)
}

// GitLab calls them merge requests; Flint normalizes every forge to the same
// pull-request vocabulary, so the mapping is worth pinning down.
func TestGitLab_ParseWebhook_MergeRequestNormalizesToPullRequest(t *testing.T) {
	payload := []byte(`{
	  "object_kind": "merge_request",
	  "user": {"username": "octocat"},
	  "project": {"path_with_namespace": "acme/api"},
	  "object_attributes": {
	    "iid": 42,
	    "title": "Add widget",
	    "action": "open",
	    "target_branch": "main",
	    "source_branch": "feature/widget",
	    "work_in_progress": true,
	    "last_commit": {"id": "cafe123"}
	  }
	}`)

	event, err := newTestGitLab(t).ParseWebhook(
		gitlabHeaders("Merge Request Hook", testSecret), payload, testSecret)
	require.NoError(t, err)

	assert.Equal(t, forge.EventPullRequest, event.Kind)
	assert.Equal(t, 42, event.PRNumber, "GitLab's iid is the user-facing number")
	assert.Equal(t, "Add widget", event.PRTitle)
	assert.Equal(t, "open", event.PRAction)
	assert.Equal(t, "main", event.BaseBranch)
	assert.Equal(t, "feature/widget", event.HeadBranch)
	assert.Equal(t, "cafe123", event.CommitSHA)
	assert.True(t, event.IsDraft, "work_in_progress maps to draft")
}

func TestGitLab_ParseWebhook_UnsupportedEvent(t *testing.T) {
	_, err := newTestGitLab(t).ParseWebhook(
		gitlabHeaders("Issue Hook", testSecret),
		[]byte(`{"object_kind": "issue"}`), testSecret)

	assert.ErrorIs(t, err, forge.ErrUnsupportedEvent)
}

// paths: filters must fail OPEN, never wedge CI: GitLab can't list a merge
// request's changed files here, and the contract is to say so explicitly.
func TestGitLab_ListPullRequestFiles_ReportsUnsupported(t *testing.T) {
	_, err := newTestGitLab(t).ListPullRequestFiles(t.Context(), "acme/api", 1)
	assert.ErrorIs(t, err, forge.ErrUnsupportedEvent)
}

func TestGitLab_Type(t *testing.T) {
	assert.Equal(t, "gitlab", newTestGitLab(t).Type())
}
