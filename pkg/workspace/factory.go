package workspace

import "fmt"

// New creates a Workspace backend based on the mode.
//
//   - "agent" or "": AgentWorkspace (gRPC workspace agent)
//   - "s3": S3Workspace (S3-backed incremental sync)
//   - "pvc": PVCWorkspace (no-op, shared volume)
//
// For "agent" mode, addr and token are required.
// For "s3" mode, bucket, region, and runID are required.
func New(mode, addr, token, bucket, region, runID string) (Workspace, error) {
	switch mode {
	case "pvc":
		return NewPVC(), nil
	case "s3":
		if bucket == "" {
			return nil, fmt.Errorf("workspace: mode=s3 requires bucket")
		}
		return NewS3(bucket, region, runID)
	case "agent", "":
		if addr == "" {
			// No workspace agent available. Fall back to S3 if configured.
			if bucket != "" {
				return NewS3(bucket, region, runID)
			}
			// No backend available — return nil (workspace sync disabled).
			return nil, nil
		}
		return NewAgent(addr, token)
	default:
		return nil, fmt.Errorf("workspace: unknown mode %q", mode)
	}
}
