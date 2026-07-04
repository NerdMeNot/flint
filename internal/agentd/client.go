package agentd

import (
	"context"
	"crypto/tls"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	agentv1 "github.com/NerdMeNot/flint/protogen/agent/v1"
)

// client wraps the AgentService connection, injecting the machine bearer
// token on every call once registered.
type client struct {
	conn  *grpc.ClientConn
	svc   agentv1.AgentServiceClient
	token string // machine bearer token; empty until registered
}

func dial(cfg Config) (*client, error) {
	c := &client{}
	creds := credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12})
	if cfg.Insecure {
		creds = nil
	}
	opts := []grpc.DialOption{
		grpc.WithUnaryInterceptor(c.authUnary),
		grpc.WithStreamInterceptor(c.authStream),
	}
	if creds != nil {
		opts = append(opts, grpc.WithTransportCredentials(creds))
	} else {
		opts = append(opts, grpc.WithTransportCredentials(insecure.NewCredentials()))
	}
	conn, err := grpc.NewClient(cfg.ServerURL, opts...)
	if err != nil {
		return nil, err
	}
	c.conn = conn
	c.svc = agentv1.NewAgentServiceClient(conn)
	return c, nil
}

func (c *client) close() { _ = c.conn.Close() }

func (c *client) authUnary(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
	return invoker(c.withAuth(ctx), method, req, reply, cc, opts...)
}

func (c *client) authStream(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
	return streamer(c.withAuth(ctx), desc, cc, method, opts...)
}

func (c *client) withAuth(ctx context.Context) context.Context {
	if c.token == "" {
		return ctx
	}
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+c.token)
}
