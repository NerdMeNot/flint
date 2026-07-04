// Package agentgrpc hosts the AgentService gRPC endpoint in the Flint control
// plane — the flint-agent daemon's control channel (registration, work
// claiming, heartbeats, log streaming, completion). It is a thin adapter:
// machine lifecycle logic lives in internal/core/fleet, step completion in the
// engine's CompleteStep chokepoint.
package agentgrpc

import (
	"context"
	"fmt"
	"net"

	"github.com/rs/zerolog/log"
	"google.golang.org/grpc"

	"github.com/NerdMeNot/flint/internal/core/engine"
	"github.com/NerdMeNot/flint/internal/core/fleet"
	"github.com/NerdMeNot/flint/pkg/logsink"
	"github.com/NerdMeNot/flint/pkg/secret"
	agentv1 "github.com/NerdMeNot/flint/protogen/agent/v1"
)

// Config wires the AgentService.
type Config struct {
	// Port the gRPC listener binds (default 9443).
	Port int
	// ServerHTTPURL is handed to agents for the HTTP data-plane endpoints.
	ServerHTTPURL string
}

// Server is the AgentService implementation.
type Server struct {
	agentv1.UnimplementedAgentServiceServer

	fleet   *fleet.Fleet
	eng     engine.Engine
	logs    logsink.LogSink
	secrets secret.SecretStore // nil = secret injection unavailable
	cfg     Config

	grpc *grpc.Server
}

// New builds the AgentService server. secrets may be nil (no master key
// configured) — GetStepSecrets then reports the store unavailable.
func New(f *fleet.Fleet, eng engine.Engine, logs logsink.LogSink, secrets secret.SecretStore, cfg Config) *Server {
	if cfg.Port == 0 {
		cfg.Port = 9443
	}
	s := &Server{fleet: f, eng: eng, logs: logs, secrets: secrets, cfg: cfg}
	s.grpc = grpc.NewServer(
		grpc.ChainUnaryInterceptor(s.authUnaryInterceptor),
		grpc.ChainStreamInterceptor(s.authStreamInterceptor),
	)
	agentv1.RegisterAgentServiceServer(s.grpc, s)
	return s
}

// ListenAndServe blocks serving gRPC until ctx is done, then drains.
func (s *Server) ListenAndServe(ctx context.Context) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", s.cfg.Port))
	if err != nil {
		return fmt.Errorf("agentgrpc: listen :%d: %w", s.cfg.Port, err)
	}
	go func() {
		<-ctx.Done()
		s.grpc.GracefulStop()
	}()
	log.Info().Int("port", s.cfg.Port).Msg("agentgrpc: AgentService listening")
	return s.grpc.Serve(lis)
}

// GRPCServer exposes the underlying server for bufconn test harnesses.
func (s *Server) GRPCServer() *grpc.Server { return s.grpc }
