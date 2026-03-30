// Package forge provides the ForgeProvider interface and types for
// integrating with Git hosting platforms (GitHub, GitLab, Bitbucket).
package forge

import (
	"context"
	"net/http"
)

// ForgeProvider abstracts operations against a Git hosting platform.
type ForgeProvider interface {
	// ParseWebhook verifies the webhook signature and parses the payload
	// into a normalized WebhookEvent.
	ParseWebhook(headers http.Header, body []byte, secret string) (*WebhookEvent, error)

	// PostCommitStatus reports a pipeline status to the forge.
	PostCommitStatus(ctx context.Context, repo, sha string, status CommitStatus) error

	// GetFile fetches a single file from a repo at a given ref.
	GetFile(ctx context.Context, repo, ref, path string) ([]byte, error)

	// GetDirectory fetches all files in a directory from a repo at a given ref.
	GetDirectory(ctx context.Context, repo, ref, path string) (map[string][]byte, error)

	// CreateWebhook registers a webhook on the repo. Returns the webhook ID.
	CreateWebhook(ctx context.Context, repo, targetURL, secret string, events []string) (string, error)

	// DeleteWebhook removes a webhook from the repo.
	DeleteWebhook(ctx context.Context, repo, webhookID string) error

	// CloneURL returns the HTTPS clone URL for a repo.
	CloneURL(repo string) string

	// Type returns the forge type identifier (e.g., "github", "gitlab", "bitbucket").
	Type() string
}

// EventKind classifies the type of forge event.
type EventKind string

const (
	EventPush        EventKind = "push"
	EventPullRequest EventKind = "pull_request"
	EventTag         EventKind = "tag"
)

// WebhookEvent is a normalized representation of a forge webhook payload.
type WebhookEvent struct {
	Kind      EventKind
	Repo      string // "owner/repo"
	Branch    string
	Tag       string
	CommitSHA string
	Message   string
	Sender    string

	// PR-specific fields.
	PRNumber   int
	PRTitle    string
	PRAction   string // opened, synchronize, closed, etc.
	BaseBranch string
	HeadBranch string
	IsDraft    bool

	// Raw payload for custom processing.
	RawPayload []byte
}

// CommitStatus represents a status to report on a commit.
type CommitStatus struct {
	State       StatusState
	Context     string // e.g., "flint/ci"
	Description string
	TargetURL   string
}

// StatusState is the state of a commit status.
type StatusState string

const (
	StatusPending StatusState = "pending"
	StatusRunning StatusState = "running"
	StatusSuccess StatusState = "success"
	StatusFailure StatusState = "failure"
	StatusError   StatusState = "error"
)
