package controller

import (
	"context"
	"time"

	flintv1 "github.com/NerdMeNot/flint/internal/core/crd/v1"
	"github.com/NerdMeNot/flint/internal/core/db"
	"github.com/rs/zerolog/log"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SyncChecker periodically compares DB state against K8s CRDs and
// cleans up orphaned rows — DB entries whose source CRD no longer exists.
//
// This handles the case where a CRD is deleted while the controller is
// down and the finalizer never runs.
type SyncChecker struct {
	Client   client.Client
	Q        *db.Queries
	Interval time.Duration
}

// Run starts the sync checker loop. Blocks until context is cancelled.
func (s *SyncChecker) Run(ctx context.Context) {
	interval := s.Interval
	if interval == 0 {
		interval = 10 * time.Minute
	}

	log.Info().Dur("interval", interval).Msg("sync checker started")

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info().Msg("sync checker stopped")
			return
		case <-ticker.C:
			s.checkRunnerPools(ctx)
			// Forge connections and auth providers are now DB-managed (no CRD),
			// so there is nothing to reconcile against. Projects use soft-delete
			// (archive), so orphan detection is unnecessary there too.
		}
	}
}

// checkRunnerPools finds DB rows without a matching RunnerPool CRD.
func (s *SyncChecker) checkRunnerPools(ctx context.Context) {
	dbNames, err := s.Q.ListRunnerPoolNames(ctx)
	if err != nil {
		log.Error().Err(err).Msg("sync: failed to list runner pools from DB")
		return
	}

	for _, name := range dbNames {
		var pool flintv1.RunnerPool
		err := s.Client.Get(ctx, client.ObjectKey{Name: name, Namespace: "flint"}, &pool)
		if err != nil {
			if client.IgnoreNotFound(err) == nil {
				log.Warn().Str("pool", name).Msg("sync: removing orphaned runner pool from DB")
				if err := s.Q.DeleteRunnerPool(ctx, name); err != nil {
					log.Error().Str("pool", name).Err(err).Msg("sync: failed to delete orphaned runner pool")
				}
			}
		}
	}
}
