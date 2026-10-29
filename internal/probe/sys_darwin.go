//go:build darwin

package probe

import (
	"context"
	"strconv"
	"syscall"
)

func diskFree(dir string) (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

func memTotal(ctx context.Context) (uint64, error) {
	v, err := firstLine(ctx, "sysctl", "-n", "hw.memsize")
	if err != nil {
		return 0, err
	}
	return strconv.ParseUint(v, 10, 64)
}
