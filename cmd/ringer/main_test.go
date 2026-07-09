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
