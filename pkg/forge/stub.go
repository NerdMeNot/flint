package forge

import (
	"context"
	"fmt"
	"net/http"
)

// StubForge is a forge provider for local development and demos. It serves a
// canned pipeline for every repo so the UI's trigger and retry actions work
// end-to-end without a real GitHub connection — the counterpart to the simulated
// step executor. It is NOT for production; it ignores signatures and credentials.
//
// The canned pipeline carries SIM_* env hints the simulated executor reads, so a
// triggered/retried run actually advances and completes locally.
type StubForge struct{}

// NewStubForge builds the stub forge provider.
func NewStubForge() *StubForge { return &StubForge{} }

func (StubForge) Type() string { return "github" } // matches the seeded forge connection

// ListPullRequestFiles reports no files (demo runs never filter by paths).
func (StubForge) ListPullRequestFiles(_ context.Context, _ string, _ int) ([]string, error) {
	return nil, nil
}

// Two stub pipeline payloads for two different consumers (a real forge would
// serve one file; these internal callers parse it with different parsers):
//
//   - GetFile  → ci.Service (run creation) parses with the CI parser → jobs: form.
//   - GetDirectory → the platform pipelines view parses with pkg/pipeline.Parse
//     (engine IR; the platform layer can't import the CI parser) → steps: form.
//
// stubPipeline is the CI (jobs:) form used for run creation/rerun.
const stubPipeline = `triggers:
  push:
    branches: [main]
  pull_request: {}
  manual: {}
image: node:20
jobs:
  install:
    steps:
      - name: install deps
        run: npm ci
  test:
    needs: [install]
    env:
      SIM_DURATION: "3s"
    steps:
      - name: run tests
        run: npm test
  build:
    needs: [test]
    steps:
      - name: build
        run: npm run build
`

// stubPipelineIR is the engine-IR (steps:) form, so the platform pipelines view
// (pkg/pipeline.Parse) renders a valid pipeline with steps.
const stubPipelineIR = `triggers:
  push:
    branches: [main]
steps:
  - name: install deps
    run: npm ci
  - name: run tests
    run: npm test
  - name: build
    run: npm run build
`

func (StubForge) GetFile(_ context.Context, _, _, _ string) ([]byte, error) {
	return []byte(stubPipeline), nil
}

func (StubForge) GetDirectory(_ context.Context, _, _, _ string) (map[string][]byte, error) {
	return map[string][]byte{"ci.yaml": []byte(stubPipelineIR)}, nil
}

// ParseWebhook synthesizes a push event so the webhook endpoint exercises the
// real run-creation path. Signature verification is skipped (dev only).
func (StubForge) ParseWebhook(_ http.Header, body []byte, _ string) (*WebhookEvent, error) {
	return &WebhookEvent{
		Kind:       EventPush,
		Repo:       "acme/web-app",
		Branch:     "main",
		CommitSHA:  "0000000000000000000000000000000000000000",
		Message:    "stub webhook event",
		Sender:     "stub",
		RawPayload: body,
	}, nil
}

func (StubForge) PostCommitStatus(context.Context, string, string, CommitStatus) error { return nil }

func (StubForge) CreateWebhook(_ context.Context, _, _, _ string, _ []string) (string, error) {
	return "stub-webhook", nil
}

func (StubForge) DeleteWebhook(context.Context, string, string) error { return nil }

func (StubForge) CloneURL(repo string) string {
	return fmt.Sprintf("https://example.invalid/%s.git", repo)
}
