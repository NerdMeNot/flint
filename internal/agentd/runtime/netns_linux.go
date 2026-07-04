package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"

	"golang.org/x/sys/unix"
)

// netnsDir holds the bind-mounted network namespace files that keep each
// step's netns alive between container starts (tmpfs — gone on reboot).
const netnsDir = "/run/flint-agent/netns"

// createNetNS creates a new empty network namespace and pins it to a bind
// mount at the returned path, so it outlives the creating thread and can be
// joined by the step and service containers.
//
// The classic pinning dance: a locked goroutine unshares CLONE_NEWNET (only
// its thread enters the new ns), bind-mounts its own /proc/<tid>/ns/net, and
// then exits WITHOUT unlocking — Go destroys the polluted thread instead of
// reusing it.
func createNetNS(id string) (string, error) {
	if err := os.MkdirAll(netnsDir, 0o711); err != nil {
		return "", err
	}
	path := filepath.Join(netnsDir, id)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o444)
	if err != nil {
		return "", fmt.Errorf("netns: create mount point: %w", err)
	}
	_ = f.Close()

	errCh := make(chan error, 1)
	go func() {
		goruntime.LockOSThread()
		// Deliberately no UnlockOSThread: the thread now lives in the wrong
		// netns; letting the goroutine exit locked retires the thread.
		if err := unix.Unshare(unix.CLONE_NEWNET); err != nil {
			errCh <- fmt.Errorf("netns: unshare: %w", err)
			return
		}
		src := fmt.Sprintf("/proc/self/task/%d/ns/net", unix.Gettid())
		errCh <- unix.Mount(src, path, "bind", unix.MS_BIND, "")
	}()
	if err := <-errCh; err != nil {
		_ = os.Remove(path)
		return "", err
	}
	return path, nil
}

// removeNetNS unpins and deletes a namespace created by createNetNS.
func removeNetNS(path string) {
	_ = unix.Unmount(path, unix.MNT_DETACH)
	_ = os.Remove(path)
}

// sweepNetNS removes stale pinned namespaces from a previous agent process
// (fail-and-retry crash model: their steps were failed and rescheduled).
func sweepNetNS() int {
	entries, err := os.ReadDir(netnsDir)
	if err != nil {
		return 0
	}
	for _, e := range entries {
		removeNetNS(filepath.Join(netnsDir, e.Name()))
	}
	return len(entries)
}
