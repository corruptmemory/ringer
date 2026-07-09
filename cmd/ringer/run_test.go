package main

import (
	"testing"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/isolate"
	"github.com/corruptmemory/ringer/internal/jail"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/manifest"
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
