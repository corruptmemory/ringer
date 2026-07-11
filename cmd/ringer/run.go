package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/engine"
	"github.com/corruptmemory/ringer/internal/hud/artifactwriter"
	"github.com/corruptmemory/ringer/internal/isolate"
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
	NoDashboard bool   `long:"no-dashboard" description:"do not ensure the Ringside HUD is running / open a browser"`
	Port        int    `long:"port" description:"HUD port for the auto-started Ringside (default: [hud] port or 8700)"`
	Args        struct {
		Manifest string `positional-arg-name:"MANIFEST" description:"path to the manifest JSON"`
	} `positional-args:"yes" required:"yes"`
}

func (c *runCmd) Execute(args []string) error {
	ctx, stop := signalContext()
	defer stop()
	return runManifestFile(ctx, c.Args.Manifest, c.MaxParallel, c.Identity, c.DryRun, c.NoDashboard, c.Port)
}

// signalContext returns a context canceled by the first SIGINT/SIGTERM.
// After that first signal the handler unregisters itself, so a second
// Ctrl-C falls back to default disposition and kills the process
// immediately — graceful teardown must never trap an impatient user.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go func() {
		<-ctx.Done()
		stop()
	}()
	return ctx, stop
}

// runManifestFile is the shared execution path behind both `run` and `demo`:
// load config/logger, load+validate the manifest at manifestPath, lint,
// resolve identity, inject the built-in mock engine, preflight, then either
// print the dry-run plan or actually run it and print the results table.
// `demo` reaches this exact function (not a reimplementation) by writing its
// generated manifest to a temp path and calling this with that path — this
// is the "same path run uses" the Task 11 brief calls for.
func runManifestFile(ctx context.Context, manifestPath string, maxParallelOverride int, identityFlag string, dryRun, noDashboard bool, hudPortOverride int) error {
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

	if !dryRun && !noDashboard {
		port := cfg.HudPort()
		if hudPortOverride > 0 {
			port = hudPortOverride
		}
		ensureHUD(cfg.StateDirPath(), port, lg, true)
	}

	m, err := manifest.FromPath(manifestPath)
	if err != nil {
		return err
	}
	if maxParallelOverride > 0 {
		m.MaxParallel = maxParallelOverride
	}

	// Lint findings are advisory only — print and keep going, never block a run.
	printLintFindings(lint.Check(m))

	identity := config.ResolveIdentity(identityFlag, cfg, filepath.Dir(manifestPath))

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

	if err := checkFullAccessGate(m, cfg.AllowFullAccess); err != nil {
		return err
	}

	if dryRun {
		// Dry-run prints the plan and exits; it must never fail on isolation
		// selection (a host without a backend should still be able to inspect
		// the plan), so selection happens only on the real run path below.
		fmt.Fprintf(os.Stdout, "run %q: %d task(s), max_parallel=%d, identity=%s\n",
			m.RunName, len(m.Tasks), m.MaxParallel, identity)
		for _, t := range m.Tasks {
			fmt.Fprintf(os.Stdout, "  - %s [%s]\n", t.Key, t.Engine)
		}
		return nil
	}

	// Isolation backend: selected once per run, only when some task will
	// actually jail (spec §6 preflight rule) — a full_access task takes
	// the unconfined lane and must not trigger selection (or a refusal)
	// on its own. Selection failures are refusals — precise, actionable,
	// before any task starts.
	iso, err := selectIsolator(m, engines, lg)
	if err != nil {
		return err
	}

	var st *store.Store
	if s, err := store.Open(cfg.DBPath()); err != nil {
		lg.Warnf("eval store unavailable (%v); continuing without eval logging", err)
	} else {
		st = s
		defer st.Close()
		// Best-effort background catalog refresh (spec §3): throttled to once
		// per 24h, never blocks or fails the run. Skipped entirely on
		// --dry-run (this whole branch is unreachable there — the dry-run
		// path returns above) and when the store itself is unavailable.
		maybeRefreshCatalog(st, catalogSourceOrDefault(cfg), lg, time.Now().UTC().Format(time.RFC3339))
	}

	var artWriter runner.ArtifactWriter
	if cfg.ArtifactEnabled() {
		artWriter = artifactwriter.New(cfg.StateDirPath(), resolveArtifactConfig(cfg), lg)
	}

	res, err := runner.Run(ctx, runner.Options{
		Manifest: m, Engines: engines, StateDir: cfg.StateDirPath(),
		Identity: identity, Store: st, Artifact: artWriter, Stdout: os.Stdout, Logger: lg,
		MaxParallel: m.MaxParallel, Isolator: iso,
	})
	if st != nil {
		// Run-end WAL checkpoint (spec §7, cznic #179): without an explicit
		// TRUNCATE checkpoint the WAL grows without bound under modernc.
		// Non-fatal — the data is durable either way — but never silent.
		if cerr := st.Checkpoint(); cerr != nil {
			lg.Warnf("eval store checkpoint: %v", cerr)
		}
	}
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stdout, "\n%-20s %-8s %-9s %s\n", "TASK", "VERDICT", "ATTEMPTS", "TOKENS")
	for _, r := range res.Results {
		fmt.Fprintf(os.Stdout, "%-20s %-8s %-9d %s\n", r.Key, r.Verdict, r.Attempts, formatTokens(r.Tokens))
	}
	if !res.AllPassed {
		return fmt.Errorf("run %s: one or more tasks failed", res.RunID)
	}
	return nil
}

// resolveArtifactConfig builds the artifactwriter.Config for cfg's artifact
// section: artifactwriter.DefaultConfig lays out the standard tree rooted at
// StateDirPath, and any of [artifact] out/report_out/index_out the user set
// overrides the corresponding path (empty -> keep the default), expanding a
// leading "~" the same way every other configured path in this codebase
// does.
func resolveArtifactConfig(cfg *config.AppConfig) artifactwriter.Config {
	ac := artifactwriter.DefaultConfig(cfg.StateDirPath())
	if cfg.Artifact.Out != "" {
		ac.OutTemplate = config.ExpandUser(cfg.Artifact.Out)
	}
	if cfg.Artifact.ReportOut != "" {
		ac.ReportTemplate = config.ExpandUser(cfg.Artifact.ReportOut)
	}
	if cfg.Artifact.IndexOut != "" {
		ac.IndexPath = config.ExpandUser(cfg.Artifact.IndexOut)
	}
	return ac
}

// selectIsolator returns the isolation backend for a run, or nil when no
// task needs one. A task with full_access takes the unconfined lane and
// never triggers selection. Selection failures (refusals) propagate.
func selectIsolator(m *manifest.Manifest, engines map[string]config.EngineConfig, lg logging.Logger) (isolate.Isolator, error) {
	needsJail := false
	for _, t := range m.Tasks {
		if t.FullAccess {
			continue
		}
		if e, err := engine.Resolve(engines, t.Engine); err == nil && e.Isolation == "jail" {
			needsJail = true
			break
		}
	}
	if !needsJail {
		return nil, nil
	}
	self, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve own binary for isolation trampoline: %w", err)
	}
	iso, err := isolate.Select(lg, m.Workdir, self)
	if err != nil {
		return nil, err
	}
	lg.Infof("isolation backend: %s", iso.Name())
	return iso, nil
}

// checkFullAccessGate refuses a run when any task requests full_access but the
// operator has not opted in via [config] allow_full_access. Fail-closed: the
// isolation layer already routes full_access tasks around the jail, so this
// makes the config doc's promised gate ("a task with full_access=true still
// fails unless this is true") real (spec §6). Applied before the dry-run
// branch so a forbidden manifest is refused whether you dry-run or run.
func checkFullAccessGate(m *manifest.Manifest, allowFullAccess bool) error {
	if allowFullAccess {
		return nil
	}
	for _, t := range m.Tasks {
		if t.FullAccess {
			return fmt.Errorf("task %q requests full_access but allow_full_access is not enabled in config; refusing (set allow_full_access = true to opt in)", t.Key)
		}
	}
	return nil
}

// formatTokens renders a TaskResult's token count for the verdict table.
// runner.TaskResult.Tokens uses -1 as its "unknown" sentinel (no token_regex
// configured, the regex didn't compile, or nothing matched) — printing that
// literal -1 would look like a real, if odd, token count rather than "we
// don't know." Blank it out instead, matching Python's behavior of leaving
// the column empty when tokens are unknown.
func formatTokens(tokens int64) string {
	if tokens < 0 {
		return "-"
	}
	return strconv.FormatInt(tokens, 10)
}

func init() {
	parser.AddCommand("run", "Run a manifest", "Execute a manifest of tasks against pluggable engines.", &runCmd{})
}
