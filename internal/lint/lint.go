// Package lint ports Ringer's manifest lint heuristics from the Python
// original's lint_manifest (ringer.py). These are non-blocking "checks that
// can't be trusted" warnings surfaced by `ringer lint` and printed (never
// blocking) after manifest load in `ringer run`.
package lint

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/corruptmemory/ringer/internal/config"
	"github.com/corruptmemory/ringer/internal/manifest"
)

// Finding is a single lint result against a manifest or one of its tasks.
// TaskKey is "" for manifest-level findings.
type Finding struct {
	TaskKey string
	Rule    string
	Message string
}

// Rule identifiers. Stable strings so callers/tests can match on them
// without depending on Message wording.
const (
	RuleCheckCannotFail     = "check-cannot-fail"
	RuleCheckSilent         = "check-silent"
	RuleSpecUnderspecified  = "spec-underspecified"
	RuleSpecFilePointer     = "spec-file-pointer"
	RuleMissingExpectFiles  = "missing-expect-files"
	RuleWriteCollision      = "write-collision"
	RuleWorktreeDeliverable = "worktree-deliverable"
	RuleWorktreeCommit      = "worktree-commit"
)

// Check returns findings for the manifest-level "checks that can't be
// trusted" heuristics: a check that cannot fail, a check that can fail
// without printing why, a spec that is probably underspecified, a spec that
// is just a pointer to another file, and a task with no declared
// deliverables.
func Check(m *manifest.Manifest) []Finding {
	var findings []Finding
	for _, t := range m.Tasks {
		if checkCannotFail(t.Check) {
			findings = append(findings, Finding{
				TaskKey: t.Key,
				Rule:    RuleCheckCannotFail,
				Message: "check cannot fail, so the task cannot be verified.",
			})
		}
		if checkMayFailSilently(t.Check) {
			findings = append(findings, Finding{
				TaskKey: t.Key,
				Rule:    RuleCheckSilent,
				Message: "check may fail without printing why; retry prompt and eval log depend on failure output.",
			})
		}
		if len(strings.TrimSpace(t.Spec)) < 80 {
			findings = append(findings, Finding{
				TaskKey: t.Key,
				Rule:    RuleSpecUnderspecified,
				Message: "spec is probably underspecified; workers are stateless and cannot ask questions.",
			})
		}
		if specIsFilePointer(t.Spec) {
			findings = append(findings, Finding{
				TaskKey: t.Key,
				Rule:    RuleSpecFilePointer,
				Message: "spec is a pointer to an instruction file; the retry prompt and any log viewer " +
					"lose context — put the instructions in the spec itself.",
			})
		}
		if len(t.ExpectFiles) == 0 && !m.Worktrees {
			findings = append(findings, Finding{
				TaskKey: t.Key,
				Rule:    RuleMissingExpectFiles,
				Message: "no expect_files; the verifier can't confirm what the task produced — " +
					"declare the expected deliverables.",
			})
		}
		if m.Worktrees && anyRelativeExpectFile(t.ExpectFiles) {
			findings = append(findings, Finding{
				TaskKey: t.Key,
				Rule:    RuleWorktreeDeliverable,
				Message: "deliverable would be deleted with the worktree; write it outside the worktree or export it in the check.",
			})
		}
		if m.Worktrees && instructsGitCommit(t.Spec) {
			findings = append(findings, Finding{
				TaskKey: t.Key,
				Rule:    RuleWorktreeCommit,
				Message: "worker commits die with the worktree; have the worker leave changes uncommitted and export the diff in the check.",
			})
		}
	}
	if !m.Worktrees {
		// Relative expect_files resolve inside each task's own directory and
		// cannot collide; only a shared absolute path is a real collision.
		pathsToTasks := map[string][]string{}
		for _, t := range m.Tasks {
			for _, p := range t.ExpectFiles {
				if filepath.IsAbs(config.ExpandUser(p)) {
					pathsToTasks[p] = append(pathsToTasks[p], t.Key)
				}
			}
		}
		paths := make([]string, 0, len(pathsToTasks))
		for p := range pathsToTasks {
			paths = append(paths, p)
		}
		sort.Strings(paths)
		for _, p := range paths {
			if keys := pathsToTasks[p]; len(keys) >= 2 {
				findings = append(findings, Finding{
					Rule:    RuleWriteCollision,
					Message: fmt.Sprintf("write collision on %s: listed by %s.", p, strings.Join(keys, ", ")),
				})
			}
		}
	}
	return findings
}

// --- W1: check cannot fail -------------------------------------------------

func checkCannotFail(check string) bool {
	stripped := strings.TrimSpace(stripShellComments(check))
	switch stripped {
	case "true", ":", "exit 0":
		return true
	}
	return consistsOnlyOfEchoCommands(stripped)
}

var (
	reSplitParts    = regexp.MustCompile(`(?:&&|;|\n)+`)
	rePipeRedirects = regexp.MustCompile(`[|<>]`)
)

func consistsOnlyOfEchoCommands(command string) bool {
	if command == "" || strings.Contains(command, "||") || rePipeRedirects.MatchString(command) {
		return false
	}
	parts := splitNonEmpty(reSplitParts.Split(command, -1))
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		tokens, err := shellSplit(part)
		if err != nil || len(tokens) == 0 || tokens[0] != "echo" {
			return false
		}
	}
	return true
}

// --- W2: check may fail silently -------------------------------------------

var reSemiNewlinePipe = regexp.MustCompile(`[;\n|]`)

func checkMayFailSilently(check string) bool {
	stripped := strings.TrimSpace(stripShellComments(check))
	if hasQuietDiffProbe(stripped) {
		return !hasFailureOutputBranch(stripped)
	}
	if stripped == "" || strings.Contains(stripped, "||") {
		return false
	}
	if reSemiNewlinePipe.MatchString(stripped) {
		return false
	}
	parts := splitNonEmpty(strings.Split(stripped, "&&"))
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if !isSilentProbe(part) {
			return false
		}
	}
	return true
}

func hasQuietDiffProbe(command string) bool {
	for _, part := range commandParts(command) {
		if hasCommandPrefix(part, "diff", "-q") {
			return true
		}
	}
	return false
}

func hasFailureOutputBranch(command string) bool {
	idx := strings.Index(command, "||")
	if idx == -1 {
		return false
	}
	branch := command[idx+2:]
	prefixes := []string{"echo", "printf", "cat", "diff", "ls"}
	for _, part := range commandParts(branch) {
		for _, prefix := range prefixes {
			if hasCommandPrefix(part, prefix) {
				return true
			}
		}
	}
	return false
}

var reCommandPartsSplit = regexp.MustCompile(`(?:&&|\|\||;|\n)+`)

// commandParts splits a shell snippet on &&, ||, ;, and newlines, trimming
// whitespace and a small set of grouping characters ({ } ( )) from each
// piece — enough to recognize commands inside `{ ...; }` or `( ... )`
// blocks without a full shell parser.
func commandParts(command string) []string {
	raw := reCommandPartsSplit.Split(command, -1)
	var parts []string
	for _, p := range raw {
		if strings.TrimSpace(p) == "" {
			continue
		}
		parts = append(parts, strings.Trim(p, " \t{}()"))
	}
	return parts
}

func hasCommandPrefix(command string, prefix ...string) bool {
	tokens, err := shellSplit(stripCommonRedirections(command))
	if err != nil || len(tokens) < len(prefix) {
		return false
	}
	for i, want := range prefix {
		if tokens[i] != want {
			return false
		}
	}
	return true
}

func isSilentProbe(command string) bool {
	return isFileExistenceTest(command) || isQuietGrep(command)
}

func isQuietGrep(command string) bool {
	tokens, err := shellSplit(stripCommonRedirections(strings.TrimSpace(command)))
	if err != nil || len(tokens) == 0 || tokens[0] != "grep" {
		return false
	}
	for _, tok := range tokens[1:] {
		if tok == "-q" {
			return true
		}
		if strings.HasPrefix(tok, "-") && strings.Contains(tok[1:], "q") {
			return true
		}
	}
	return false
}

var fileTestOps = map[string]bool{
	"-e": true, "-f": true, "-s": true, "-d": true,
	"-r": true, "-w": true, "-x": true, "-L": true,
}

func isFileExistenceTest(command string) bool {
	tokens, err := shellSplit(stripCommonRedirections(strings.TrimSpace(command)))
	if err != nil {
		return false
	}
	if len(tokens) >= 3 && tokens[0] == "test" && fileTestOps[tokens[1]] {
		return true
	}
	return len(tokens) >= 4 && tokens[0] == "[" && fileTestOps[tokens[1]] && tokens[len(tokens)-1] == "]"
}

var (
	reRedirFD   = regexp.MustCompile(`\s+\d?>&\d+\s*$`)
	reRedirFile = regexp.MustCompile(`\s+\d?>\S+\s*$`)
)

func stripCommonRedirections(command string) string {
	command = reRedirFD.ReplaceAllString(command, "")
	command = reRedirFile.ReplaceAllString(command, "")
	return strings.TrimSpace(command)
}

// --- W8: spec is a pointer to another file ----------------------------------

var (
	reDoWhatItSays    = regexp.MustCompile(`(?i)do (exactly )?what (it|the file|that file) says`)
	reFilePointerSpec = regexp.MustCompile(`(?i)\b(read|open|follow|see)\b[^\n.]{0,100}?/[\w~][\w./~-]*`)
)

// specIsFilePointer is true when the spec's substance lives in some other
// file. "Read /path/to/instructions.md and do what it says" hides the brief
// from anyone watching the run and starves the retry prompt. Long specs that
// merely reference files for CONTEXT are fine — the heuristic only fires
// when the spec is short enough that the pointer must be the whole plan.
func specIsFilePointer(spec string) bool {
	text := strings.TrimSpace(spec)
	if reDoWhatItSays.MatchString(text) {
		return true
	}
	if len(text) >= 600 {
		return false
	}
	return reFilePointerSpec.MatchString(text)
}

// --- shell tokenizing --------------------------------------------------

// splitNonEmpty trims each part and drops the empty ones. Mirrors the
// Python pattern `[p.strip() for p in re.split(...) if p.strip()]`.
func splitNonEmpty(parts []string) []string {
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func isShellSpace(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '\r', '\f', '\v':
		return true
	}
	return false
}

func isDoubleQuoteEscapable(c byte) bool {
	switch c {
	case '$', '`', '"', '\\', '\n':
		return true
	}
	return false
}

// shellSplit is a small POSIX-ish word splitter mirroring Python's
// shlex.split(s, posix=True): whitespace-separated tokens, single quotes are
// fully literal, double quotes allow backslash-escaping of $ ` " \ and
// newline, and a bare backslash outside quotes escapes the next character.
// Returns an error for an unterminated quote or a trailing bare backslash —
// the caller treats that the same as shlex.split raising ValueError (i.e.
// "not a recognizable shell command", so the heuristic just doesn't match).
func shellSplit(s string) ([]string, error) {
	var tokens []string
	var buf []byte
	inToken := false
	i, n := 0, len(s)
	for i < n {
		c := s[i]
		switch {
		case isShellSpace(c):
			if inToken {
				tokens = append(tokens, string(buf))
				buf = buf[:0]
				inToken = false
			}
			i++
		case c == '\'':
			inToken = true
			i++
			start := i
			for i < n && s[i] != '\'' {
				i++
			}
			if i >= n {
				return nil, errUnterminatedQuote
			}
			buf = append(buf, s[start:i]...)
			i++
		case c == '"':
			inToken = true
			i++
			for i < n && s[i] != '"' {
				if s[i] == '\\' && i+1 < n && isDoubleQuoteEscapable(s[i+1]) {
					buf = append(buf, s[i+1])
					i += 2
					continue
				}
				buf = append(buf, s[i])
				i++
			}
			if i >= n {
				return nil, errUnterminatedQuote
			}
			i++
		case c == '\\':
			inToken = true
			if i+1 >= n {
				return nil, errTrailingBackslash
			}
			if s[i+1] == '\n' {
				i += 2
				continue
			}
			buf = append(buf, s[i+1])
			i += 2
		default:
			inToken = true
			buf = append(buf, c)
			i++
		}
	}
	if inToken {
		tokens = append(tokens, string(buf))
	}
	return tokens, nil
}

type shellSplitError string

func (e shellSplitError) Error() string { return string(e) }

const (
	errUnterminatedQuote = shellSplitError("lint: unterminated quote")
	errTrailingBackslash = shellSplitError("lint: trailing backslash with no escaped character")
)

// anyRelativeExpectFile reports whether any declared deliverable is a
// relative path — in worktrees mode those live inside the checkout and are
// destroyed with it on PASS. "~"-prefixed paths count as absolute, matching
// Python's expanduser-then-is_absolute.
func anyRelativeExpectFile(paths []string) bool {
	for _, p := range paths {
		if strings.TrimSpace(p) == "" {
			continue
		}
		if !strings.HasPrefix(p, "~") && !filepath.IsAbs(p) {
			return true
		}
	}
	return false
}

// negatedGitCommitRe matches a "do not / don't / never / no [run]" phrase
// ENDING immediately before a "git commit" occurrence (ringer.py:785-792).
var negatedGitCommitRe = regexp.MustCompile(
	`(?:do\s+not|don't|never|no)[\s` + "`" + `'"()\[\]{}:;,.!?-]*(?:run[\s` + "`" + `'"()\[\]{}:;,.!?-]*)?$`)

// instructsGitCommit reports whether the spec tells the worker to run
// `git commit`, ignoring occurrences negated within the preceding 48
// characters (ringer.py:772-782).
func instructsGitCommit(spec string) bool {
	lower := strings.ToLower(spec)
	start := 0
	for {
		idx := strings.Index(lower[start:], "git commit")
		if idx == -1 {
			return false
		}
		idx += start
		from := idx - 48
		if from < 0 {
			from = 0
		}
		if !negatedGitCommitRe.MatchString(lower[from:idx]) {
			return true
		}
		start = idx + len("git commit")
	}
}

// --- shell comment stripping ------------------------------------------------

// stripShellComments removes '#'-to-end-of-line comments that are not
// inside a quoted string, so a literal '#' in e.g. test -s '#artifact'
// isn't mistaken for a comment marker.
func stripShellComments(command string) string {
	var out []byte
	inSingle, inDouble, escaped := false, false, false
	i, n := 0, len(command)
	for i < n {
		c := command[i]
		switch {
		case escaped:
			out = append(out, c)
			escaped = false
			i++
		case c == '\\' && !inSingle:
			out = append(out, c)
			escaped = true
			i++
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			out = append(out, c)
			i++
		case c == '"' && !inSingle:
			inDouble = !inDouble
			out = append(out, c)
			i++
		case c == '#' && !inSingle && !inDouble && (len(out) == 0 || isShellSpace(out[len(out)-1])):
			for i < n && command[i] != '\n' {
				i++
			}
		default:
			out = append(out, c)
			i++
		}
	}
	return string(out)
}
