package forge

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

// GitLabAdapter wraps the official GitLab Go client as a ForgeProvider.
// Handles: pagination, rate limiting, retries, OAuth token management.
type GitLabAdapter struct {
	client *gitlab.Client
}

// NewGitLab creates a GitLabAdapter. Supports gitlab.com and self-hosted.
func NewGitLab(token string, opts ...gitlab.ClientOptionFunc) (*GitLabAdapter, error) {
	client, err := gitlab.NewClient(token, opts...)
	if err != nil {
		return nil, fmt.Errorf("forge: failed to create GitLab client: %w", err)
	}
	return &GitLabAdapter{client: client}, nil
}

func (g *GitLabAdapter) Type() string { return "gitlab" }

// ParseWebhook verifies the GitLab webhook token and parses the event.
func (g *GitLabAdapter) ParseWebhook(headers http.Header, body []byte, secret string) (*WebhookEvent, error) {
	token := headers.Get("X-Gitlab-Token")
	if token == "" {
		return nil, fmt.Errorf("%w: missing X-Gitlab-Token header", ErrWebhookInvalid)
	}
	if token != secret {
		return nil, ErrSignatureMismatch
	}

	// Use the official client's webhook parser.
	eventType := headers.Get("X-Gitlab-Event")
	event, err := gitlab.ParseWebhook(gitlab.EventType(eventType), body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWebhookInvalid, err)
	}

	switch e := event.(type) {
	case *gitlab.PushEvent:
		we := &WebhookEvent{
			Kind:       EventPush,
			Repo:       e.Project.PathWithNamespace,
			CommitSHA:  e.After,
			Sender:     e.UserUsername,
			RawPayload: body,
		}
		if len(e.Ref) > 11 && e.Ref[:11] == "refs/heads/" {
			we.Branch = e.Ref[11:]
		}
		if len(e.Commits) > 0 {
			we.Message = e.Commits[len(e.Commits)-1].Message
		}
		return we, nil

	case *gitlab.TagEvent:
		tag := e.Ref
		if len(tag) > 10 && tag[:10] == "refs/tags/" {
			tag = tag[10:]
		}
		return &WebhookEvent{
			Kind:       EventTag,
			Repo:       e.Project.PathWithNamespace,
			CommitSHA:  e.CheckoutSHA,
			Tag:        tag,
			Sender:     e.UserUsername,
			RawPayload: body,
		}, nil

	case *gitlab.MergeEvent:
		return &WebhookEvent{
			Kind:       EventPullRequest,
			Repo:       e.Project.PathWithNamespace,
			CommitSHA:  e.ObjectAttributes.LastCommit.ID,
			Sender:     e.User.Username,
			PRNumber:   int(e.ObjectAttributes.IID),
			PRTitle:    e.ObjectAttributes.Title,
			PRAction:   e.ObjectAttributes.Action,
			BaseBranch: e.ObjectAttributes.TargetBranch,
			HeadBranch: e.ObjectAttributes.SourceBranch,
			IsDraft:    e.ObjectAttributes.WorkInProgress,
			RawPayload: body,
		}, nil

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedEvent, eventType)
	}
}

func (g *GitLabAdapter) PostCommitStatus(ctx context.Context, repo, sha string, status CommitStatus) error {
	state := mapGitLabState(status.State)

	_, _, err := g.client.Commits.SetCommitStatus(repo, sha, &gitlab.SetCommitStatusOptions{
		State:       gitlab.BuildStateValue(state),
		Name:        gitlab.Ptr(status.Context),
		Description: gitlab.Ptr(status.Description),
		TargetURL:   gitlab.Ptr(status.TargetURL),
	}, gitlab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTransient, err)
	}
	return nil
}

func (g *GitLabAdapter) GetFile(ctx context.Context, repo, ref, path string) ([]byte, error) {
	raw, _, err := g.client.RepositoryFiles.GetRawFile(repo, path,
		&gitlab.GetRawFileOptions{Ref: gitlab.Ptr(ref)},
		gitlab.WithContext(ctx))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrTransient, err)
	}
	return raw, nil
}

func (g *GitLabAdapter) GetDirectory(ctx context.Context, repo, ref, path string) (map[string][]byte, error) {
	// List with pagination.
	var allFiles []*gitlab.TreeNode
	opts := &gitlab.ListTreeOptions{
		Path:        gitlab.Ptr(path),
		Ref:         gitlab.Ptr(ref),
		ListOptions: gitlab.ListOptions{PerPage: 100},
	}

	for {
		nodes, resp, err := g.client.Repositories.ListTree(repo, opts, gitlab.WithContext(ctx))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrTransient, err)
		}
		allFiles = append(allFiles, nodes...)

		if resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	files := make(map[string][]byte, len(allFiles))
	for _, node := range allFiles {
		if node.Type != "blob" {
			continue
		}
		content, err := g.GetFile(ctx, repo, ref, node.Path)
		if err != nil {
			return nil, err
		}
		files[node.Name] = content
	}
	return files, nil
}

func (g *GitLabAdapter) CreateWebhook(ctx context.Context, repo, targetURL, secret string, events []string) (string, error) {
	pushEvents := contains(events, "push") || len(events) == 0
	mrEvents := contains(events, "pull_request") || contains(events, "merge_request") || len(events) == 0
	tagEvents := contains(events, "tag") || len(events) == 0

	hook, _, err := g.client.Projects.AddProjectHook(repo, &gitlab.AddProjectHookOptions{
		URL:                 gitlab.Ptr(targetURL),
		Token:               gitlab.Ptr(secret),
		PushEvents:          gitlab.Ptr(pushEvents),
		MergeRequestsEvents: gitlab.Ptr(mrEvents),
		TagPushEvents:       gitlab.Ptr(tagEvents),
	}, gitlab.WithContext(ctx))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTransient, err)
	}
	return strconv.FormatInt(int64(hook.ID), 10), nil
}

func (g *GitLabAdapter) DeleteWebhook(ctx context.Context, repo, webhookID string) error {
	id, err := strconv.Atoi(webhookID)
	if err != nil {
		return fmt.Errorf("forge: invalid webhook ID %q: %w", webhookID, err)
	}
	_, err = g.client.Projects.DeleteProjectHook(repo, int64(id), gitlab.WithContext(ctx))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTransient, err)
	}
	return nil
}

func (g *GitLabAdapter) CloneURL(repo string) string {
	baseURL := g.client.BaseURL().String()
	return fmt.Sprintf("%s%s.git", baseURL, repo)
}

func mapGitLabState(state StatusState) string {
	switch state {
	case StatusPending:
		return "pending"
	case StatusRunning:
		return "running"
	case StatusSuccess:
		return "success"
	case StatusFailure, StatusError:
		return "failed"
	default:
		return "pending"
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
