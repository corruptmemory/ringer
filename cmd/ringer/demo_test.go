package main

import (
	"strings"
	"testing"

	"github.com/corruptmemory/ringer/internal/manifest"
)

// TestBuildDemoManifestProducesValidTasks is the RED/GREEN anchor for Task
// 11: the demo's manifest-builder must produce bytes that pass the exact
// same validation `run` applies to a user-supplied manifest
// (manifest.FromBytes), with the three task shapes the brief calls for: a
// plain pass, a multi-file pass, and a fail-then-retry-pass (MOCK_FAIL_ONCE),
// mirroring internal/runner/runner_test.go's proven mock manifest shapes.
func TestBuildDemoManifestProducesValidTasks(t *testing.T) {
	data, err := buildDemoManifest(t.TempDir())
	if err != nil {
		t.Fatalf("buildDemoManifest: %v", err)
	}

	m, err := manifest.FromBytes(data)
	if err != nil {
		t.Fatalf("manifest.FromBytes rejected demo manifest: %v", err)
	}
	if len(m.Tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d: %+v", len(m.Tasks), m.Tasks)
	}

	byKey := map[string]manifest.Task{}
	for _, tk := range m.Tasks {
		byKey[tk.Key] = tk
	}
	for _, key := range []string{"alpha", "bravo", "charlie"} {
		tk, ok := byKey[key]
		if !ok {
			t.Fatalf("missing task %q", key)
		}
		if tk.Engine != "mock" {
			t.Errorf("task %s: Engine = %q, want %q", key, tk.Engine, "mock")
		}
		if tk.Check == "" {
			t.Errorf("task %s: Check is empty", key)
		}
		if len(tk.ExpectFiles) == 0 {
			t.Errorf("task %s: ExpectFiles is empty", key)
		}
	}

	// alpha: a single plain MOCK_FILE write, no fail directive — passes on
	// the first attempt.
	if n := strings.Count(byKey["alpha"].Spec, "MOCK_FILE:"); n != 1 {
		t.Errorf("alpha: expected 1 MOCK_FILE block, got %d", n)
	}
	if strings.Contains(byKey["alpha"].Spec, "MOCK_FAIL") {
		t.Errorf("alpha: should not contain a fail directive")
	}

	// bravo: multi-file — two MOCK_FILE blocks in one spec, two expected files.
	if n := strings.Count(byKey["bravo"].Spec, "MOCK_FILE:"); n != 2 {
		t.Errorf("bravo: expected 2 MOCK_FILE blocks, got %d", n)
	}
	if len(byKey["bravo"].ExpectFiles) != 2 {
		t.Errorf("bravo: expected 2 ExpectFiles, got %d: %v", len(byKey["bravo"].ExpectFiles), byKey["bravo"].ExpectFiles)
	}

	// charlie: fail-then-retry-pass via the MOCK_FAIL_ONCE sentinel, followed
	// by a single MOCK_FILE write that only lands on the retry.
	if !strings.Contains(byKey["charlie"].Spec, "MOCK_FAIL_ONCE") {
		t.Errorf("charlie: expected MOCK_FAIL_ONCE directive in spec")
	}
	if n := strings.Count(byKey["charlie"].Spec, "MOCK_FILE:"); n != 1 {
		t.Errorf("charlie: expected 1 MOCK_FILE block after the fail-once, got %d", n)
	}
}
