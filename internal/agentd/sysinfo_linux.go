package agentd

import "golang.org/x/sys/unix"

// totalMemoryMB reports the machine's physical memory.
func totalMemoryMB() int64 {
	var info unix.Sysinfo_t
	if err := unix.Sysinfo(&info); err != nil {
		return 0
	}
	return int64(info.Totalram) * int64(info.Unit) / (1 << 20)
}

// freeDiskGB reports free space on the data dir's filesystem.
func freeDiskGB(path string) int64 {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0
	}
	return int64(st.Bavail) * st.Bsize / (1 << 30)
}
