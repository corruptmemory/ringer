# Blueprint — adapt with care

# test-hardening

## What it is

A test-hardening swarm assigns one module or behavior to each worker and lets that worker add tests only inside owned test files. The check runs the real test command, parses the runner summary, proves the test count increased, rejects assertion-free tests, rejects production-code edits, and exports the patch before Ringer deletes the passing worktree.

This is a blueprint, not a recorded proven kit. Adapt the test-count regex and assertion pattern to the runner before using it on a real branch.

## When to use

Use this after a review, migration, or bug-fix pass when the next useful work is coverage across many independent modules.

Do not use this when workers need to change production code, shared fixtures, global test setup, snapshots, or runner configuration. Split that setup into a separate fix task first, then run test-hardening with test-only ownership.

## Fill in

| Placeholder | What goes there |
|---|---|
| `{{PROJECT}}` | Short project name used in the run name. |
| `{{WORKDIR}}` | Scratch run directory outside the repo. |
| `{{REPO_PATH}}` | Absolute path to the repo that Ringer should create task worktrees from. |
| `{{TEST_KEY}}` | Stable task key; also becomes the exported patch filename. |
| `{{OWNED_TEST_FILES — semicolon-separated repo-relative test files or test directory prefixes this worker may modify}}` | Exact test files or test directory prefixes this worker may create or edit. Do not include `src/` or production directories. |
| `{{FORBIDDEN_PATHS}}` | Semicolon-separated production paths that must not change, for example `src/;app/;lib/;packages/core/src/`. |
| `{{MODULE_UNDER_TEST — source module, route, command, or behavior to cover, with entry-point files for reading only}}` | Source surface the worker may read but not edit. |
| `{{TEST_BEHAVIOR — the missing behavior, edge case, regression, or invariant the new tests must prove}}` | The behavior the new tests should prove. |
| `{{TEST_COMMAND}}` | Exact test command to run from the worktree. Use the same command to collect `{{BASELINE_TEST_COUNT}}`. |
| `{{BASELINE_TEST_COUNT}}` | Integer test count from the same runner command before the swarm starts. |
| `{{NEW_TEST_FILES}}` | Semicolon-separated repo-relative test files that must exist after the worker finishes. |
| `{{PYTHON}}` | Python executable for the check script. |
| `{{KIT_DIR}}` | Absolute path to `templates/test-hardening` in this Ringer checkout or copied kit location. |
| `{{TEST_COUNT_REGEX}}` | Regex with one capture group for the total test count in runner output, or `AUTO` for common pytest, Jest, Vitest, Mocha, and unittest summaries. |
| `{{ASSERTION_PATTERN}}` | Regex counted as an assertion, for example `\\b(expect|assert|toEqual|toBe|pytest\\.raises)\\b`. |
| `{{MIN_ASSERTIONS_PER_FILE}}` | Minimum assertion-pattern matches required in each new test file. |
| `{{MIN_ASSERTION_DENSITY}}` | Minimum assertions per non-empty non-comment line, for example `0.02`. |
| `{{EXPORT_DIR}}` | Absolute directory outside all task worktrees where checks write `<task-key>.patch`. Create it before the run. |

## Checks

`checks/test_hardening_check.py` first inspects changed paths and fails if any path is outside the owned test files or inside a forbidden production path.

It verifies every `{{NEW_TEST_FILES}}` path exists, counts assertion matches and assertion density, runs `{{TEST_COMMAND}}`, parses the runner summary, requires the parsed count to be greater than `{{BASELINE_TEST_COUNT}}`, then stages and exports a non-empty patch to `{{EXPORT_DIR}}/{{TEST_KEY}}.patch`.

This is hard to game because a worker cannot make tests pass by editing source, cannot add empty smoke tests with no assertions, and cannot claim more coverage unless the runner summary count actually increases.

`expect_files` is empty on purpose. In worktrees mode, the patch is produced by the check outside the task worktree; Ringer's normal task-file harvest would otherwise point at files that are deleted after PASS.

## Mix with

Use `templates/review-swarm.json` first to identify missing coverage per module. Use `templates/fix-swarm.json` first when tests require a production-code fix before hardening.

Use `templates/migration-swarm/` before test-hardening when a mechanical migration changed many modules and needs focused regression coverage.

## Gotchas

A worker who can edit source can make any test pass. Ownership must exclude `src/` and any other production-code paths.

The baseline count must come from the same `{{TEST_COMMAND}}` and same runner configuration. A different shard, filter, or reporter makes the increase meaningless.

Snapshot updates are production-like side effects for this pattern unless the task explicitly owns the snapshot files. Keep them out by default.

Passing tasks get their worktree deleted. The exported patch is the deliverable.

