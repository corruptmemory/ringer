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
		if _, err := exec.LookPath(e.Bin); err != nil {
			errs = append(errs, fmt.Errorf("engine %q: binary %q not found on PATH", name, e.Bin))
		}
	}
	return errors.Join(errs...)
}

// ParseTokens pulls a token count out of output using tokenRegex: it takes
// the LAST match (an engine's running output often reports a token total
// more than once, so the final report wins over an earlier, smaller one),
// and within that match prefers the last capture group, falling back to the
// whole match when the regex has no groups. TrimSpace guards against
// regexes whose capture group includes surrounding whitespace. Returns -1
// (the "unknown" sentinel) when: tokenRegex is empty, tokenRegex fails to
// compile, there's no match, or the matched text isn't a valid base-10
// integer.
func ParseTokens(tokenRegex, output string) int64 {
	if tokenRegex == "" {
		return -1
	}
	re, err := regexp.Compile(tokenRegex)
	if err != nil {
		return -1
	}
	matches := re.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return -1
	}
	last := matches[len(matches)-1]
	grp := last[len(last)-1]
	// Strip grouping commas ("75,417") before parsing — the token_regex
	// capture classes intentionally allow [0-9,] (e.g. codex prints
	// "tokens used\n75,417"), but ParseInt rejects the comma. Without this
	// the count silently degrades to the -1 sentinel ("???" in the HUD).
	n, err := strconv.ParseInt(strings.ReplaceAll(strings.TrimSpace(grp), ",", ""), 10, 64)
	if err != nil {
		return -1
	}
	return n
}
