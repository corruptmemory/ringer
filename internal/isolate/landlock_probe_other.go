//go:build !linux

package isolate

// LandlockABI reports Landlock as unavailable off Linux.
func LandlockABI() (int, bool) { return 0, false }
