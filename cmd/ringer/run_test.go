package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/isolate"
	"github.com/corruptmemory/ringer/internal/jail"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/manifest"
	"github.com/jessevdk/go-flags"
)

// TestSelectIsolator covers the branch logic the run.go isolation-selection
// loop used to carry inline with no committed test: skip entirely when no
// task's resolved engine asks for isolation="jail", select a real backend
// when one does, and — the key regression guard the Task 10 review flagged
// — still skip when the ONLY jail-requesting task is full_access:true (spec
// §6: full_access takes the unconfined lane and must never trigger
// selection, or a refusal, on its own).
func TestSelectIsolator(t *testing.T) {
	jailOK := jail.CheckUnsharePreflight().OK()
	jailEngines := map[string]config.EngineConfig{
		"jailed": {Bin: "sh", Isolation: "jail"},
	}

	tests := []struct {
		name    string
		tasks   []manifest.Task
		engines map[string]config.EngineConfig
		// skipUnlessJailOK marks a row that only makes a firm assertion when
		// this host actually supports the jail backend (userns preflight);
		// on a host without it, Select would fall through to Landlock or
		// refuse, which is a different (and separately-tested) code path.
		skipUnlessJailOK bool
		wantNil          bool
	}{
		{
			name:    "no jail engine -> nil, nil",
			tasks:   []manifest.Task{{Key: "a", Engine: "plain"}},
			engines: map[string]config.EngineConfig{"plain": {Bin: "sh"}},
			wantNil: true,
		},
		{
			name:             "jail engine present -> a real isolator",
			tasks:            []manifest.Task{{Key: "a", Engine: "jailed"}},
			engines:          jailEngines,
			skipUnlessJailOK: true,
			wantNil:          false,
		},
		{
			name:    "jail engine but the only task is full_access -> nil, nil (skip path)",
			tasks:   []manifest.Task{{Key: "a", Engine: "jailed", FullAccess: true}},
			engines: jailEngines,
			wantNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.skipUnlessJailOK && !jailOK {
				t.Skip("userns preflight failed on this host; jail-selection assertion doesn't apply")
			}
			m := &manifest.Manifest{Workdir: t.TempDir(), Tasks: tc.tasks}
			iso, err := selectIsolator(m, tc.engines, logging.Default())
			if err != nil {
				t.Fatalf("selectIsolator: %v", err)
			}
			if tc.wantNil {
				if iso != nil {
					t.Fatalf("iso = %T, want nil", iso)
				}
				return
			}
			if iso == nil {
				t.Fatalf("iso = nil, want a non-nil isolator")
			}
			if _, ok := iso.(*isolate.JailIsolator); !ok {
				t.Fatalf("iso = %T, want *isolate.JailIsolator (jail preflight OK on this host)", iso)
			}
		})
	}
}

// TestPortFlagParsing drives the real go-flags parser (like
// TestModelsFlagParsing) to prove runCmd and demoCmd both carry a --port
// flag that binds into their Port field. Port *resolution* (precedence
// between the flag, [hud] config, and the 8700 default) is unit-tested in
// internal/config; this just proves the flag is wired.
func TestPortFlagParsing(t *testing.T) {
	t.Run("runCmd --port", func(t *testing.T) {
		var c runCmd
		p := flags.NewParser(&c, flags.None)
		if _, err := p.ParseArgs([]string{"--port", "9100", "manifest.json"}); err != nil {
			t.Fatalf("ParseArgs: %v", err)
		}
		if c.Port != 9100 {
			t.Errorf("Port = %d, want 9100", c.Port)
		}
	})

	t.Run("demoCmd --port", func(t *testing.T) {
		var c demoCmd
		p := flags.NewParser(&c, flags.None)
		if _, err := p.ParseArgs([]string{"--port", "9100"}); err != nil {
			t.Fatalf("ParseArgs: %v", err)
		}
		if c.Port != 9100 {
			t.Errorf("Port = %d, want 9100", c.Port)
		}
	})
}

// TestRunManifestFileDryRunIgnoresHudPort proves the dry-run path never
// spawns/ensures a HUD (brief Step 6). It swaps the package-level ensureHUD
// seam for a recorder and asserts it is NOT called on a dry-run — so if the
// `!dryRun` guard at run.go regressed, this test FAILS instead of silently
// launching a real detached `ringer hud` during the test run (the fork-bomb
// hazard Plan 4 fought). A bogus hudPortOverride and a nonexistent config
// path further prove the port-resolution logic is short-circuited too.
func TestRunManifestFileDryRunIgnoresHudPort(t *testing.T) {
	dir := t.TempDir()
	data, err := buildDemoManifest(t.TempDir())
	if err != nil {
		t.Fatalf("buildDemoManifest: %v", err)
	}
	manifestPath := filepath.Join(dir, "ringer.json")
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	prevConfig := opts.Config
	opts.Config = filepath.Join(dir, "nonexistent-config.toml")
	t.Cleanup(func() { opts.Config = prevConfig })

	orig := ensureHUD
	defer func() { ensureHUD = orig }()
	called := false
	ensureHUD = func(stateDir string, port int, lg logging.Logger, openBrowser bool) { called = true }

	if err := runManifestFile(context.Background(), manifestPath, 0, "test", true, false, 65535); err != nil {
		t.Fatalf("runManifestFile dry-run: %v", err)
	}
	if called {
		t.Fatal("dry-run must not spawn/ensure a HUD")
	}
}
