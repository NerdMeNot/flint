// Package workspace manages the shared working directory between pipeline
// steps. It provides a single Workspace interface with three backends:
//
//   - AgentWorkspace: emptyDir per pod + gRPC workspace agent for incremental sync
//   - S3Workspace: emptyDir per pod + S3 for incremental sync
//   - PVCWorkspace: shared ReadWriteMany volume, no sync needed
//
// The agent calls SyncIn before each step and SyncOut after each step.
// The workspace backend handles manifests, diffing, and file transfer
// internally — the agent doesn't know about xxh3 hashes or manifest formats.
package workspace

import "context"

// Workspace manages incremental sync of the working directory between steps.
//
// All implementations are safe for concurrent use.
type Workspace interface {
	// SyncIn pulls the remote workspace state into the local working directory.
	// Only files that are new or changed on the remote are downloaded.
	// Returns (true, nil) if sync occurred, (false, nil) if the remote is empty
	// (first step — no previous state to sync).
	SyncIn(ctx context.Context, workDir string) (bool, error)

	// SyncOut pushes the local working directory to the remote workspace.
	// Only files that are new or changed locally are uploaded.
	// Files deleted locally are removed from the remote.
	SyncOut(ctx context.Context, workDir string) error

	// Close releases any resources (gRPC connections, S3 clients).
	Close() error
}
