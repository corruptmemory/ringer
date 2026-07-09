package lint

import (
	"testing"

	"github.com/corruptmemory/ringer/internal/manifest"
)

// Mirrors tests/test_lint.py's LONG_SPEC / GOOD_CHECK fixtures: a spec/check
// pair that trips none of the heuristics, so each table case only needs to
// override the field(s) under test.
const (
	longSpec = "Create the requested artifact in the current working directory, keep the change scoped, " +
		"and make the check command able to explain any failure clearly."
	goodCheck = "test -s output.txt && grep -q 'ready' output.txt || " +
		"{ echo 'FAIL: output.txt missing or does not contain ready'; exit 1; }"
)

// task builds a clean baseline manifest.Task, mirroring test_lint.py's
// LintManifestTests.task() helper. Callers mutate the returned value.
func task(key string) manifest.Task {
	return manifest.Task{
		Key:         key,
		Spec:        longSpec,
		Check:       goodCheck,
		ExpectFiles: []string{"output.txt"},
		Verified:    "the output file exists and contains the expected content",
	}
}

// oneTaskManifest wraps a single task in a minimal, otherwise-clean manifest.
func oneTaskManifest(t manifest.Task, worktrees bool) *manifest.Manifest {
	return &manifest.Manifest{
		RunName:     "lint-test",
		Workdir:     "/tmp/lint-test",
		MaxParallel: 1,
		Worktrees:   worktrees,
		Tasks:       []manifest.Task{t},
	}
}

func hasRule(findings []Finding, taskKey, rule string) bool {
	for _, f := range findings {
		if f.TaskKey == taskKey && f.Rule == rule {
			return true
		}
	}
	return false
}

func TestCheck(t *testing.T) {
	longContextualSpec := "You are a read-only reviewer. Study the code bundle at /tmp/bundle.txt as your " +
		"source material, then write ./review.md with sections VERDICT, BLOCKERS, and " +
		"EVIDENCE. For every blocker cite file and line from the bundle. Do not modify " +
		"any file other than ./review.md. The review must judge correctness, security, " +
		"and migration safety, and each claim needs a quoted line of code as evidence. " +
		"If a concern cannot be verified from the bundle alone, list it under an " +
		"UNCERTAIN heading instead of asserting it. Keep the verdict to one sentence. " +
		"Write plainly; the reader is a busy maintainer deciding whether to merge today."

	tests := []struct {
		name           string
		manifest       func() *manifest.Manifest
		wantRule       string // rule expected present on task "one"; "" means none expected
		wantAbsentRule string // rule expected ABSENT on task "one" (in addition to/instead of wantRule)
		wantEmpty      bool   // assert zero findings overall
	}{
		{
			name: "compliant manifest is clean",
			manifest: func() *manifest.Manifest {
				return oneTaskManifest(task("one"), false)
			},
			wantEmpty: true,
		},
		{
			// W1 case 1: pure echo chain can't fail.
			name: "check-cannot-fail: echo chain",
			manifest: func() *manifest.Manifest {
				tk := task("one")
				tk.Check = "echo ok && echo done"
				return oneTaskManifest(tk, false)
			},
			wantRule: RuleCheckCannotFail,
		},
		{
			// W1 case 2: bare true, with a trailing shell comment.
			name: "check-cannot-fail: true with comment",
			manifest: func() *manifest.Manifest {
				tk := task("one")
				tk.Check = "true # worker left the placeholder check"
				return oneTaskManifest(tk, false)
			},
			wantRule: RuleCheckCannotFail,
		},
		{
			// W1 negative: a '#' inside a quoted argument is not a comment,
			// and the check has a real failure branch (||), so it must NOT
			// be flagged as unverifiable.
			name: "check-cannot-fail: quoted hash is not a comment",
			manifest: func() *manifest.Manifest {
				tk := task("one")
				tk.Check = "test -s '#artifact' || { echo 'FAIL: #artifact missing'; exit 1; }"
				return oneTaskManifest(tk, false)
			},
			wantAbsentRule: RuleCheckCannotFail,
		},
		{
			// W2 case 1: chained file-existence-only probes, no failure output.
			name: "check-silent: file existence chain",
			manifest: func() *manifest.Manifest {
				tk := task("one")
				tk.Check = "test -f output.txt && [ -s report.md ]"
				return oneTaskManifest(tk, false)
			},
			wantRule: RuleCheckSilent,
		},
		{
			// W2 case 2: quiet diff probe with no failure branch.
			name: "check-silent: quiet diff",
			manifest: func() *manifest.Manifest {
				tk := task("one")
				tk.Check = "diff -q expected.txt actual.txt"
				return oneTaskManifest(tk, false)
			},
			wantRule: RuleCheckSilent,
		},
		{
			// W2 negative: quiet diff followed by a failure-output branch.
			name: "check-silent: quiet diff with failure branch is fine",
			manifest: func() *manifest.Manifest {
				tk := task("one")
				tk.Check = "diff -q a b || { echo FAIL; diff a b; exit 1; }"
				return oneTaskManifest(tk, false)
			},
			wantAbsentRule: RuleCheckSilent,
		},
		{
			// W2 case 3: quiet grep.
			name: "check-silent: quiet grep",
			manifest: func() *manifest.Manifest {
				tk := task("one")
				tk.Check = "grep -q x file"
				return oneTaskManifest(tk, false)
			},
			wantRule: RuleCheckSilent,
		},
		{
			// W2 case 4: chained quiet-grep + existence probes.
			name: "check-silent: chained probes",
			manifest: func() *manifest.Manifest {
				tk := task("one")
				tk.Check = "grep -q x file && test -s output.txt"
				return oneTaskManifest(tk, false)
			},
			wantRule: RuleCheckSilent,
		},
		{
			// W7: spec under 80 chars is probably underspecified.
			name: "spec-underspecified: short spec",
			manifest: func() *manifest.Manifest {
				tk := task("one")
				tk.Spec = "Do it."
				return oneTaskManifest(tk, false)
			},
			wantRule: RuleSpecUnderspecified,
		},
		{
			// W8: short spec whose substance is "go read this other file".
			name: "spec-file-pointer: pointer to instruction file",
			manifest: func() *manifest.Manifest {
				tk := task("one")
				tk.Spec = "Read the instructions at /tmp/brief.md and do exactly what it says in there."
				return oneTaskManifest(tk, false)
			},
			wantRule: RuleSpecFilePointer,
		},
		{
			// W8 negative: a long spec that references a file as CONTEXT
			// (not as the whole plan) must not be flagged.
			name: "spec-file-pointer: long contextual spec is fine",
			manifest: func() *manifest.Manifest {
				tk := task("one")
				tk.Spec = longContextualSpec
				tk.ExpectFiles = []string{"review.md"}
				return oneTaskManifest(tk, false)
			},
			wantEmpty: true,
		},
		{
			// W9: no expect_files (and not worktrees) means the verifier
			// can't confirm what the task produced.
			name: "missing-expect-files: empty expect_files",
			manifest: func() *manifest.Manifest {
				tk := task("one")
				tk.ExpectFiles = nil
				return oneTaskManifest(tk, false)
			},
			wantRule: RuleMissingExpectFiles,
		},
		{
			// W9 negative: worktrees mode legitimately exports deliverables
			// outside the taskdir, so the finding must not fire there.
			// (Constructed directly since manifest.FromBytes currently
			// rejects worktrees:true outright — Plan 3 territory.)
			name: "missing-expect-files: worktrees manifest not flagged",
			manifest: func() *manifest.Manifest {
				tk := task("one")
				tk.ExpectFiles = nil
				return oneTaskManifest(tk, true)
			},
			wantAbsentRule: RuleMissingExpectFiles,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			findings := Check(tc.manifest())
			if tc.wantEmpty && len(findings) != 0 {
				t.Fatalf("expected zero findings, got %+v", findings)
			}
			if tc.wantRule != "" && !hasRule(findings, "one", tc.wantRule) {
				t.Fatalf("expected rule %q on task \"one\", got %+v", tc.wantRule, findings)
			}
			if tc.wantAbsentRule != "" && hasRule(findings, "one", tc.wantAbsentRule) {
				t.Fatalf("expected rule %q ABSENT on task \"one\", got %+v", tc.wantAbsentRule, findings)
			}
		})
	}
}
