//go:build !linux

package agentd

// Non-linux builds exist for development (hostshell runtime); capacity
// self-reporting is best-effort there.
func totalMemoryMB() int64      { return 0 }
func freeDiskGB(_ string) int64 { return 0 }
