package agentd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog"

	"github.com/NerdMeNot/flint/internal/core/agent"
	"github.com/NerdMeNot/flint/pkg/artifact"
	"github.com/NerdMeNot/flint/pkg/cache"
	"github.com/NerdMeNot/flint/pkg/checkout"
	"github.com/NerdMeNot/flint/pkg/pipeline"
	agentv1 "github.com/NerdMeNot/flint/protogen/agent/v1"
)

// dataPlane bundles the machine-local stores the daemon shares across steps:
// git mirrors and the layered cache — the persistent-machine speed wins.
type dataPlane struct {
	mirrors *checkout.MirrorStore
	cache   *cache.LocalStore
}

func newDataPlane(dataDir string, cacheMaxBytes int64) (*dataPlane, error) {
	local, err := cache.NewLocal(filepath.Join(dataDir, "cache"), cacheMaxBytes)
	if err != nil {
		return nil, err
	}
	return &dataPlane{
		mirrors: &checkout.MirrorStore{Root: filepath.Join(dataDir, "git-mirrors")},
		cache:   local,
	}, nil
}

// checkoutCommand reports whether the resolved step is the built-in checkout
// (use: checkout resolves to this exact run command).
func checkoutCommand(stepDef *pipeline.Step) bool {
	return strings.TrimSpace(stepDef.Run.String()) == "flint-agent checkout"
}

// runCheckout executes the built-in checkout natively on the daemon: the
// per-repo bare mirror serves objects from local disk, so repeat builds pay
// the fetch delta instead of a full clone.
func (d *Daemon) runCheckout(ctx context.Context, a *agentv1.Assignment, stepDef *pipeline.Step, wsDir string, logger zerolog.Logger) *agentv1.StepResult {
	payload := a.GetPayload()

	opts := checkout.Options{
		Repo: payload.GetRepo(),
		Ref:  payload.GetRef(),
		SHA:  payload.GetCommitSha(),
		Path: wsDir,
	}
	// Step inputs (with:) override the run context, matching the env-driven
	// behavior of the container checkout.
	applyCheckoutInputs(&opts, stepDef.With)

	// Clone URL + forge token from the control plane (scoped to this step).
	if resp, err := d.client.svc.GetCloneToken(ctx, &agentv1.GetCloneTokenRequest{
		AssignmentId: a.GetAssignmentId(),
	}); err == nil {
		if opts.CloneURL == "" {
			opts.CloneURL = resp.GetCloneUrl()
		}
		if opts.Token == "" {
			opts.Token = resp.GetToken()
		}
	} else {
		logger.Warn().Err(err).Msg("agentd: clone token unavailable — trying default clone URL")
	}

	if err := checkout.RunWithMirror(ctx, d.data.mirrors, opts); err != nil {
		return failResult("checkout: " + err.Error())
	}
	return &agentv1.StepResult{Status: "succeeded", ExitCode: 0}
}

// fetchSecrets pulls the step's secrets at execution time and writes them to
// a mode-0600 env file the runtime injects — never the daemon's own process
// env. Returns the file path ("" when the step maps no secrets) and a cleanup
// function that shreds it.
func (d *Daemon) fetchSecrets(ctx context.Context, a *agentv1.Assignment, ioDir string) (string, func(), error) {
	payload := a.GetPayload()
	if len(payload.GetSecretMapping()) == 0 {
		return "", func() {}, nil
	}
	resp, err := d.client.svc.GetStepSecrets(ctx, &agentv1.GetStepSecretsRequest{
		AssignmentId: a.GetAssignmentId(),
	})
	if err != nil {
		return "", nil, fmt.Errorf("fetch secrets: %w", err)
	}

	var b strings.Builder
	for k, v := range resp.GetSecrets() {
		b.WriteString(k)
		b.WriteString("=")
		b.WriteString(v)
		b.WriteString("\n")
	}
	path := filepath.Join(ioDir, "secrets.env")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		return "", nil, err
	}
	cleanup := func() {
		// Overwrite before unlink so the plaintext doesn't linger on disk.
		if info, err := os.Stat(path); err == nil {
			_ = os.WriteFile(path, make([]byte, info.Size()), 0o600)
		}
		_ = os.Remove(path)
	}
	return path, cleanup, nil
}

// restoreCache evaluates the step's cache key (post-checkout hashFiles) and
// restores from the layered local→S3 cache. Failures degrade to a cold build,
// never a failed step.
func (d *Daemon) restoreCache(ctx context.Context, payload *agentv1.StepPayload, stepDef *pipeline.Step, wsDir string, logger zerolog.Logger) {
	if stepDef.Cache == nil {
		return
	}
	key, err := evaluateCacheKey(payload, stepDef, wsDir)
	if err != nil {
		logger.Warn().Err(err).Msg("agentd: cache key evaluation failed (skipping restore)")
		return
	}
	store := d.layeredCache(payload)
	if match, err := store.RestoreWithFallback(ctx, wsDir, key, stepDef.Cache.RestoreKeys, stepDef.Cache.Paths); err != nil {
		logger.Warn().Err(err).Msg("agentd: cache restore failed (continuing cold)")
	} else if match != "" {
		logger.Info().Str("key", match).Msg("agentd: cache restored")
	}
}

// saveCache persists the step's cache paths after success.
func (d *Daemon) saveCache(ctx context.Context, payload *agentv1.StepPayload, stepDef *pipeline.Step, wsDir string, logger zerolog.Logger) {
	if stepDef.Cache == nil {
		return
	}
	key, err := evaluateCacheKey(payload, stepDef, wsDir)
	if err != nil {
		logger.Warn().Err(err).Msg("agentd: cache key evaluation failed (skipping save)")
		return
	}
	if err := d.layeredCache(payload).Save(ctx, wsDir, key, stepDef.Cache.Paths); err != nil {
		logger.Warn().Err(err).Str("key", key).Msg("agentd: cache save failed")
	}
}

// layeredCache stacks the machine-local tier over S3 when object storage is
// configured for this step's org/project.
func (d *Daemon) layeredCache(payload *agentv1.StepPayload) cache.Cache {
	var remote cache.Cache
	if s := payload.GetStorage(); s.GetBucket() != "" {
		remote = cache.NewS3(payload.GetOrgId(), payload.GetProjectId(), s.GetBucket(), s.GetRegion())
	}
	return cache.NewLayered(d.data.cache, remote)
}

// evaluateCacheKey interpolates the key expression with the step's git
// context, hashing files under this run's workspace.
func evaluateCacheKey(payload *agentv1.StepPayload, stepDef *pipeline.Step, wsDir string) (string, error) {
	hasher := &agent.WorkspaceHasher{Workspace: wsDir}
	exprCtx := pipeline.BuildRuntimeContext(pipeline.RuntimeContextOpts{
		Branch:     payload.GetRef(),
		CommitSha:  payload.GetCommitSha(),
		FileHasher: hasher,
	})
	return pipeline.Interpolate(stepDef.Cache.Key, exprCtx)
}

// downloadArtifacts fetches this step's declared artifact inputs from object
// storage into the workspace.
func (d *Daemon) downloadArtifacts(ctx context.Context, payload *agentv1.StepPayload, stepDef *pipeline.Step, wsDir string) error {
	if len(stepDef.Inputs) == 0 {
		return nil
	}
	s := payload.GetStorage()
	if s.GetBucket() == "" {
		return fmt.Errorf("step declares artifact inputs but no object storage is configured")
	}
	store := artifact.NewS3Store(s.GetBucket(), s.GetRegion())
	for _, in := range stepDef.Inputs {
		name := in.Name
		if name == "" {
			name = in.Path
		}
		ref := artifact.Ref{
			OrgID: payload.GetOrgId(), RunID: payload.GetRunId(),
			StepName: in.From, Name: name,
		}
		if err := store.Download(ctx, ref, filepath.Join(wsDir, in.Path)); err != nil {
			return fmt.Errorf("artifact %s from %s: %w", name, in.From, err)
		}
	}
	return nil
}

// uploadArtifacts publishes this step's declared artifact outputs.
func (d *Daemon) uploadArtifacts(ctx context.Context, payload *agentv1.StepPayload, stepDef *pipeline.Step, wsDir string) error {
	if len(stepDef.Outputs) == 0 {
		return nil
	}
	s := payload.GetStorage()
	if s.GetBucket() == "" {
		return fmt.Errorf("step declares artifact outputs but no object storage is configured")
	}
	store := artifact.NewS3Store(s.GetBucket(), s.GetRegion())
	for _, out := range stepDef.Outputs {
		name := out.Name
		if name == "" {
			name = out.Path
		}
		ref := artifact.Ref{
			OrgID: payload.GetOrgId(), RunID: payload.GetRunId(),
			StepName: payload.GetStepName(), Name: name,
		}
		if err := store.Upload(ctx, ref, filepath.Join(wsDir, out.Path)); err != nil {
			return fmt.Errorf("artifact %s: %w", name, err)
		}
	}
	return nil
}

// applyCheckoutInputs overlays step inputs (with:) onto the checkout options.
func applyCheckoutInputs(opts *checkout.Options, inputs map[string]string) {
	if v := inputs["repo"]; v != "" {
		opts.Repo = v
	}
	if v := inputs["ref"]; v != "" {
		opts.Ref = v
	}
	if v := inputs["sha"]; v != "" {
		opts.SHA = v
	}
	if v := inputs["token"]; v != "" {
		opts.Token = v
	}
	if v := inputs["path"]; v != "" {
		opts.Path = filepath.Join(opts.Path, v)
	}
	if inputs["submodules"] == "true" {
		opts.Submodules = true
	}
}
