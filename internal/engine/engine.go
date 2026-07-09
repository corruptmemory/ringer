package engine

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"

	"github.com/corruptmemory/ringer/internal/config"
)

func BuiltinCodex() config.EngineConfig {
	return config.EngineConfig{
		Bin:          "codex",
		ArgsTemplate: []string{"exec", "--skip-git-repo-check", "{access_args}", "{engine_args}", "-C", "{taskdir}", "{spec}"},
	}
}

func Resolve(engines map[string]config.EngineConfig, name string) (config.EngineConfig, error) {
	if name == "" {
		name = "codex"
	}
	if e, ok := engines[name]; ok {
		return e, nil
	}
	if name == "codex" {
		return BuiltinCodex(), nil
	}
	return config.EngineConfig{}, fmt.Errorf("unknown engine %q (not in config, and only \"codex\" is built in)", name)
}

// scalar placeholders are replaced within a token; list placeholders replace the
// whole token with zero or more tokens.
func BuildArgv(e config.EngineConfig, taskDir, spec, model string, engineArgs []string, fullAccess bool) (string, []string) {
	access := e.SandboxArgs
	if fullAccess {
		access = e.FullAccessArgs
	}
	lists := map[string][]string{
		"{engine_args}":      engineArgs,
		"{access_args}":      access,
		"{sandbox_args}":     e.SandboxArgs,
		"{full_access_args}": e.FullAccessArgs,
	}
	scalars := strings.NewReplacer("{taskdir}", taskDir, "{spec}", spec, "{model}", model)
	var argv []string
	for _, tok := range e.ArgsTemplate {
		if repl, isList := lists[tok]; isList {
			argv = append(argv, repl...)
			continue
		}
		argv = append(argv, scalars.Replace(tok))
	}
	return e.Bin, argv
}

func Preflight(engines map[string]config.EngineConfig, used map[string]bool) error {
	var errs []error
	for name := range used {
		e, err := Resolve(engines, name)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if e.Isolation == "jail" {
			errs = append(errs, fmt.Errorf("engine %q uses isolation=\"jail\", which lands in Plan 3; use \"none\" for now", name))
			continue
		}
		if _, err := exec.LookPath(e.Bin); err != nil {
			errs = append(errs, fmt.Errorf("engine %q: binary %q not found on PATH", name, e.Bin))
		}
	}
	return errors.Join(errs...)
}

func ParseTokens(tokenRegex, output string) int64 {
	if tokenRegex == "" {
		return -1
	}
	re, err := regexp.Compile(tokenRegex)
	if err != nil {
		return -1
	}
	m := re.FindStringSubmatch(output)
	if len(m) < 2 {
		return -1
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil {
		return -1
	}
	return n
}
