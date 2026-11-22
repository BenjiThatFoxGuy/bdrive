//go:build linux

package psdconv

import "golang.org/x/sys/unix"

func WorkerSupported() bool { return true }

func setWorkerLimit() error {
	const limit = 1280 << 20
	var current unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_DATA, &current); err != nil {
		return err
	}
	bound := min(uint64(limit), current.Cur, current.Max)
	return unix.Setrlimit(unix.RLIMIT_DATA, &unix.Rlimit{Cur: bound, Max: bound})
}
