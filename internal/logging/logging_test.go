package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
)

func TestLevelFiltering(t *testing.T) {
	logger, capt := NewCapture() // starts at Info per NewCapture's contract
	logger.Debug("debug line")
	if got := capt.String(); strings.Contains(got, "debug line") {
		t.Fatalf("Debug at Info level should be suppressed, got: %q", got)
	}
	logger.Info("info line")
	logger.Warn("warn line")
	logger.Error("error line")
	got := capt.String()
	for _, want := range []string{"info line", "warn line", "error line"} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q, got: %q", want, got)
		}
	}
}

func TestPrintfMethods(t *testing.T) {
	logger, capt := NewCapture()
	logger.Debugf("debug %d %s", 1, "x") // suppressed at Info
	logger.Infof("info %d %s", 2, "y")
	got := capt.String()
	if strings.Contains(got, "debug 1 x") {
		t.Errorf("Debugf should be suppressed at Info level, got: %q", got)
	}
	if !strings.Contains(got, "info 2 y") {
		t.Errorf("Infof missing formatted line, got: %q", got)
	}
}

func TestCaptureIsSynchronousNotLingering(t *testing.T) {
	logger, capt := NewCapture()
	logger.Info("line one")
	// No sleep: a synchronous, mutex-protected drain must have the line
	// available the instant the logging call returns.
	if got := capt.String(); !strings.Contains(got, "line one") {
		t.Fatalf("expected synchronous capture of %q, got: %q", "line one", got)
	}
}

func TestWithLevel(t *testing.T) {
	logger, capt := NewCapture() // Info
	logger.Debug("hidden")
	if strings.Contains(capt.String(), "hidden") {
		t.Fatalf("Debug should be hidden at Info level")
	}
	debugLogger := logger.WithLevel(slog.LevelDebug)
	debugLogger.Debug("now visible")
	if !strings.Contains(capt.String(), "now visible") {
		t.Fatalf("WithLevel(Debug) should emit Debug lines, got: %q", capt.String())
	}
}

func TestDefaultIsAlwaysAvailable(t *testing.T) {
	logger := Default() // no configuration step
	if logger == nil {
		t.Fatal("Default() returned nil")
	}
	logger.Info("process starting") // must not panic
}

func TestNewBuildsFromConfig(t *testing.T) {
	cases := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"zero value defaults to text/info", Config{}, false},
		{"explicit debug+json", Config{Level: slog.LevelDebug, Format: "json"}, false},
		{"unknown format rejected", Config{Format: "xml"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l, err := New(tc.cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error, got nil")
				}
				return
			}
			if err != nil || l == nil {
				t.Fatalf("New: l=%v err=%v", l, err)
			}
		})
	}
}

func TestConfigLevelParsesFromTOML(t *testing.T) {
	type fixture struct {
		Logging Config `toml:"logging"`
	}
	p := filepath.Join(t.TempDir(), "c.toml")
	if err := os.WriteFile(p, []byte("[logging]\nlevel = \"debug\"\nformat = \"json\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var f fixture
	if _, err := toml.DecodeFile(p, &f); err != nil {
		t.Fatalf("DecodeFile: %v", err)
	}
	if f.Logging.Level != slog.LevelDebug || f.Logging.Format != "json" {
		t.Errorf("parsed %+v", f.Logging)
	}
}
