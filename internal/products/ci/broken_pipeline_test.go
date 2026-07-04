package ci

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/testutil/mocks"
	"github.com/NerdMeNot/flint/pkg/forge"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// statusRecorder captures PostCommitStatus calls (posted async by the service).
type statusRecorder struct {
	mu       sync.Mutex
	statuses []forge.CommitStatus
}

func (r *statusRecorder) record(status forge.CommitStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.statuses = append(r.statuses, status)
}

func (r *statusRecorder) wait(t *testing.T, n int) []forge.CommitStatus {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		if len(r.statuses) >= n {
			out := append([]forge.CommitStatus(nil), r.statuses...)
			r.mu.Unlock()
			return out
		}
		r.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected %d commit statuses, got %d", n, len(r.statuses))
	return nil
}

// TestHandleWebhook_BrokenPipeline_PostsFailureStatus: an unparseable pipeline
// must surface as a failure commit status on the forge, not a silent skip.
func TestHandleWebhook_BrokenPipeline_PostsFailureStatus(t *testing.T) {
	ctx := context.Background()
	rec := &statusRecorder{}

	fg := mocks.NewForgeProvider(t)
	fg.On("ParseWebhook", mock.Anything, mock.Anything, "hook-secret").Return(&forge.WebhookEvent{
		Kind: forge.EventPush, Repo: "acme/app", Branch: "main",
		CommitSHA: "deadbeef", Message: "break ci", Sender: "dev",
	}, nil)
	// One pipeline file, with a typo'd field (strict decode error).
	fg.On("GetDirectory", mock.Anything, "acme/app", "deadbeef", ".flint/").
		Return(map[string][]byte{"ci.yaml": nil}, nil)
	fg.On("GetFile", mock.Anything, "acme/app", "deadbeef", ".flint/ci.yaml").
		Return([]byte("triggers:\n  push: {branches: [main]}\njobs:\n  build:\n    timout: 5m\n    steps: [{run: make}]\n"), nil)
	fg.On("PostCommitStatus", mock.Anything, "acme/app", "deadbeef", mock.Anything).
		Run(func(args mock.Arguments) {
			rec.record(args.Get(3).(forge.CommitStatus))
		}).Return(nil)

	q := mocks.NewQuerier(t)
	q.On("GetWebhookSecret", mock.Anything, "github").Return("hook-secret", nil)
	q.On("GetProjectByRepoPath", mock.Anything, "acme/app").Return(db.GetProjectByRepoPathRow{
		ID: "proj-1", OrgID: "org-1",
	}, nil)

	svc := NewService(nil, fg, q)
	runIDs, err := svc.HandleWebhook(ctx, http.Header{}, []byte("{}"), "github")
	require.NoError(t, err)
	assert.Empty(t, runIDs, "a broken pipeline must not create a run")

	statuses := rec.wait(t, 1)
	assert.Equal(t, forge.StatusFailure, statuses[0].State)
	assert.Equal(t, "flint/ci.yaml", statuses[0].Context)
	assert.Contains(t, statuses[0].Description, "timout", "the status should carry the parse error")
	assert.LessOrEqual(t, len(statuses[0].Description), 140, "GitHub caps descriptions at 140 chars")
}
