package main

import (
	"strings"
	"testing"
)

func TestVersionString(t *testing.T) {
	v := Version()
	if !strings.HasPrefix(v, "ringer ") {
		t.Fatalf("Version() = %q, want prefix %q", v, "ringer ")
	}
}
