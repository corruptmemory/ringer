# Spike findings — 2026-07-08 (Plan 1)

## S1: opencode under namespace-UID-0 (spec §13.1)

- Command: `go test -tags=spike -run TestSpikeOpencodeNsRoot -v ./internal/jail/`
- Verdict: **tolerates ns-root.** `opencode --version` (v1.17.15) exits 0
  running as ns-root (mapped uid=0 inside the namespace) with no
  `SetDropUser` configured.
- Mounts used: `HostMounts(root)` (read-only `/usr`, `/etc`, `/bin`, `/lib`,
  `/lib64`, `/sbin`) + a writable workspace bind + a `TmpfsMount` scratch +
  one extra read-only bind not in the brief's original sketch:
  `BindMount("/home/jim/.opencode", filepath.Join(root, "/home/jim/.opencode"), true)`
  — this makes the opencode binary (and its bundled `node_modules`) visible
  inside the jail at its host-identical absolute path, so
  `j.Command("/home/jim/.opencode/bin/opencode", "--version")` resolves
  exactly as it would on the host. No other mounts were required — the
  first attempt with this mount set succeeded on the first try. `opencode
  --version` did **not** need `~/.config/opencode` or `~/.local/share/opencode`
  bind-mounted; those directories were not mounted and the probe still
  passed.
- Raw output (verbatim from the `-v` run):

  ```
  === RUN   TestSpikeOpencodeNsRoot
      spike_opencode_test.go:65: opencode --version (ns-root):
          err=<nil>
          1.17.15
      spike_opencode_test.go:69: id inside jail: err=<nil>
          uid=0(root) gid=0(root) groups=0(root)
      spike_opencode_test.go:75: VERDICT: opencode tolerates ns-root — SetDropUser not needed for the opencode lane
  --- PASS: TestSpikeOpencodeNsRoot (0.63s)
  PASS
  ok  	github.com/corruptmemory/ringer/internal/jail	0.629s
  ```

- Plan 2 consequence:
  - `SetDropUser` wiring needed for the opencode lane: **no.** ns-root is
    sufficient; opencode does not refuse to run as uid 0 the way Claude
    Code's permission-bypass safety check does (see `unshare.go`'s comment
    on `SetDropUser`). Jailed opencode workers can run without dropping
    privileges, so the UID-mapping cleanup tax (files owned by a mapped
    subuid needing chown-back after teardown) does not apply to this lane.
  - `jail_state_dirs` needed for `--version`: **none.** Only the read-only
    bind of the opencode install directory itself
    (`/home/jim/.opencode` → same path inside the jail) was required. This
    spike did not exercise `opencode run` (an authenticated task execution),
    which may touch `~/.local/share/opencode` (auth.json, opencode.db) or
    `~/.config/opencode`; if Plan 2's real worker invocations do more than
    `--version`, those two host directories should be added to
    `jail_state_dirs` as candidate binds and re-probed before assuming they
    are unnecessary. This spike only proves the `--version` cold-start path.

## S2: worktrees x jail (spec §13.2)

- (filled by Task 8)

## S3: modernc multi-process smoke (spec §13.3)

- Command: `go test -run TestMultiProcessWrites -v ./internal/store/`
- Verdict: **PASS.** After Task 5's `Open()` pragma-order fix (commit
  `4ca0288`, "fix: arm busy_timeout before WAL switch in store Open
  (multi-process init)" — reorders `openPragmas` so `busy_timeout` is set
  before `journal_mode=WAL`, and wraps the pragma loop in `withBusyRetry`),
  the test was re-sampled at depth well beyond the single-run minimum:
  **80/80 runs, 0 failures** (`go test -run TestMultiProcessWrites -count=1
  ./internal/store/`, looped 80x as separate process invocations, the same
  way CI runs it). Task 6's report had originally found a ~20-25% flake rate
  (`SQLITE_BUSY_RECOVERY` on concurrent WAL init) before this fix landed;
  that flake is gone post-fix.
  - Row count: 1000/1000 (5 children × 200 rows), `Integrity()` clean —
    both implied by PASS (`t.Errorf` on either condition failing).
- libc pin: `go list -m modernc.org/libc` → `modernc.org/libc v1.73.4`

## Jail preflight on this machine (Task 4 step 3)

```
$ go test -run TestPreflightReportsStatus -v ./internal/jail/
=== RUN   TestPreflightReportsStatus
    unshare_test.go:22: UnshareFound=true UserNSEnabled=true SubUIDMapped=true SubGIDMapped=true OK=true
--- PASS: TestPreflightReportsStatus (0.00s)
PASS
```

`CheckUnsharePreflight().OK() == true` on this machine: `unshare` binary
present, `kernel.unprivileged_userns_clone=1`, and this user has both
`/etc/subuid` and `/etc/subgid` entries. Rootless `UnshareJail` genuinely
runs commands as ns-root here — consistent with Task 4's
`TestUnshareJailRunsCommand` passing live and with this spike's own `id`
probe reporting `uid=0(root)` inside the jail.
