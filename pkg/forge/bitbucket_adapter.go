package forge

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ktrysmt/go-bitbucket"
)

// BitbucketAdapter wraps ktrysmt/go-bitbucket as a ForgeProvider.
type BitbucketAdapter struct {
	client *bitbucket.Client
}

// NewBitbucket creates a BitbucketAdapter with OAuth credentials.
func NewBitbucket(clientID, clientSecret string) (*BitbucketAdapter, error) {
	client, err := bitbucket.NewOAuthClientCredentials(clientID, clientSecret)
	if err != nil {
		return nil, fmt.Errorf("forge: failed to create Bitbucket client: %w", err)
	}
	return &BitbucketAdapter{client: client}, nil
}

// NewBitbucketWithToken creates a BitbucketAdapter with a bearer token.
func NewBitbucketWithToken(token string) (*BitbucketAdapter, error) {
	client, err := bitbucket.NewOAuthbearerToken(token)
	if err != nil {
		return nil, fmt.Errorf("forge: failed to create Bitbucket client: %w", err)
	}
	return &BitbucketAdapter{client: client}, nil
}

func (b *BitbucketAdapter) Type() string { return "bitbucket" }

// ListPullRequestFiles is not implemented for Bitbucket yet — callers treat
// ErrUnsupportedEvent as "changed files unknown" and fail open.
func (b *BitbucketAdapter) ListPullRequestFiles(ctx context.Context, repo string, prNumber int) ([]string, error) {
	return nil, fmt.Errorf("%w: bitbucket changed-files listing", ErrUnsupportedEvent)
}

// ParseWebhook parses and verifies a Bitbucket webhook.
// Bitbucket Cloud supports HMAC-SHA256 signatures via X-Hub-Signature header.
func (b *BitbucketAdapter) ParseWebhook(headers http.Header, body []byte, secret string) (*WebhookEvent, error) {
	// Verify signature if present and secret is configured.
	if sig := headers.Get("X-Hub-Signature"); sig != "" && secret != "" {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(sig), []byte(expected)) {
			return nil, ErrSignatureMismatch
		}
	}

	eventType := headers.Get("X-Event-Key")
	switch eventType {
	case "repo:push":
		return b.parsePush(body)
	case "pullrequest:created", "pullrequest:updated":
		return b.parsePullRequest(body, eventType)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedEvent, eventType)
	}
}

func (b *BitbucketAdapter) parsePush(body []byte) (*WebhookEvent, error) {
	var payload struct {
		Push struct {
			Changes []struct {
				New struct {
					Name   string `json:"name"`
					Type   string `json:"type"`
					Target struct {
						Hash    string `json:"hash"`
						Message string `json:"message"`
					} `json:"target"`
				} `json:"new"`
			} `json:"changes"`
		} `json:"push"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Actor struct {
			Nickname string `json:"nickname"`
		} `json:"actor"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWebhookInvalid, err)
	}

	// Process ALL changes, not just the first one.
	// Return the most recent change (last in array).
	if len(payload.Push.Changes) == 0 {
		return nil, fmt.Errorf("%w: no changes in push event", ErrWebhookInvalid)
	}

	// Use last change (most recent).
	change := payload.Push.Changes[len(payload.Push.Changes)-1]
	event := &WebhookEvent{
		Kind:       EventPush,
		Repo:       payload.Repository.FullName,
		CommitSHA:  change.New.Target.Hash,
		Message:    change.New.Target.Message,
		Sender:     payload.Actor.Nickname,
		RawPayload: body,
	}

	if change.New.Type == "tag" {
		event.Kind = EventTag
		event.Tag = change.New.Name
	} else {
		event.Branch = change.New.Name
	}

	return event, nil
}

func (b *BitbucketAdapter) parsePullRequest(body []byte, eventType string) (*WebhookEvent, error) {
	var payload struct {
		PullRequest struct {
			ID     int    `json:"id"`
			Title  string `json:"title"`
			State  string `json:"state"`
			Source struct {
				Branch struct {
					Name string `json:"name"`
				} `json:"branch"`
				Commit struct {
					Hash string `json:"hash"`
				} `json:"commit"`
			} `json:"source"`
			Destination struct {
				Branch struct {
					Name string `json:"name"`
				} `json:"branch"`
			} `json:"destination"`
		} `json:"pullrequest"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
		Actor struct {
			Nickname string `json:"nickname"`
		} `json:"actor"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWebhookInvalid, err)
	}

	action := "opened"
	if eventType == "pullrequest:updated" {
		action = "synchronize"
	}

	return &WebhookEvent{
		Kind:       EventPullRequest,
		Repo:       payload.Repository.FullName,
		CommitSHA:  payload.PullRequest.Source.Commit.Hash,
		Sender:     payload.Actor.Nickname,
		PRNumber:   payload.PullRequest.ID,
		PRTitle:    payload.PullRequest.Title,
		PRAction:   action,
		BaseBranch: payload.PullRequest.Destination.Branch.Name,
		HeadBranch: payload.PullRequest.Source.Branch.Name,
		IsDraft:    strings.EqualFold(payload.PullRequest.State, "DRAFT"),
		RawPayload: body,
	}, nil
}

func (b *BitbucketAdapter) PostCommitStatus(ctx context.Context, repo, sha string, status CommitStatus) error {
	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return err
	}

	cmo := &bitbucket.CommitsOptions{
		Owner:    owner,
		RepoSlug: repoName,
		Revision: sha,
	}
	cso := &bitbucket.CommitStatusOptions{
		State: mapBitbucketState(status.State),
		Key:   status.Context,
		Url:   status.TargetURL,
	}

	_, err = b.client.Repositories.Commits.CreateCommitStatus(cmo, cso)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTransient, err)
	}
	return nil
}

func (b *BitbucketAdapter) GetFile(ctx context.Context, repo, ref, path string) ([]byte, error) {
	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	content, err := b.client.Repositories.Repository.GetFileBlob(&bitbucket.RepositoryBlobOptions{
		Owner:    owner,
		RepoSlug: repoName,
		Ref:      ref,
		Path:     path,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTransient, err)
	}
	return content.Content, nil
}

func (b *BitbucketAdapter) GetDirectory(ctx context.Context, repo, ref, path string) (map[string][]byte, error) {
	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	dirContents, err := b.client.Repositories.Repository.ListFiles(&bitbucket.RepositoryFilesOptions{
		Owner:    owner,
		RepoSlug: repoName,
		Ref:      ref,
		Path:     path,
	})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTransient, err)
	}

	files := make(map[string][]byte)
	for _, f := range dirContents {
		if f.Type != "commit_file" {
			continue
		}
		content, err := b.GetFile(ctx, repo, ref, f.Path)
		if err != nil {
			return nil, err
		}
		name := f.Path
		if idx := strings.LastIndex(f.Path, "/"); idx >= 0 {
			name = f.Path[idx+1:]
		}
		files[name] = content
	}
	return files, nil
}

func (b *BitbucketAdapter) CreateWebhook(ctx context.Context, repo, targetURL, secret string, events []string) (string, error) {
	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return "", err
	}

	bbEvents := make([]string, 0, len(events))
	for _, e := range events {
		switch e {
		case "push":
			bbEvents = append(bbEvents, "repo:push")
		case "pull_request":
			bbEvents = append(bbEvents, "pullrequest:created", "pullrequest:updated")
		}
	}
	if len(bbEvents) == 0 {
		bbEvents = []string{"repo:push", "pullrequest:created", "pullrequest:updated"}
	}

	hook, err := b.client.Repositories.Webhooks.Create(&bitbucket.WebhooksOptions{
		Owner:       owner,
		RepoSlug:    repoName,
		Description: "Flint CI",
		Url:         targetURL,
		Active:      true,
		Events:      bbEvents,
	})
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTransient, err)
	}

	return hook.Uuid, nil
}

func (b *BitbucketAdapter) DeleteWebhook(ctx context.Context, repo, webhookID string) error {
	owner, repoName, err := splitRepo(repo)
	if err != nil {
		return err
	}

	_, err = b.client.Repositories.Webhooks.Delete(&bitbucket.WebhooksOptions{
		Owner:    owner,
		RepoSlug: repoName,
		Uuid:     webhookID,
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTransient, err)
	}
	return nil
}

func (b *BitbucketAdapter) CloneURL(repo string) string {
	return fmt.Sprintf("https://bitbucket.org/%s.git", repo)
}

func mapBitbucketState(state StatusState) string {
	switch state {
	case StatusPending, StatusRunning:
		return "INPROGRESS"
	case StatusSuccess:
		return "SUCCESSFUL"
	case StatusFailure, StatusError:
		return "FAILED"
	default:
		return "INPROGRESS"
	}
}
