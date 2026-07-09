// Package logging provides ringer's minimal, always-on logging interface.
//
// The logger is never unconfigured: Default returns a working Info-level
// logger to stderr, usable from the very first line of startup, before any
// config file or CLI flag has been parsed. Loading a Config via New only
// refines the level and format of that same logger — it never "enables"
// logging. This makes "logging before logging is configured" a non-problem.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
)

type Logger interface {
	Debug(msg string, args ...any)
	Debugf(format string, args ...any)
	Info(msg string, args ...any)
	Infof(format string, args ...any)
	Warn(msg string, args ...any)
	Warnf(format string, args ...any)
	Error(msg string, args ...any)
	Errorf(format string, args ...any)
	WithLevel(level slog.Level) Logger
}

// Config controls a Logger built by New. The zero value is valid and sane:
// Level's zero value is slog.LevelInfo; an empty Format is treated as "text".
type Config struct {
	Level  slog.Level
	Format string // "text" (default) or "json"
}

type slogLogger struct {
	log    *slog.Logger
	out    io.Writer
	format string
}

// Default returns a working Info-level logger to stderr. Always safe to call,
// requires no configuration — log through this before New has been called.
func Default() Logger { return newSlogLogger(os.Stderr, slog.LevelInfo, "text") }

// New builds a Logger from cfg. It is the only constructor that can fail
// (unknown Format) and it never exits the process — the CLI boundary decides
// how to surface the error.
func New(cfg Config) (Logger, error) {
	format := cfg.Format
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" {
		return nil, fmt.Errorf("logging: unknown format %q (want \"text\" or \"json\")", format)
	}
	return newSlogLogger(os.Stderr, cfg.Level, format), nil
}

func newSlogLogger(out io.Writer, level slog.Level, format string) *slogLogger {
	return &slogLogger{log: slog.New(newHandler(out, level, format)), out: out, format: format}
}

func newHandler(out io.Writer, level slog.Level, format string) slog.Handler {
	opts := &slog.HandlerOptions{Level: level}
	if format == "json" {
		return slog.NewJSONHandler(out, opts)
	}
	return slog.NewTextHandler(out, opts)
}

func (l *slogLogger) Debug(msg string, args ...any)     { l.log.Debug(msg, args...) }
func (l *slogLogger) Debugf(format string, args ...any) { l.log.Debug(fmt.Sprintf(format, args...)) }
func (l *slogLogger) Info(msg string, args ...any)      { l.log.Info(msg, args...) }
func (l *slogLogger) Infof(format string, args ...any)  { l.log.Info(fmt.Sprintf(format, args...)) }
func (l *slogLogger) Warn(msg string, args ...any)      { l.log.Warn(msg, args...) }
func (l *slogLogger) Warnf(format string, args ...any)  { l.log.Warn(fmt.Sprintf(format, args...)) }
func (l *slogLogger) Error(msg string, args ...any)     { l.log.Error(msg, args...) }
func (l *slogLogger) Errorf(format string, args ...any) { l.log.Error(fmt.Sprintf(format, args...)) }

// WithLevel returns a sibling logger (same destination + format) at level.
func (l *slogLogger) WithLevel(level slog.Level) Logger {
	return newSlogLogger(l.out, level, l.format)
}
