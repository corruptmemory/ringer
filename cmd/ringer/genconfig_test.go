package main

import (
	"os"
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/config"
)

// The committed config.sample.toml must be exactly what gen-config emits, so it
// can never drift from the config structs again.
func TestConfigSampleIsFresh(t *testing.T) {
	want, err := config.RenderDocumented(config.ExampleConfig())
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile("../../config.sample.toml")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("config.sample.toml is stale — regenerate with `ringer gen-config -o config.sample.toml`.\nDiff first lines:\n got: %q\nwant: %q", first(string(got)), first(want))
	}
}

func first(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
