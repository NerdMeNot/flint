package agentgrpc

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/NerdMeNot/flint/internal/core/db"
)

type machineCtxKey struct{}

// authUnaryInterceptor enforces the machine bearer token on everything except
// RegisterMachine (which authenticates with its own registration token).
func (s *Server) authUnaryInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	if strings.HasSuffix(info.FullMethod, "/RegisterMachine") {
		return handler(ctx, req)
	}
	mctx, err := s.authenticate(ctx)
	if err != nil {
		return nil, err
	}
	return handler(mctx, req)
}

func (s *Server) authStreamInterceptor(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
	mctx, err := s.authenticate(ss.Context())
	if err != nil {
		return err
	}
	return handler(srv, &wrappedStream{ServerStream: ss, ctx: mctx})
}

// authenticate resolves the bearer token to a live machine row and stores it
// in the context.
func (s *Server) authenticate(ctx context.Context) (context.Context, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}
	values := md.Get("authorization")
	if len(values) == 0 {
		return nil, status.Error(codes.Unauthenticated, "missing authorization")
	}
	token, ok := strings.CutPrefix(values[0], "Bearer ")
	if !ok || token == "" {
		return nil, status.Error(codes.Unauthenticated, "malformed authorization header")
	}
	machine, err := s.fleet.AuthenticateMachine(ctx, token)
	if err != nil {
		return nil, status.Error(codes.Unauthenticated, "unknown or retired machine token")
	}
	return context.WithValue(ctx, machineCtxKey{}, machine), nil
}

// machineFromCtx returns the authenticated machine row.
func machineFromCtx(ctx context.Context) (db.Machine, bool) {
	m, ok := ctx.Value(machineCtxKey{}).(db.Machine)
	return m, ok
}

type wrappedStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (w *wrappedStream) Context() context.Context { return w.ctx }
