package forge_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"testing"

	"github.com/NerdMeNot/flint/pkg/forge"
)

const testSecret = "test-webhook-secret"

func signPayload(payload []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func newTestGitHub() forge.ForgeProvider {
	return forge.NewGitHub("test-token", nil)
}

func TestGitHub_ParseWebhook_Push(t *testing.T) {
	payload := []byte(`{
		"ref": "refs/heads/main",
		"after": "abc123def456",
		"repository": {"full_name": "acme/api"},
		"head_commit": {"message": "fix: resolve bug"},
		"sender": {"login": "octocat"}
	}`)

	g := newTestGitHub()
	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", signPayload(payload, testSecret))
	headers.Set("X-GitHub-Event", "push")

	event, err := g.ParseWebhook(headers, payload, testSecret)
	if err != nil {
		t.Fatalf("ParseWebhook() error: %v", err)
	}

	if event.Kind != forge.EventPush {
		t.Errorf("Kind = %q, want %q", event.Kind, forge.EventPush)
	}
	if event.Repo != "acme/api" {
		t.Errorf("Repo = %q", event.Repo)
	}
	if event.Branch != "main" {
		t.Errorf("Branch = %q, want %q", event.Branch, "main")
	}
	if event.CommitSHA != "abc123def456" {
		t.Errorf("CommitSHA = %q", event.CommitSHA)
	}
	if event.Sender != "octocat" {
		t.Errorf("Sender = %q", event.Sender)
	}
}

func TestGitHub_ParseWebhook_Tag(t *testing.T) {
	payload := []byte(`{
		"ref": "refs/tags/v1.2.0",
		"after": "def789",
		"repository": {"full_name": "acme/api"},
		"head_commit": {"message": "release v1.2.0"},
		"sender": {"login": "octocat"}
	}`)

	g := newTestGitHub()
	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", signPayload(payload, testSecret))
	headers.Set("X-GitHub-Event", "push")

	event, err := g.ParseWebhook(headers, payload, testSecret)
	if err != nil {
		t.Fatalf("ParseWebhook() error: %v", err)
	}

	if event.Kind != forge.EventTag {
		t.Errorf("Kind = %q, want %q", event.Kind, forge.EventTag)
	}
	if event.Tag != "v1.2.0" {
		t.Errorf("Tag = %q, want %q", event.Tag, "v1.2.0")
	}
}

func TestGitHub_ParseWebhook_PullRequest(t *testing.T) {
	payload := []byte(`{
		"action": "opened",
		"number": 42,
		"pull_request": {
			"title": "Add feature X",
			"draft": false,
			"head": {"sha": "head123", "ref": "feature/x"},
			"base": {"ref": "main"}
		},
		"repository": {"full_name": "acme/api"},
		"sender": {"login": "contributor"}
	}`)

	g := newTestGitHub()
	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", signPayload(payload, testSecret))
	headers.Set("X-GitHub-Event", "pull_request")

	event, err := g.ParseWebhook(headers, payload, testSecret)
	if err != nil {
		t.Fatalf("ParseWebhook() error: %v", err)
	}

	if event.Kind != forge.EventPullRequest {
		t.Errorf("Kind = %q", event.Kind)
	}
	if event.PRNumber != 42 {
		t.Errorf("PRNumber = %d", event.PRNumber)
	}
	if event.PRTitle != "Add feature X" {
		t.Errorf("PRTitle = %q", event.PRTitle)
	}
	if event.PRAction != "opened" {
		t.Errorf("PRAction = %q", event.PRAction)
	}
	if event.BaseBranch != "main" {
		t.Errorf("BaseBranch = %q", event.BaseBranch)
	}
	if event.HeadBranch != "feature/x" {
		t.Errorf("HeadBranch = %q", event.HeadBranch)
	}
}

func TestGitHub_ParseWebhook_InvalidSignature(t *testing.T) {
	payload := []byte(`{
		"ref": "refs/heads/main",
		"after": "abc",
		"repository": {"full_name": "acme/api"},
		"sender": {"login": "x"}
	}`)

	g := newTestGitHub()
	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", "sha256=0000000000000000000000000000000000000000000000000000000000000000")
	headers.Set("X-GitHub-Event", "push")

	_, err := g.ParseWebhook(headers, payload, testSecret)
	if err == nil {
		t.Fatal("expected error for invalid signature")
	}
	if !errors.Is(err, forge.ErrSignatureMismatch) {
		t.Errorf("expected ErrSignatureMismatch, got: %v", err)
	}
}

func TestGitHub_ParseWebhook_MissingSignature(t *testing.T) {
	g := newTestGitHub()
	headers := http.Header{}
	headers.Set("X-GitHub-Event", "push")

	_, err := g.ParseWebhook(headers, []byte(`{}`), testSecret)
	if err == nil {
		t.Fatal("expected error for missing signature")
	}
	if !errors.Is(err, forge.ErrWebhookInvalid) {
		t.Errorf("expected ErrWebhookInvalid, got: %v", err)
	}
}

func TestGitHub_ParseWebhook_UnsupportedEvent(t *testing.T) {
	payload := []byte(`{}`)

	g := newTestGitHub()
	headers := http.Header{}
	headers.Set("X-Hub-Signature-256", signPayload(payload, testSecret))
	headers.Set("X-GitHub-Event", "deployment")

	_, err := g.ParseWebhook(headers, payload, testSecret)
	if err == nil {
		t.Fatal("expected error for unsupported event")
	}
	if !errors.Is(err, forge.ErrUnsupportedEvent) {
		t.Errorf("expected ErrUnsupportedEvent, got: %v", err)
	}
}

func TestGitHub_CloneURL(t *testing.T) {
	g := newTestGitHub()
	got := g.CloneURL("acme/api")
	want := "https://github.com/acme/api.git"
	if got != want {
		t.Errorf("CloneURL() = %q, want %q", got, want)
	}
}

func TestGitHub_Type(t *testing.T) {
	g := newTestGitHub()
	if got := g.Type(); got != "github" {
		t.Errorf("Type() = %q, want %q", got, "github")
	}
}

func TestVerifyHMACSHA256(t *testing.T) {
	payload := []byte("test payload")
	sig := signPayload(payload, "secret")

	if err := forge.VerifyHMACSHA256(payload, sig, "secret"); err != nil {
		t.Errorf("VerifyHMACSHA256() error: %v", err)
	}

	if err := forge.VerifyHMACSHA256(payload, sig, "wrong-secret"); err == nil {
		t.Error("expected error for wrong secret")
	}
}
