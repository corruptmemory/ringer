//go:build linux

package main

import (
	"testing"

	"github.com/corruptmemory/ringer/internal/isolate"
)

func TestLandlockAvailableOnThisHost(t *testing.T) {
	if _, ok := isolate.LandlockABI(); !ok {
		t.Skip("kernel lacks Landlock; trampoline's defensive refusal path is the safe default")
	}
	// Present: the trampoline will proceed to enforce rather than refuse.
}
