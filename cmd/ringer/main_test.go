package main

import (
	"log/slog"
	"testing"

	"github.com/corruptmemory/ringer/internal/config"
)

func TestResolveLogLevel(t *testing.T) {
	cases := []struct {
		name      string
		flagValue string
		cfg       *config.AppConfig
		want      slog.Level
		wantErr   bool
	}{
		{
			"flag takes precedence",
			"debug",
			&config.AppConfig{Logging: config.LoggingConfig{Level: slog.LevelWarn}},
			slog.LevelDebug,
			false,
		},
		{
			"config used if no flag",
			"",
			&config.AppConfig{Logging: config.LoggingConfig{Level: slog.LevelError}},
			slog.LevelError,
			false,
		},
		{
			"default to info with nil config",
			"",
			nil,
			slog.LevelInfo,
			false,
		},
		{
			"default to info with empty config",
			"",
			&config.AppConfig{},
			slog.LevelInfo,
			false,
		},
		{
			"invalid flag level",
			"invalid",
			nil,
			0,
			true,
		},
		{
			"all slog levels",
			"warn",
			nil,
			slog.LevelWarn,
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveLogLevel(tc.flagValue, tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("resolveLogLevel: %v", err)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFormatTokens is the RED/GREEN anchor for the final-review finding that
// the verdict table's TOKENS column printed runner.TaskResult's -1 "unknown"
// sentinel as a literal negative number. -1 (and any other negative value,
// defensively) must render blank ("-"); a real token count must render as
// its plain decimal digits, matching Python's behavior of leaving the column
// empty when tokens are unknown.
func TestFormatTokens(t *testing.T) {
	cases := []struct {
		name   string
		tokens int64
		want   string
	}{
		{"unknown sentinel renders blank", -1, "-"},
		{"any negative value renders blank", -42, "-"},
		{"zero tokens renders as 0", 0, "0"},
		{"a real token count renders as digits", 1234, "1234"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatTokens(tc.tokens); got != tc.want {
				t.Errorf("formatTokens(%d) = %q, want %q", tc.tokens, got, tc.want)
			}
		})
	}
}
