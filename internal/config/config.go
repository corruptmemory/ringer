package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// LoggingConfig mirrors logging.Config field-for-field (Level slog.Level,
// Format string) instead of importing internal/logging, keeping config
// dependency-light; the CLI boundary does the trivial conversion. slog.Level
// implements encoding.TextUnmarshaler, so `level = "debug"` decodes for free.
// Zero value == {Info, ""} == the sane default (absent section => Info/text).
type LoggingConfig struct {
	Level  slog.Level `toml:"level" doc:"Minimum log level: \"debug\", \"info\", \"warn\", or \"error\". Default info."`
	Format string     `toml:"format" doc:"Log output format: \"text\" or \"json\". Default text."`
}

type EngineConfig struct {
	Bin            string   `toml:"bin" doc:"Path or PATH-resolved name of the engine binary."`
	ArgsTemplate   []string `toml:"args_template" doc:"Argv template. Placeholders: {taskdir}, {spec}, {access_args}, {engine_args}, {model}."`
	SandboxArgs    []string `toml:"sandbox_args" doc:"Args substituted for {access_args} on a sandboxed (non full-access) task."`
	FullAccessArgs []string `toml:"full_access_args" doc:"Args substituted for {access_args} when the task requests full_access and allow_full_access is set."`
	TokenRegex     string   `toml:"token_regex" doc:"Regex whose first capture group is the integer token count parsed from engine output."`
	ModelDefault   string   `toml:"model_default" doc:"Model id used for {model} when the task manifest doesn't set one."`
	Isolation      string   `toml:"isolation" doc:"Linux rootless isolation: \"none\" (default) or \"jail\" (per-task user-namespace chroot)."`
	JailStateDirs  []string `toml:"jail_state_dirs" doc:"Paths bound read-write inside the jail (engine state, e.g. ~/.config/opencode)."`
	JailRoBinds    []string `toml:"jail_ro_binds" doc:"Paths bound read-only inside the jail (engine installs outside the host toolchain, e.g. ~/.opencode)."`
}

type ArtifactConfig struct {
	Enabled   *bool  `toml:"enabled" doc:"Render zero-LLM HTML artifacts from state. Absent -> true (Python parity); false runs headless."`
	Out       string `toml:"out" doc:"Live status page path, re-rendered on every state flush. Supports {run_id}/{run_name}. Default ~/.ringer/artifacts/{run_id}.html."`
	ReportOut string `toml:"report_out" doc:"Final report path, rendered once when a run finishes. Default ~/.ringer/artifacts/{run_id}-report.html."`
	IndexOut  string `toml:"index_out" doc:"Multi-run index path listing every run under state_dir. Default ~/.ringer/artifacts/index.html."`
}

type EvalConfig struct {
	DBPath string `toml:"db_path" doc:"SQLite eval database path. Empty -> <state_dir>/ringer.db."`
}

type ScoreboardConfig struct {
	ModelIdentityPath string `toml:"model_identity_path" doc:"Override for the model identity registry TOML. Empty -> embedded registry/model-identity.toml."`
	ModelNotesPath    string `toml:"model_notes_path" doc:"Override for the model judgment notes file. Empty -> embedded docs/MODEL-NOTES.md."`
}

// CatalogConfig overrides where the OpenRouter model catalog is fetched
// from. Source empty -> catalog.DefaultSource (see AppConfig.CatalogSource).
type CatalogConfig struct {
	Source string `toml:"source" doc:"OpenRouter model catalog URL. Empty -> the default OpenRouter models endpoint."`
}

// HudConfig configures the Ringside HUD. Port is the fixed port the HUD binds
// (127.0.0.1 only, fails if taken); run/demo probe + auto-spawn on it.
type HudConfig struct {
	Port int `toml:"port" doc:"HUD port (127.0.0.1). run/demo auto-spawn + probe here. Default 8700 (matches hud.DefaultPort)."`
}

type AppConfig struct {
	IdentityDefault string                  `toml:"identity_default" doc:"Default identity stamped into state JSON and eval rows, overridden by --identity / FLEET_IDENTITY / RINGER_IDENTITY / .fleet-agent."`
	StateDir        string                  `toml:"state_dir" doc:"Directory holding run state and the eval database. Empty -> ~/.ringer."`
	AllowFullAccess bool                    `toml:"allow_full_access" doc:"Belt-and-suspenders gate: a task with full_access=true still fails unless this is true."`
	Logging         LoggingConfig           `toml:"logging" doc:"Log level and output format."`
	Eval            EvalConfig              `toml:"eval" doc:"Eval database location."`
	Artifact        ArtifactConfig          `toml:"artifact" doc:"Zero-LLM HTML artifact rendering (status page, report, index)."`
	Scoreboard      ScoreboardConfig        `toml:"scoreboard" doc:"Overrides for the model scoreboard's identity/notes files."`
	Catalog         CatalogConfig           `toml:"catalog" doc:"Override for the OpenRouter model catalog source."`
	Hud             HudConfig               `toml:"hud" doc:"Ringside HUD dashboard settings."`
	Engines         map[string]EngineConfig `toml:"engines" doc:"Per-engine spawn config. One [engines.<name>] table per engine."`
}

func DefaultPath() string {
	if p := os.Getenv("RINGER_CONFIG"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "config.toml"
	}
	return filepath.Join(home, ".config", "ringer", "config.toml")
}

// ExpandUser expands a leading "~" or "~/" to the current user's home
// directory, mirroring Python's Path.expanduser for the paths ringer's
// config and manifests carry. Non-tilde paths pass through unchanged.
func ExpandUser(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(strings.TrimPrefix(p, "~"), "/"))
		}
	}
	return p
}

func (c *AppConfig) StateDirPath() string {
	if c.StateDir != "" {
		return ExpandUser(c.StateDir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".ringer"
	}
	return filepath.Join(home, ".ringer")
}

// ArtifactEnabled resolves the artifact.enabled default: absent -> true
// (Python parity), explicit `enabled = false` -> false.
func (c *AppConfig) ArtifactEnabled() bool {
	return c.Artifact.Enabled == nil || *c.Artifact.Enabled
}

func (c *AppConfig) DBPath() string {
	if c.Eval.DBPath != "" {
		return ExpandUser(c.Eval.DBPath)
	}
	return filepath.Join(c.StateDirPath(), "ringer.db")
}

// ModelIdentityPath returns the expanded override path for the identity
// registry, or "" to signal "use the embedded default".
func (c *AppConfig) ModelIdentityPath() string {
	if c.Scoreboard.ModelIdentityPath == "" {
		return ""
	}
	return ExpandUser(c.Scoreboard.ModelIdentityPath)
}

// ModelNotesPath is the override for MODEL-NOTES, or "" for the embedded default.
func (c *AppConfig) ModelNotesPath() string {
	if c.Scoreboard.ModelNotesPath == "" {
		return ""
	}
	return ExpandUser(c.Scoreboard.ModelNotesPath)
}

// CatalogSource returns the configured catalog fetch source override, or ""
// to signal "use the caller's default" (catalog.DefaultSource).
func (c *AppConfig) CatalogSource() string {
	return c.Catalog.Source
}

// HudPort resolves the HUD port: the configured [hud] port, or 8700.
// (config is a leaf; the literal mirrors internal/hud.DefaultPort.)
func (c *AppConfig) HudPort() int {
	if c.Hud.Port > 0 {
		return c.Hud.Port
	}
	return 8700
}

func Load(path string) (*AppConfig, error) {
	var c AppConfig
	md, err := toml.DecodeFile(path, &c)
	if err != nil {
		if os.IsNotExist(err) {
			return &AppConfig{}, nil // sane defaults without a config file
		}
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	for _, k := range md.Undecoded() {
		key := k.String()
		for removed, hint := range removedKeys {
			if key == removed || strings.HasPrefix(key, removed+".") {
				return nil, fmt.Errorf("config %s: key %q was removed in the Go rewrite — %s", path, key, hint)
			}
		}
		return nil, fmt.Errorf("config %s: unknown key %q (typo? removed?)", path, key)
	}
	for name, e := range c.Engines {
		switch e.Isolation {
		case "", "none", "jail":
		default:
			return nil, fmt.Errorf("config %s: engines.%s.isolation must be \"none\" or \"jail\", got %q", path, name, e.Isolation)
		}
	}
	switch c.Logging.Format {
	case "", "text", "json":
	default:
		return nil, fmt.Errorf("config %s: logging.format must be \"text\" or \"json\", got %q", path, c.Logging.Format)
	}
	return &c, nil
}
