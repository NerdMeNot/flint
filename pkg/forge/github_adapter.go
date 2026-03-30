package forge

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/go-github/v69/github"
)

// GitHubAdapter wraps google/go-github as a ForgeProvider.
// Handles: pagination, rate limiting, retries, GitHub App auth.
type GitHubAdapter struct {
	client  *github.Client
	appAuth *GitHubAppAuth
}

// NewGitHub creates a GitHubAdapter. If appAuth is provided, it's used for
// automatic token refresh. Otherwise, a static token is used.
func NewGitHub(token string, appAuth *GitHubAppAuth, opts ...func(*GitHubAdapter)) *GitHubAdapter {
	var httpClient *http.Client

	if appAuth != nil {
		// Use a transport that auto-refreshes the installation token.
		httpClient = &http.Client{
			Transport: &gitHubAppTransport{appAuth: appAuth},
		}
	} else if token != "" {
		httpClient = &http.Client{
			Transport: &github.BasicAuthTransport{
				Username: "x-access-token",
				Password: token,
			},
		}
	}

	client := github.NewClient(httpClient)

	adapter := &GitHubAdapter{
		client:  client,
		appAuth: appAuth,
	}

	for _, opt := range opts {
		opt(adapter)
	}

	return adapter
}

// WithGitHubBaseURL sets a custom base URL (for GitHub Enterprise).
func WithGitHubBaseURL(baseURL string) func(*GitHubAdapter) {
	return func(a *GitHubAdapter) {
		a.client, _ = a.client.WithEnterpriseURLs(baseURL, baseURL)
	}
}

func (g *GitHubAdapter) Type() string { return "github" }

func (g *GitHubAdapter) ParseWebhook(headers http.Header, body []byte, secret string) (*WebhookEvent, error) {
	sig := headers.Get("X-Hub-Signature-256")
	if sig == "" {
		return nil, fmt.Errorf("%w: missing X-Hub-Signature-256 header", ErrWebhookInvalid)
	}
	if err := VerifyHMACSHA256(body, sig, secret); err != nil {
		return nil, err
	}

	eventType := headers.Get("X-GitHub-Event")
	payload, err := github.ParseWebHook(eventType, body)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrWebhookInvalid, err)
	}

	switch e := payload.(type) {
	case *github.PushEvent:
		event := &WebhookEvent{
			Kind:       EventPush,
			Repo:       e.GetRepo().GetFullName(),
			CommitSHA:  e.GetAfter(),
			Message:    e.GetHeadCommit().GetMessage(),
			Sender:     e.GetSender().GetLogin(),
			RawPayload: body,
		}
		ref := e.GetRef()
		if strings.HasPrefix(ref, "refs/tags/") {
			event.Kind = EventTag
			event.Tag = strings.TrimPrefix(ref, "refs/tags/")
		} else if strings.HasPrefix(ref, "refs/heads/") {
			event.Branch = strings.TrimPrefix(ref, "refs/heads/")
		}
		return event, nil

	case *github.PullRequestEvent:
		return &WebhookEvent{
			Kind:       EventPullRequest,
			Repo:       e.GetRepo().GetFullName(),
			CommitSHA:  e.GetPullRequest().GetHead().GetSHA(),
			Sender:     e.GetSender().GetLogin(),
			PRNumber:   e.GetNumber(),
			PRTitle:    e.GetPullRequest().GetTitle(),
			PRAction:   e.GetAction(),
			BaseBranch: e.GetPullRequest().GetBase().GetRef(),
			HeadBranch: e.GetPullRequest().GetHead().GetRef(),
			IsDraft:    e.GetPullRequest().GetDraft(),
			RawPayload: body,
		}, nil

	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedEvent, eventType)
	}
}

func (g *GitHubAdapter) PostCommitStatus(ctx context.Context, repo, sha string, status CommitStatus) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}

	_, _, err = g.client.Repositories.CreateStatus(ctx, owner, name, sha, &github.RepoStatus{
		State:       github.Ptr(mapGitHubState(status.State)),
		Context:     github.Ptr(status.Context),
		Description: github.Ptr(status.Description),
		TargetURL:   github.Ptr(status.TargetURL),
	})
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTransient, err)
	}
	return nil
}

func (g *GitHubAdapter) GetFile(ctx context.Context, repo, ref, path string) ([]byte, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	content, _, resp, err := g.client.Repositories.GetContents(ctx, owner, name, path,
		&github.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return nil, fmt.Errorf("%w: file %s at ref %s", ErrNotFound, path, ref)
		}
		return nil, fmt.Errorf("%w: %v", ErrTransient, err)
	}

	decoded, err := content.GetContent()
	if err != nil {
		return nil, fmt.Errorf("forge: failed to decode file content: %w", err)
	}
	return []byte(decoded), nil
}

func (g *GitHubAdapter) GetDirectory(ctx context.Context, repo, ref, path string) (map[string][]byte, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	_, dirContents, resp, err := g.client.Repositories.GetContents(ctx, owner, name, path,
		&github.RepositoryContentGetOptions{Ref: ref})
	if err != nil {
		if resp != nil && resp.StatusCode == 404 {
			return nil, fmt.Errorf("%w: directory %s at ref %s", ErrNotFound, path, ref)
		}
		return nil, fmt.Errorf("%w: %v", ErrTransient, err)
	}

	files := make(map[string][]byte, len(dirContents))
	for _, entry := range dirContents {
		if entry.GetType() != "file" {
			continue
		}
		content, err := g.GetFile(ctx, repo, ref, entry.GetPath())
		if err != nil {
			return nil, err
		}
		files[entry.GetName()] = content
	}
	return files, nil
}

func (g *GitHubAdapter) CreateWebhook(ctx context.Context, repo, targetURL, secret string, events []string) (string, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return "", err
	}
	if len(events) == 0 {
		events = []string{"push", "pull_request"}
	}

	hook, _, err := g.client.Repositories.CreateHook(ctx, owner, name, &github.Hook{
		Events: events,
		Active: github.Ptr(true),
		Config: &github.HookConfig{
			URL:         github.Ptr(targetURL),
			ContentType: github.Ptr("json"),
			Secret:      github.Ptr(secret),
			InsecureSSL: github.Ptr("0"),
		},
	})
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTransient, err)
	}
	return strconv.FormatInt(hook.GetID(), 10), nil
}

func (g *GitHubAdapter) DeleteWebhook(ctx context.Context, repo, webhookID string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	id, err := strconv.ParseInt(webhookID, 10, 64)
	if err != nil {
		return fmt.Errorf("forge: invalid webhook ID %q: %w", webhookID, err)
	}

	_, err = g.client.Repositories.DeleteHook(ctx, owner, name, id)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrTransient, err)
	}
	return nil
}

func (g *GitHubAdapter) CloneURL(repo string) string {
	return fmt.Sprintf("https://github.com/%s.git", repo)
}

// gitHubAppTransport auto-refreshes the GitHub App installation token.
type gitHubAppTransport struct {
	appAuth *GitHubAppAuth
}

func (t *gitHubAppTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.appAuth.Token()
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return http.DefaultTransport.RoundTrip(req)
}

func splitRepo(repo string) (owner, name string, err error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("forge: invalid repo path %q (expected owner/name)", repo)
	}
	return parts[0], parts[1], nil
}

func mapGitHubState(state StatusState) string {
	switch state {
	case StatusPending, StatusRunning:
		return "pending"
	case StatusSuccess:
		return "success"
	case StatusFailure:
		return "failure"
	case StatusError:
		return "error"
	default:
		return "pending"
	}
}
