//go:build !unix

package agent

import "errors"

// SystemDiskUsage is unavailable off Unix. The agent only ever runs on Linux;
// this stub exists so the module still builds on the development machine, and
// returning an error means CollectDisks reports no disks — which the server
// reads as "could not determine" and leaves the record alone.
func SystemDiskUsage(string) (int64, int64, error) {
	return 0, 0, errors.New("filesystem usage is only available on Unix")
}
