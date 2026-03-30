package controller

import (
	"context"
	"time"

	flintv1 "github.com/NerdMeNot/flint/internal/crd/v1"
	"github.com/NerdMeNot/flint/internal/db"
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
			s.checkForgeConnections(ctx)
			s.checkAuthProviders(ctx)
			// Pipeline projects use soft-delete (archive), so orphan
			// detection is less critical — archived projects are harmless.
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

// checkAuthProviders finds DB rows without a matching AuthProvider CRD.
func (s *SyncChecker) checkAuthProviders(ctx context.Context) {
	dbRows, err := s.Q.ListAuthProviderConfigNames(ctx)
	if err != nil {
		log.Error().Err(err).Msg("sync: failed to list auth providers from DB")
		return
	}

	var crdList flintv1.AuthProviderList
	if err := s.Client.List(ctx, &crdList, client.InNamespace("flint")); err != nil {
		log.Error().Err(err).Msg("sync: failed to list AuthProvider CRDs")
		return
	}

	crdNames := make(map[string]bool, len(crdList.Items))
	for _, ap := range crdList.Items {
		crdNames[ap.Name] = true
	}

	for _, row := range dbRows {
		if !crdNames[row.DisplayName] {
			log.Warn().Str("name", row.DisplayName).Str("id", row.ID).Msg("sync: removing orphaned auth provider from DB")
			if _, err := s.Q.DeleteAuthProviderConfig(ctx, row.ProviderType); err != nil {
				log.Error().Str("id", row.ID).Err(err).Msg("sync: failed to delete orphaned auth provider")
			}
		}
	}
}

// checkForgeConnections finds DB rows without a matching ForgeConnection CRD.
func (s *SyncChecker) checkForgeConnections(ctx context.Context) {
	dbRows, err := s.Q.ListForgeConnectionNames(ctx)
	if err != nil {
		log.Error().Err(err).Msg("sync: failed to list forge connections from DB")
		return
	}

	// List all ForgeConnection CRDs.
	var crdList flintv1.ForgeConnectionList
	if err := s.Client.List(ctx, &crdList, client.InNamespace("flint")); err != nil {
		log.Error().Err(err).Msg("sync: failed to list ForgeConnection CRDs")
		return
	}

	crdNames := make(map[string]bool, len(crdList.Items))
	for _, fc := range crdList.Items {
		crdNames[fc.Name] = true
	}

	for _, row := range dbRows {
		if !crdNames[row.DisplayName] {
			log.Warn().Str("name", row.DisplayName).Str("id", row.ID).Msg("sync: removing orphaned forge connection from DB")
			if err := s.Q.DeleteForgeConnectionByID(ctx, row.ID); err != nil {
				log.Error().Str("id", row.ID).Err(err).Msg("sync: failed to delete orphaned forge connection")
			}
		}
	}
}
