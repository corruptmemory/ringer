package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/engine"
	"github.com/corruptmemory/ringer/internal/lint"
	"github.com/corruptmemory/ringer/internal/logging"
	"github.com/corruptmemory/ringer/internal/manifest"
	"github.com/corruptmemory/ringer/internal/runner"
	"github.com/corruptmemory/ringer/internal/store"
)

type runCmd struct {
	MaxParallel int    `long:"max-parallel" description:"override manifest max_parallel"`
	Identity    string `long:"identity" description:"identity for eval rows (default: resolved from config/env/hostname)"`
	DryRun      bool   `long:"dry-run" description:"print the plan and exit"`
	NoDashboard bool   `long:"no-dashboard" description:"accepted; always headless in Plan 2 (no HUD yet)"`
	Args        struct {
		Manifest string `positional-arg-name:"MANIFEST" description:"path to the manifest JSON"`
	} `positional-args:"yes" required:"yes"`
}

func (c *runCmd) Execute(args []string) error {
	cfgPath := opts.Config
	if cfgPath == "" {
		cfgPath = config.DefaultPath()
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return err
	}

	// Build the logger — fail loud here, at the CLI boundary, not in a library init().
	lvl, err := resolveLogLevel(opts.LogLevel, cfg)
	if err != nil {
		return fmt.Errorf("--log-level: %w", err)
	}
	lg, err := logging.New(logging.Config{Level: lvl, Format: cfg.Logging.Format})
	if err != nil {
		return err
	}

	manifestPath := c.Args.Manifest
	m, err := manifest.FromPath(manifestPath)
	if err != nil {
		return err
	}
	if c.MaxParallel > 0 {
		m.MaxParallel = c.MaxParallel
	}

	// Lint findings are advisory only — print and keep going, never block a run.
	printLintFindings(lint.Check(m))

	identity := config.ResolveIdentity(c.Identity, cfg, filepath.Dir(manifestPath))

	// Engines: config engines plus a built-in mock pointing at this binary.
	self, _ := os.Executable()
	engines := map[string]config.EngineConfig{}
	for k, v := range cfg.Engines {
		engines[k] = v
	}
	if _, ok := engines["mock"]; !ok {
		engines["mock"] = config.EngineConfig{
			Bin: self, ArgsTemplate: []string{"mock-worker", "{spec}"}, Isolation: "none",
		}
	}

	used := map[string]bool{}
	for _, t := range m.Tasks {
		name := t.Engine
		if name == "" {
			name = "codex"
		}
		used[name] = true
	}
	if err := engine.Preflight(engines, used); err != nil {
		return err
	}

	if c.DryRun {
		fmt.Fprintf(os.Stdout, "run %q: %d task(s), max_parallel=%d, identity=%s\n",
			m.RunName, len(m.Tasks), m.MaxParallel, identity)
		for _, t := range m.Tasks {
			fmt.Fprintf(os.Stdout, "  - %s [%s]\n", t.Key, t.Engine)
		}
		return nil
	}

	var st *store.Store
	if s, err := store.Open(cfg.DBPath()); err != nil {
		lg.Warnf("eval store unavailable (%v); continuing without eval logging", err)
	} else {
		st = s
		defer st.Close()
	}

	res, err := runner.Run(context.Background(), runner.Options{
		Manifest: m, Engines: engines, StateDir: cfg.StateDirPath(),
		Identity: identity, Store: st, Stdout: os.Stdout, Logger: lg,
		MaxParallel: m.MaxParallel,
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "\n%-20s %-8s %-9s %s\n", "TASK", "VERDICT", "ATTEMPTS", "TOKENS")
	for _, r := range res.Results {
		fmt.Fprintf(os.Stdout, "%-20s %-8s %-9d %d\n", r.Key, r.Verdict, r.Attempts, r.Tokens)
	}
	if !res.AllPassed {
		return fmt.Errorf("run %s: one or more tasks failed", res.RunID)
	}
	return nil
}

func init() {
	parser.AddCommand("run", "Run a manifest", "Execute a manifest of tasks against pluggable engines.", &runCmd{})
}
