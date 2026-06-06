package wsagent

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// wsTokenHeader is the metadata key that step pods use to authenticate.
// Value must equal the run-scoped token injected at dispatch time.
const wsTokenHeader = "x-flint-ws-token"

// NewTokenInterceptors returns a matched pair of unary and stream server
// interceptors that validate the x-flint-ws-token metadata header.
// Every inbound RPC must carry the correct token or it is rejected with
// codes.Unauthenticated before reaching any handler.
func NewTokenInterceptors(token string) (grpc.UnaryServerInterceptor, grpc.StreamServerInterceptor) {
	validate := func(ctx context.Context) error {
		md, ok := metadata.FromIncomingContext(ctx)
		if !ok {
			return status.Error(codes.Unauthenticated, "missing gRPC metadata")
		}
		vals := md.Get(wsTokenHeader)
		if len(vals) == 0 || vals[0] != token {
			return status.Error(codes.Unauthenticated, "invalid workspace token")
		}
		return nil
	}

	unary := func(
		ctx context.Context,
		req any,
		_ *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		if err := validate(ctx); err != nil {
			return nil, err
		}
		return handler(ctx, req)
	}

	stream := func(
		srv any,
		ss grpc.ServerStream,
		_ *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) error {
		if err := validate(ss.Context()); err != nil {
			return err
		}
		return handler(srv, ss)
	}

	return unary, stream
}
