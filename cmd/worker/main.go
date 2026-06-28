package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/NerdMeNot/flint/internal/core/dbkit"
	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/observe"
	"github.com/NerdMeNot/flint/internal/core/runner"
	workerinformer "github.com/NerdMeNot/flint/internal/core/worker/informer"
	"github.com/NerdMeNot/flint/internal/platform/config"
	"github.com/NerdMeNot/flint/internal/products/workflows"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	version    = "dev"
	commit     = "unknown"
	configPath string
)

func main() {
	root := &cobra.Command{
		Use:     "flint-worker",
		Short:   "Flint CI worker — workflow engine, K8s job orchestration",
		Version: fmt.Sprintf("%s (%s)", version, commit),
		RunE:    run,
	}

	root.Flags().StringVar(&configPath, "config", "", "path to config file")

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func run(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if err := config.Validate(cfg, "worker"); err != nil {
		return fmt.Errorf("config validation: %w", err)
	}

	shutdown, err := observe.Init(ctx, observe.Config{
		ServiceName:    "flint-worker",
		ServiceVersion: version,
		LogLevel:       "info",
	})
	if err != nil {
		return fmt.Errorf("observability init: %w", err)
	}
	defer shutdown(ctx)

	log.Info().
		Str("version", version).
		Str("jobNamespace", cfg.Worker.JobNamespaceOrDefault()).
		Msg("flint-worker starting")

	// Database.
	pool, err := dbkit.NewPool(ctx, dbkit.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.PortOrDefault(),
		Database: cfg.Database.Database,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		SSLMode:  cfg.Database.SSLMode,
	})
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	// Engine. The JWT secret signs/verifies task tokens (must match the server;
	// never injected into step pods, unlike the internal token).
	eng := engine.New(pool, []byte(cfg.Auth.JWT.Secret))
	defer eng.Close()

	// Runner pool registry — loaded from the DB (the source of truth; no CRD/
	// controller). Configurable default pool for jobs without an explicit runner.
	runner.SetDefault(cfg.Worker.DefaultRunnerPoolOrDefault())
	registry := runner.NewRegistry()
	q := db.New(pool)
	if err := runner.LoadAll(ctx, q, registry); err != nil {
		log.Warn().Err(err).Msg("runner: failed to load pools from DB (will retry on refresh)")
	}
	// Periodic refresh so admin pool changes (via the API) reach the worker.
	go func() {
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := runner.LoadAll(ctx, q, registry); err != nil {
					log.Debug().Err(err).Msg("runner: pool refresh failed")
				}
			}
		}
	}()

	// Build the executor registry (step exec type → executor). The k8s backend
	// handles run/use/steps via Kubernetes Jobs (with the informer as completion
	// fallback); the http executor runs in-process and reports completion via
	// eng.CompleteStep.
	//
	// FLINT_EXECUTOR=sim swaps the container backend for the simulated executor:
	// no Kubernetes, no pods — steps sleep, emit synthetic logs, and report a
	// scripted outcome, letting the real engine drive runs to completion locally.
	executors := engine.ExecutorRegistry{}
	var k8sClient kubernetes.Interface
	if os.Getenv("FLINT_EXECUTOR") == "sim" {
		container := engine.NewSimExecutor(eng.CompleteStep, &logsink.FilesystemSink{BaseDir: cfg.Storage.FS.Path})
		log.Info().Str("logPath", cfg.Storage.FS.Path).Msg("container executor: sim (no Kubernetes)")
		executors["run"] = container
		executors["use"] = container
		executors["steps"] = container
	} else {
		var kerr error
		k8sClient, kerr = buildK8sClient()
		if kerr != nil {
			log.Warn().Err(kerr).Msg("K8s client unavailable — container steps will not be dispatched (DB-only mode)")
		} else {
			serverURL := fmt.Sprintf("http://flint-server.flint:%d", cfg.Server.PortOrDefault())
			container := engine.NewK8sExecutor(k8sClient, registry, cfg.Worker.AgentImage,
				cfg.Worker.JobNamespaceOrDefault(), serverURL, cfg.Server.InternalToken)
			log.Info().Msg("container executor: kubernetes")
			executors["run"] = container
			executors["use"] = container
			executors["steps"] = container
		}
	}
	// http steps run in-process regardless of the container backend.
	executors["http"] = engine.NewHTTPExecutor(eng.CompleteStep)

	// Worker loop — polls Postgres, dispatches steps via the registry, fires timers.
	loop := engine.NewLoop(eng, executors, engine.LoopConfig{
		SweepInterval: cfg.Worker.SweepIntervalOrDefault(),
		SigningKey:    []byte(cfg.Auth.JWT.Secret),
	})

	// The K8s informer detects Job completions/failures in seconds, catching
	// steps whose agent never reported back.
	if k8sClient != nil {
		go func() {
			watcher := workerinformer.New(k8sClient, eng, pool, workerinformer.Config{
				Namespace: cfg.Worker.JobNamespaceOrDefault(),
			})
			if err := watcher.Run(ctx); err != nil {
				log.Error().Err(err).Msg("K8s informer stopped with error")
			}
		}()
	}

	// Workflow cron scheduler — fires due schedules into the engine. Runs in the
	// worker (alongside the loop) when the Workflows product is enabled.
	if cfg.Products.WorkflowsEnabled() {
		go workflows.NewScheduler(db.New(pool), eng).Run(ctx)
		log.Info().Msg("workflows: cron scheduler enabled")
	}

	log.Info().Msg("flint-worker ready, starting engine loop")
	return loop.Run(ctx)
}

// buildK8sClient creates a kubernetes.Interface using in-cluster config when
// running inside a pod, or falls back to the user's kubeconfig for local
// development. Returns (nil, err) when neither is available.
func buildK8sClient() (kubernetes.Interface, error) {
	// In-cluster: service account token + CA cert are mounted automatically.
	cfg, err := rest.InClusterConfig()
	if err == nil {
		return kubernetes.NewForConfig(cfg)
	}

	// Local dev: try $KUBECONFIG, then ~/.kube/config.
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			kubeconfig = filepath.Join(home, ".kube", "config")
		}
	}
	if kubeconfig == "" {
		return nil, fmt.Errorf("no in-cluster config and no kubeconfig found")
	}

	cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("build kubeconfig: %w", err)
	}
	return kubernetes.NewForConfig(cfg)
}
