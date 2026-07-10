package main

import (
	"os"
	"testing"
)

// TestMain guards against the ensure-HUD-running tests re-exec'ing this test
// binary as `ringer hud ...`: under `go test`, os.Executable() is the test
// binary, so spawnDetachedHUD would otherwise re-run the whole suite
// recursively (a fork bomb). When invoked as the hud helper, exit
// immediately without running any tests.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "hud" {
		os.Exit(0)
	}
	os.Exit(m.Run())
}
