package wsagent

import (
	"context"
	"io"
	"net"
	"testing"

	workspacev1 "github.com/NerdMeNot/flint/protogen/workspace/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const testToken = "test-run-token-abc123"

// testEnv holds everything needed for an in-process gRPC test.
type testEnv struct {
	client workspacev1.WorkspaceServiceClient
	conn   *grpc.ClientConn
	srv    *grpc.Server
}

// newTestEnv creates a Server backed by a temp dir, wires it up with auth
// interceptors on a bufconn listener, and returns a connected client.
// The caller should defer env.close().
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	dir := t.TempDir()
	ws, err := NewServer(dir)
	require.NoError(t, err)

	unaryInt, streamInt := NewTokenInterceptors(testToken)
	grpcSrv := grpc.NewServer(
		grpc.ChainUnaryInterceptor(unaryInt),
		grpc.ChainStreamInterceptor(streamInt),
	)
	workspacev1.RegisterWorkspaceServiceServer(grpcSrv, ws)

	lis := bufconn.Listen(1024 * 1024)
	go func() {
		if err := grpcSrv.Serve(lis); err != nil {
			// Serve returns after GracefulStop; ignore.
		}
	}()

	conn, err := grpc.NewClient(
		"passthrough://bufconn",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	t.Cleanup(func() {
		conn.Close()
		grpcSrv.GracefulStop()
	})

	return &testEnv{
		client: workspacev1.NewWorkspaceServiceClient(conn),
		conn:   conn,
		srv:    grpcSrv,
	}
}

// authedCtx returns a context that carries the valid test token.
func authedCtx() context.Context {
	md := metadata.Pairs(wsTokenHeader, testToken)
	return metadata.NewOutgoingContext(context.Background(), md)
}

// putFile is a helper that streams a file via Put.
func putFile(t *testing.T, client workspacev1.WorkspaceServiceClient, path string, content []byte) *workspacev1.PutResponse {
	t.Helper()
	ctx := authedCtx()

	stream, err := client.Put(ctx)
	require.NoError(t, err)

	// Send path message.
	err = stream.Send(&workspacev1.PutRequest{
		Data: &workspacev1.PutRequest_Path{Path: path},
	})
	require.NoError(t, err)

	// Send content in chunks of 64 KiB to exercise multi-chunk path.
	const chunkSize = 64 * 1024
	for i := 0; i < len(content); i += chunkSize {
		end := i + chunkSize
		if end > len(content) {
			end = len(content)
		}
		err = stream.Send(&workspacev1.PutRequest{
			Data: &workspacev1.PutRequest_Chunk{Chunk: content[i:end]},
		})
		require.NoError(t, err)
	}

	resp, err := stream.CloseAndRecv()
	require.NoError(t, err)
	return resp
}

// getFile is a helper that reads a file via Get and returns the full content.
func getFile(t *testing.T, client workspacev1.WorkspaceServiceClient, path string) []byte {
	t.Helper()
	ctx := authedCtx()

	stream, err := client.Get(ctx, &workspacev1.GetRequest{Path: path})
	require.NoError(t, err)

	var buf []byte
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		buf = append(buf, resp.Chunk...)
	}
	return buf
}

// ─────────────────────────────────────────────────────────────
// Tests
// ─────────────────────────────────────────────────────────────

func TestPutGetRoundTrip(t *testing.T) {
	env := newTestEnv(t)

	content := []byte("hello, workspace agent!")
	putResp := putFile(t, env.client, "greeting.txt", content)
	assert.Equal(t, int64(len(content)), putResp.BytesWritten)

	got := getFile(t, env.client, "greeting.txt")
	assert.Equal(t, content, got)
}

func TestPutGetRoundTripLargeFile(t *testing.T) {
	env := newTestEnv(t)

	// 300 KiB — forces multiple chunks in both Put and Get.
	content := make([]byte, 300*1024)
	for i := range content {
		content[i] = byte(i % 251) // non-trivial pattern
	}

	putResp := putFile(t, env.client, "data/large.bin", content)
	assert.Equal(t, int64(len(content)), putResp.BytesWritten)

	got := getFile(t, env.client, "data/large.bin")
	assert.Equal(t, content, got)
}

func TestPutUpdatesManifest(t *testing.T) {
	env := newTestEnv(t)

	content := []byte("manifest-test-content")
	putFile(t, env.client, "src/main.go", content)

	ctx := authedCtx()
	manifest, err := env.client.GetManifest(ctx, &workspacev1.GetManifestRequest{})
	require.NoError(t, err)

	require.Len(t, manifest.Entries, 1)
	entry := manifest.Entries[0]
	assert.Equal(t, "src/main.go", entry.Path)
	assert.Equal(t, int64(len(content)), entry.Size)
	assert.NotEmpty(t, entry.Hash, "hash should be populated")
	assert.Greater(t, entry.MtimeUnix, int64(0), "mtime should be set")
}

func TestRemoveUpdatesManifest(t *testing.T) {
	env := newTestEnv(t)
	ctx := authedCtx()

	// Put a file.
	putFile(t, env.client, "to-delete.txt", []byte("doomed"))

	// Verify it's in the manifest.
	m1, err := env.client.GetManifest(ctx, &workspacev1.GetManifestRequest{})
	require.NoError(t, err)
	require.Len(t, m1.Entries, 1)
	v1 := m1.Version

	// Remove the file.
	_, err = env.client.Remove(ctx, &workspacev1.RemoveRequest{Path: "to-delete.txt"})
	require.NoError(t, err)

	// Manifest should be empty and version should have incremented.
	m2, err := env.client.GetManifest(ctx, &workspacev1.GetManifestRequest{})
	require.NoError(t, err)
	assert.Empty(t, m2.Entries)
	assert.Greater(t, m2.Version, v1, "version should increment after Remove")
}

func TestGetManifestVersionTracking(t *testing.T) {
	env := newTestEnv(t)
	ctx := authedCtx()

	// Initial version is 0.
	m0, err := env.client.GetManifest(ctx, &workspacev1.GetManifestRequest{})
	require.NoError(t, err)
	assert.Equal(t, int64(0), m0.Version)

	// Put increments version to 1.
	putFile(t, env.client, "a.txt", []byte("a"))
	m1, err := env.client.GetManifest(ctx, &workspacev1.GetManifestRequest{})
	require.NoError(t, err)
	assert.Equal(t, int64(1), m1.Version)

	// Second Put increments to 2.
	putFile(t, env.client, "b.txt", []byte("b"))
	m2, err := env.client.GetManifest(ctx, &workspacev1.GetManifestRequest{})
	require.NoError(t, err)
	assert.Equal(t, int64(2), m2.Version)

	// Remove increments to 3.
	_, err = env.client.Remove(ctx, &workspacev1.RemoveRequest{Path: "a.txt"})
	require.NoError(t, err)
	m3, err := env.client.GetManifest(ctx, &workspacev1.GetManifestRequest{})
	require.NoError(t, err)
	assert.Equal(t, int64(3), m3.Version)

	// Overwriting an existing file also increments.
	putFile(t, env.client, "b.txt", []byte("b-updated"))
	m4, err := env.client.GetManifest(ctx, &workspacev1.GetManifestRequest{})
	require.NoError(t, err)
	assert.Equal(t, int64(4), m4.Version)
}

func TestStat(t *testing.T) {
	env := newTestEnv(t)
	ctx := authedCtx()

	content := []byte("stat me")
	putFile(t, env.client, "info.txt", content)

	resp, err := env.client.Stat(ctx, &workspacev1.StatRequest{Path: "info.txt"})
	require.NoError(t, err)

	assert.Equal(t, "info.txt", resp.Name)
	assert.Equal(t, int64(len(content)), resp.Size)
	assert.False(t, resp.IsDir)
	assert.Greater(t, resp.ModTimeUnix, int64(0))
}

func TestStatNotFound(t *testing.T) {
	env := newTestEnv(t)
	ctx := authedCtx()

	_, err := env.client.Stat(ctx, &workspacev1.StatRequest{Path: "nonexistent.txt"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestReadDir(t *testing.T) {
	env := newTestEnv(t)
	ctx := authedCtx()

	// Create several files under sub/.
	putFile(t, env.client, "sub/alpha.txt", []byte("a"))
	putFile(t, env.client, "sub/beta.txt", []byte("bb"))
	putFile(t, env.client, "sub/gamma.txt", []byte("ccc"))

	resp, err := env.client.ReadDir(ctx, &workspacev1.ReadDirRequest{Path: "sub"})
	require.NoError(t, err)

	names := make(map[string]int64)
	for _, e := range resp.Entries {
		names[e.Name] = e.Size
		assert.False(t, e.IsDir)
	}

	assert.Len(t, names, 3)
	assert.Equal(t, int64(1), names["alpha.txt"])
	assert.Equal(t, int64(2), names["beta.txt"])
	assert.Equal(t, int64(3), names["gamma.txt"])
}

func TestReadDirIncludesSubdirectories(t *testing.T) {
	env := newTestEnv(t)
	ctx := authedCtx()

	// Create a file nested two levels deep — this forces creation of parent dirs.
	putFile(t, env.client, "root/child/deep.txt", []byte("deep"))
	putFile(t, env.client, "root/sibling.txt", []byte("flat"))

	resp, err := env.client.ReadDir(ctx, &workspacev1.ReadDirRequest{Path: "root"})
	require.NoError(t, err)

	entries := make(map[string]bool) // name -> isDir
	for _, e := range resp.Entries {
		entries[e.Name] = e.IsDir
	}

	assert.True(t, entries["child"], "child should be a directory")
	assert.False(t, entries["sibling.txt"], "sibling.txt should be a file")
}

func TestMkdirAll(t *testing.T) {
	env := newTestEnv(t)
	ctx := authedCtx()

	_, err := env.client.MkdirAll(ctx, &workspacev1.MkdirAllRequest{Path: "a/b/c"})
	require.NoError(t, err)

	// Stat the created directory.
	resp, err := env.client.Stat(ctx, &workspacev1.StatRequest{Path: "a/b/c"})
	require.NoError(t, err)
	assert.True(t, resp.IsDir)
}

func TestMkdirAllIdempotent(t *testing.T) {
	env := newTestEnv(t)
	ctx := authedCtx()

	_, err := env.client.MkdirAll(ctx, &workspacev1.MkdirAllRequest{Path: "x/y"})
	require.NoError(t, err)

	// Calling again should not error.
	_, err = env.client.MkdirAll(ctx, &workspacev1.MkdirAllRequest{Path: "x/y"})
	require.NoError(t, err)
}

// ─────────────────────────────────────────────────────────────
// Auth interceptor tests
// ─────────────────────────────────────────────────────────────

func TestAuthValidToken(t *testing.T) {
	env := newTestEnv(t)
	ctx := authedCtx()

	// A simple unary RPC should succeed with the correct token.
	_, err := env.client.GetManifest(ctx, &workspacev1.GetManifestRequest{})
	require.NoError(t, err)
}

func TestAuthValidTokenStreaming(t *testing.T) {
	env := newTestEnv(t)

	// Put (client-streaming) should succeed with the correct token.
	content := []byte("auth ok")
	putResp := putFile(t, env.client, "authed.txt", content)
	assert.Equal(t, int64(len(content)), putResp.BytesWritten)

	// Get (server-streaming) should succeed with the correct token.
	got := getFile(t, env.client, "authed.txt")
	assert.Equal(t, content, got)
}

func TestAuthInvalidToken(t *testing.T) {
	env := newTestEnv(t)

	// Unary RPC with wrong token.
	md := metadata.Pairs(wsTokenHeader, "wrong-token")
	ctx := metadata.NewOutgoingContext(context.Background(), md)

	_, err := env.client.GetManifest(ctx, &workspacev1.GetManifestRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthInvalidTokenStreaming(t *testing.T) {
	env := newTestEnv(t)

	// Client-streaming Put with wrong token.
	md := metadata.Pairs(wsTokenHeader, "wrong-token")
	ctx := metadata.NewOutgoingContext(context.Background(), md)

	stream, err := env.client.Put(ctx)
	require.NoError(t, err, "stream creation itself should not fail")

	err = stream.Send(&workspacev1.PutRequest{
		Data: &workspacev1.PutRequest_Path{Path: "nope.txt"},
	})
	// The error may surface on Send or CloseAndRecv depending on timing.
	if err == nil {
		_, err = stream.CloseAndRecv()
	}
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthMissingToken(t *testing.T) {
	env := newTestEnv(t)

	// Unary RPC with no token at all.
	ctx := context.Background()

	_, err := env.client.GetManifest(ctx, &workspacev1.GetManifestRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestAuthMissingTokenStreaming(t *testing.T) {
	env := newTestEnv(t)

	// Server-streaming Get with no token.
	ctx := context.Background()

	stream, err := env.client.Get(ctx, &workspacev1.GetRequest{Path: "anything.txt"})
	// Error may surface here or on first Recv.
	if err == nil {
		_, err = stream.Recv()
	}
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

// ─────────────────────────────────────────────────────────────
// Edge cases
// ─────────────────────────────────────────────────────────────

func TestPutEmptyFile(t *testing.T) {
	env := newTestEnv(t)

	// Put a zero-byte file (path message only, no chunks).
	putResp := putFile(t, env.client, "empty.txt", nil)
	assert.Equal(t, int64(0), putResp.BytesWritten)

	// Get should return empty content.
	got := getFile(t, env.client, "empty.txt")
	assert.Empty(t, got)

	// Manifest should track it.
	ctx := authedCtx()
	m, err := env.client.GetManifest(ctx, &workspacev1.GetManifestRequest{})
	require.NoError(t, err)
	require.Len(t, m.Entries, 1)
	assert.Equal(t, int64(0), m.Entries[0].Size)
}

func TestGetNotFound(t *testing.T) {
	env := newTestEnv(t)
	ctx := authedCtx()

	stream, err := env.client.Get(ctx, &workspacev1.GetRequest{Path: "does-not-exist.txt"})
	require.NoError(t, err, "stream creation should succeed")

	_, err = stream.Recv()
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestRemoveNotFound(t *testing.T) {
	env := newTestEnv(t)
	ctx := authedCtx()

	_, err := env.client.Remove(ctx, &workspacev1.RemoveRequest{Path: "ghost.txt"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, status.Code(err))
}

func TestPutOverwriteUpdatesManifest(t *testing.T) {
	env := newTestEnv(t)
	ctx := authedCtx()

	putFile(t, env.client, "file.txt", []byte("version1"))
	m1, err := env.client.GetManifest(ctx, &workspacev1.GetManifestRequest{})
	require.NoError(t, err)
	require.Len(t, m1.Entries, 1)
	hash1 := m1.Entries[0].Hash

	putFile(t, env.client, "file.txt", []byte("version2-different"))
	m2, err := env.client.GetManifest(ctx, &workspacev1.GetManifestRequest{})
	require.NoError(t, err)
	require.Len(t, m2.Entries, 1)
	hash2 := m2.Entries[0].Hash

	assert.NotEqual(t, hash1, hash2, "hash should change when content changes")
	assert.Equal(t, int64(len("version2-different")), m2.Entries[0].Size)
}
