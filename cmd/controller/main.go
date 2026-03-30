package main

import (
	"context"
	"fmt"
	"os"
	"time"

	flintv1 "github.com/NerdMeNot/flint/internal/crd/v1"
	"github.com/NerdMeNot/flint/internal/config"
	"github.com/NerdMeNot/flint/internal/controller"
	"github.com/NerdMeNot/flint/internal/db"
	"github.com/NerdMeNot/flint/internal/dbkit"
	"github.com/NerdMeNot/flint/internal/observe"
	"github.com/NerdMeNot/flint/internal/runner"
	"github.com/rs/zerolog/log"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
)

var (
	version    = "dev"
	commit     = "unknown"
	configPath string
	scheme     = runtime.NewScheme()
)

func init() {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = flintv1.AddToScheme(scheme)
}

func main() {
	root := &cobra.Command{
		Use:     "flint-controller",
		Short:   "Flint CI controller — CRD reconciler for Pipeline, RunnerPool, ForgeConnection",
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

	if !cfg.Controller.Enabled {
		log.Info().Msg("flint-controller is disabled, exiting")
		return nil
	}

	shutdown, err := observe.Init(ctx, observe.Config{
		ServiceName:    "flint-controller",
		ServiceVersion: version,
		LogLevel:       "info",
	})
	if err != nil {
		return fmt.Errorf("observability init: %w", err)
	}
	defer shutdown(ctx)

	log.Info().Str("version", version).Msg("flint-controller starting")

	// Database connection.
	dbCfg := dbkit.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.PortOrDefault(),
		Database: cfg.Database.Database,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		SSLMode:  cfg.Database.SSLMode,
	}
	pool, err := dbkit.NewPool(ctx, dbCfg)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer pool.Close()

	// Controller manager with leader election.
	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                  scheme,
		LeaderElection:          true,
		LeaderElectionID:        "flint-controller-leader",
		LeaderElectionNamespace: "flint",
	})
	if err != nil {
		return fmt.Errorf("creating controller manager: %w", err)
	}

	// Register reconcilers.
	registry := runner.NewRegistry()

	q := db.New(pool)

	if err := (&controller.RunnerPoolReconciler{
		Client:   mgr.GetClient(),
		Q:        q,
		Registry: registry,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setting up RunnerPool controller: %w", err)
	}

	if err := (&controller.PipelineReconciler{
		Client: mgr.GetClient(),
		Q:      q,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setting up Pipeline controller: %w", err)
	}

	if err := (&controller.ForgeConnectionReconciler{
		Client: mgr.GetClient(),
		Q:      q,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setting up ForgeConnection controller: %w", err)
	}

	if err := (&controller.AuthProviderReconciler{
		Client: mgr.GetClient(),
		Q:      q,
	}).SetupWithManager(mgr); err != nil {
		return fmt.Errorf("setting up AuthProvider controller: %w", err)
	}

	// Start orphan detection in background.
	syncCtx, syncCancel := context.WithCancel(ctx)
	defer syncCancel()
	go (&controller.SyncChecker{
		Client:   mgr.GetClient(),
		Q:        q,
		Interval: 10 * time.Minute,
	}).Run(syncCtx)

	log.Info().Msg("starting controller manager")
	return mgr.Start(ctrl.SetupSignalHandler())
}
