//go:build unix

package agent

import "syscall"

// SystemDiskUsage reports the total and used bytes of the filesystem holding
// path, via statfs. Used is derived as total minus free rather than total
// minus available: available excludes the root-reserved blocks, which would
// make a freshly formatted ext4 look ~5% full.
func SystemDiskUsage(path string) (total, used int64, err error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0, err
	}
	bsize := int64(st.Bsize)
	total = int64(st.Blocks) * bsize
	used = total - int64(st.Bfree)*bsize
	return total, used, nil
}
