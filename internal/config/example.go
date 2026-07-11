// internal/config/example.go
package config

import "log/slog"

// ExampleConfig returns a curated, fully-populated AppConfig mirroring the
// values in the repo's config.sample.toml. It is the source of truth for
// `ringer gen-config`'s example values (RenderDocumented reflects the struct
// tags for the schema + comments, but the Engines map has no static fields to
// reflect, so its example entries must be curated here by hand).
func ExampleConfig() AppConfig {
	return AppConfig{
		IdentityDefault: "codex-mini",
		StateDir:        "~/.ringer",
		AllowFullAccess: false,
		Logging: LoggingConfig{
			Level:  slog.LevelInfo,
			Format: "text",
		},
		Eval: EvalConfig{
			DBPath: "",
		},
		Artifact: ArtifactConfig{
			Enabled:   ptr(true),
			Out:       "~/.ringer/artifacts/{run_id}.html",
			ReportOut: "~/.ringer/artifacts/{run_id}-report.html",
			IndexOut:  "~/.ringer/artifacts/index.html",
		},
		Scoreboard: ScoreboardConfig{
			ModelIdentityPath: "",
			ModelNotesPath:    "",
		},
		Catalog: CatalogConfig{
			Source: "",
		},
		Hud: HudConfig{
			Port: 8700,
		},
		Engines: map[string]EngineConfig{
			"codex": {
				Bin: "codex",
				ArgsTemplate: []string{
					"exec",
					"--skip-git-repo-check",
					"{access_args}",
					"{engine_args}",
					"-C",
					"{taskdir}",
					"{spec}",
				},
				SandboxArgs:    []string{"--sandbox", "workspace-write"},
				FullAccessArgs: []string{"--dangerously-bypass-approvals-and-sandbox"},
				TokenRegex:     `tokens\s+used\s*:?\s*([0-9][0-9,]*)`,
			},
			"opencode": {
				Bin: "/home/you/.opencode/bin/opencode",
				ArgsTemplate: []string{
					"{taskdir}",
					"{access_args}",
					"run",
					"-m",
					"{model}",
					"--dangerously-skip-permissions",
					"--format",
					"json",
					"{engine_args}",
					"--dir",
					"{taskdir}",
					"{spec}",
				},
				SandboxArgs:    []string{},
				FullAccessArgs: []string{"--no-sandbox"},
				TokenRegex:     `"tokens":\{"total":([0-9]+)`,
				ModelDefault:   "openrouter/z-ai/glm-5.2",
				Isolation:      "jail",
				JailStateDirs:  []string{"~/.config/opencode", "~/.local/share/opencode"},
				JailRoBinds:    []string{"~/.opencode"},
			},
		},
	}
}

func ptr[T any](v T) *T { return &v }
