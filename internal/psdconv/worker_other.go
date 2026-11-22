//go:build !linux

package psdconv

func WorkerSupported() bool { return false }
func setWorkerLimit() error { return ErrUnsupportedPreview }
