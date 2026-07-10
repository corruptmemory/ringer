package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/corruptmemory/ringer/internal/manifest"
)

// demoCmd mirrors runCmd's flags (minus the manifest positional arg, which
// demo generates itself) so a user reaching for `run`'s options finds the
// same knobs on `demo`.
type demoCmd struct {
	MaxParallel int    `long:"max-parallel" description:"override demo manifest max_parallel"`
	Identity    string `long:"identity" description:"identity for eval rows (default: resolved from config/env/hostname)"`
	DryRun      bool   `long:"dry-run" description:"print the plan and exit"`
	NoDashboard bool   `long:"no-dashboard" description:"do not ensure the Ringside HUD is running / open a browser"`
}

func (c *demoCmd) Execute(args []string) error {
	manifestPath, err := writeDemoManifest()
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "demo manifest: %s\n", manifestPath)
	// Delegate to the exact same execution path `run` uses — demo differs
	// only in where the manifest came from, not in how it's run.
	ctx, stop := signalContext()
	defer stop()
	return runManifestFile(ctx, manifestPath, c.MaxParallel, c.Identity, c.DryRun, c.NoDashboard)
}

// buildDemoManifest returns the JSON bytes of a self-contained, 3-task mock
// manifest that proves the whole run path end-to-end at zero API cost. The
// task shapes mirror internal/runner/runner_test.go's proven mock manifests:
//
//   - alpha: a single MOCK_FILE write — passes on the first attempt.
//   - bravo: two MOCK_FILE blocks in one spec — proves multi-file tasks work.
//   - charlie: MOCK_FAIL_ONCE followed by a MOCK_FILE write — attempt 1 fails
//     deterministically (zero filesystem side effects) and attempt 2 finds
//     the marker the first attempt left behind and passes, exercising the
//     runner's fail-then-retry path exactly like runner_test.go's "retry" task.
//
// workdir is embedded as the manifest's workdir; the runner creates a
// "<workdir>/<task key>" directory per task.
func buildDemoManifest(workdir string) ([]byte, error) {
	m := manifest.Manifest{
		RunName:     "ringer-demo",
		Workdir:     workdir,
		MaxParallel: 3,
		Tasks: []manifest.Task{
			{
				Key:         "alpha",
				Engine:      "mock",
				Spec:        "Mock task: writes alpha.txt with fixed content; deterministic, no network, zero cost.\nMOCK_FILE: alpha.txt\nalpha ready\nMOCK_END\n",
				Check:       `test "$(cat alpha.txt)" = "alpha ready"`,
				ExpectFiles: []string{"alpha.txt"},
				Verified:    "alpha.txt exists and contains exactly the expected text",
			},
			{
				Key:    "bravo",
				Engine: "mock",
				Spec: "Mock task: writes two files (bravo.txt and bravo2.txt) in a single run; proves multi-file task execution works correctly.\n" +
					"MOCK_FILE: bravo.txt\nbravo ready\nMOCK_END\n" +
					"MOCK_FILE: bravo2.txt\nbravo two ready\nMOCK_END\n",
				Check:       `test "$(cat bravo.txt)" = "bravo ready" && test "$(cat bravo2.txt)" = "bravo two ready"`,
				ExpectFiles: []string{"bravo.txt", "bravo2.txt"},
				Verified:    "bravo.txt and bravo2.txt both exist with the expected content",
			},
			{
				Key:         "charlie",
				Engine:      "mock",
				Spec:        "Mock task: fails on first attempt (deterministically, with no side effects), then succeeds on retry; proves failure recovery works.\nMOCK_FAIL_ONCE\nMOCK_FILE: charlie.txt\ncharlie ready\nMOCK_END\n",
				Check:       `test "$(cat charlie.txt)" = "charlie ready"`,
				ExpectFiles: []string{"charlie.txt"},
				Verified:    "charlie.txt exists after a simulated first-attempt failure and retry",
			},
		},
	}
	return json.MarshalIndent(&m, "", "  ")
}

// writeDemoManifest builds the demo manifest and writes it to a fresh temp
// directory, mirroring ringer.py's create_demo_manifest: a "ringer-demo-*"
// temp root holding ringer.json plus a "work" subdirectory the tasks write
// into. Returns the manifest path.
func writeDemoManifest() (string, error) {
	root, err := os.MkdirTemp("", "ringer-demo-")
	if err != nil {
		return "", err
	}
	workdir := filepath.Join(root, "work")
	data, err := buildDemoManifest(workdir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, "ringer.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func init() {
	parser.AddCommand("demo", "Run a zero-cost mock demo",
		"Generate a 3-task mock manifest (pass, multi-file, fail-then-retry-pass) and run it through the same path as `run`, at zero API cost.",
		&demoCmd{})
}
