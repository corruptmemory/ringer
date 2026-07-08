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

- Command: `go test -tags=spike -run TestSpikeWorktreeInJail -v ./internal/jail/`
- Setup: a parent repo (`git init`, one commit adding `hello.txt`), then
  `git worktree add wt-alpha` off it. An uncommitted edit was made to
  `hello.txt` in the worktree before jailing, to also exercise `git diff`
  (not just `git status`).
  Three probes vary the variable **one at a time** so the finding is
  actually isolated (an earlier version of this doc changed two variables
  between A and B and overstated the requirement — Probe C corrects it).
- **Probe A (baseline failure — both mismatched):** worktree bind-mounted
  rw at a jail-internal path (`<rootA>/workspace`) AND parent repo
  bind-mounted RO at a **different**, jail-internal path
  (`<rootA>/parent-repo`) that does **not** match its host path.
  Command inside jail: `git -C /workspace -c safe.directory=* status`.
  **Verdict: FAILS** (`fatal: not a git repository: (null)`, exit 128) —
  the worktree's `.git` file contains
  `gitdir: <host-repo-path>/.git/worktrees/wt-alpha`, an absolute host path
  that does not resolve inside the jail when the parent repo is mounted
  elsewhere. (Note: this varies two things at once — Probe C isolates
  which one actually matters.)
- **Probe B (baseline success — both host-identical):** both worktree and
  parent repo bind-mounted at their **host-identical** absolute paths
  inside the jail (`<rootB><host-abs-path>`), parent mounted **read-only**.
  Commands inside jail: `git -C <wt> -c safe.directory=* status` and
  `git -C <wt> -c safe.directory=* diff`.
  **Verdict: SUCCEEDS** for both `status` and `diff`, with the parent
  mounted read-only. `diff` correctly showed the uncommitted `hello.txt`
  edit, confirming RO parent access is sufficient to read the objects/refs
  needed for a working-tree diff, not just a status check.
- **Probe C (the ISOLATING probe — worktree jail-internal, parent
  host-identical):** worktree bind-mounted rw at a **jail-internal** path
  (`<rootC>/workspace`); parent repo bind-mounted RO at its
  **host-identical** absolute path. No `safe.directory` (consistent with
  the incidental finding below).
  Commands inside jail: `git -C /workspace status` and
  `git -C /workspace diff`.
  **Verdict: SUCCEEDS** for both `status` and `diff`. This is the decisive
  result: **only the PARENT repo needs a host-identical mount path.** The
  worktree itself can live at any convenience path (`/workspace`), because
  a worktree's `.git` file only encodes the PARENT's absolute path
  (`gitdir: <parent>/.git/worktrees/<name>`); git resolves via that pointer
  and never consults a reverse pointer keyed on the worktree's own
  location. Probe A failed because it moved the *parent*, not because it
  moved the worktree.
- **Incidental-noise check (dubious ownership):** the task brief predicted
  git running as ns-root (uid 0 inside the namespace) over host directories
  owned by uid 1000 would trigger "detected dubious ownership" and require
  `-c safe.directory=*` or a global `safe.directory` config. **This did NOT
  happen on this machine.** A diagnostic probe ran plain
  `git -C <wt> status` (no `safe.directory` at all) inside the host-identical
  jail and it succeeded cleanly — no ownership complaint. Root cause:
  `UnshareJail.Command()` uses `unshare --map-root-user`, which identity-maps
  the invoking real UID (1000, `jim`) to namespace UID 0. Host files owned by
  uid 1000 therefore appear, from inside the namespace, as owned by uid 0 —
  matching the git process's own euid 0. There is no UID mismatch to trigger
  git's ownership check in this specific mapping mode. `-c safe.directory=*`
  was still included on all probe commands defensively (cheap, harmless,
  and correctly isolates this from the real path-mismatch signal being
  tested) but is **not required** under `--map-root-user`. This would likely
  differ under a `--map-auto`-only (multi-UID/subuid-range) mapping, which
  Plan 2 should keep in mind if it ever changes the mapping mode.
- **HOME:** not set/mounted inside the jail (no `/home` bind, `cmd.Env` left
  at its default inherited value, so `HOME` pointed at the host's
  `/home/jim`, which does not exist inside the chroot). No failure or
  warning resulted — `git status`/`git diff` with `-c safe.directory=*`
  read no config file that required `$HOME` to exist. **Not needed** for
  this probe; flagged here in case a future probe (e.g. one that needs to
  write `~/.gitconfig`, or a `git commit`) requires it.
- `id` inside jail B: `uid=0(root) gid=0(root) groups=0(root)` — confirms
  ns-root, consistent with T7's finding.
- Verbatim output (from the `-v` run, `/tmp/spike-worktree.txt`):

  ```
  === RUN   TestSpikeWorktreeInJail
      spike_worktree_test.go:103: PROBE A (both at mismatched jail-internal paths): err=exit status 128
          fatal: not a git repository: (null)
      spike_worktree_test.go:132: id inside jail B: err=<nil>
          uid=0(root) gid=0(root) groups=0(root)
      spike_worktree_test.go:135: PROBE B diagnostic (host-identical paths, NO safe.directory): err=<nil>
          On branch wt-alpha
          Changes not staged for commit:
            (use "git add <file>..." to update what will be committed)
            (use "git restore <file>..." to discard changes in working directory)
          	modified:   hello.txt

          no changes added to commit (use "git add" and/or "git commit -a")
      spike_worktree_test.go:138: PROBE B (host-identical paths, parent RO, safe.directory=*): err=<nil>
          On branch wt-alpha
          Changes not staged for commit:
            (use "git add <file>..." to update what will be committed)
            (use "git restore <file>..." to discard changes in working directory)
          	modified:   hello.txt

          no changes added to commit (use "git add" and/or "git commit -a")
      spike_worktree_test.go:141: PROBE B git diff (host-identical paths, parent RO): err=<nil>
          diff --git a/hello.txt b/hello.txt
          index ce01362..94954ab 100644
          --- a/hello.txt
          +++ b/hello.txt
          @@ -1 +1,2 @@
           hello
          +world
      spike_worktree_test.go:149: VERDICT (diff): git diff also succeeds with parent RO
      spike_worktree_test.go:175: PROBE C (worktree at jail-internal /workspace, parent host-identical RO): status err=<nil>
          On branch wt-alpha
          Changes not staged for commit:
            (use "git add <file>..." to update what will be committed)
            (use "git restore <file>..." to discard changes in working directory)
          	modified:   hello.txt

          no changes added to commit (use "git add" and/or "git commit -a")
      spike_worktree_test.go:178: PROBE C git diff (worktree at /workspace, parent host-identical RO): err=<nil>
          diff --git a/hello.txt b/hello.txt
          index ce01362..94954ab 100644
          --- a/hello.txt
          +++ b/hello.txt
          @@ -1 +1,2 @@
           hello
          +world
      spike_worktree_test.go:183: VERDICT (probe A): both-mismatched breaks git status (baseline failure)
      spike_worktree_test.go:187: VERDICT (probe C): ISOLATED — worktree at jail-internal /workspace works when parent is host-identical. Only the PARENT needs a host-identical mount path; the worktree can live at a convenience path.
      spike_worktree_test.go:194: VERDICT (probe C diff): git diff also succeeds with worktree at /workspace + parent host-identical RO
  --- PASS: TestSpikeWorktreeInJail (0.48s)
  PASS
  ok  	github.com/corruptmemory/ringer/internal/jail	0.483s
  ```

- Plan 2 consequence — the worktrees-mode jail mount rule:
  - **Only the PARENT repo must be bind-mounted at its host-identical
    absolute path** inside the jail (Probe C, isolated). The **worktree
    can be bind-mounted at a jail-internal convenience path** (e.g.
    `/workspace`) — this is simpler and avoids exposing host directory
    structure inside the jail. The requirement exists because the
    worktree's `.git` file embeds the *parent's* absolute host path
    (`gitdir: <host-parent>/.git/worktrees/<name>`), which git resolves
    literally at runtime with no jail-relative rewriting; the worktree's
    own mount location is never encoded anywhere, so it is free.
  - Concretely: `BindMount(worktree, "<root>/workspace", rw)` +
    `BindMount(parentRepo, "<root><host-abs-path-of-parent>", ro)`.
  - **Parent repo RO is sufficient for `git status` and `git diff`.** Plan
    2's worktrees-mode mount table can mount the parent repo read-only for
    any lane that only needs to inspect/diff, which is the common case for
    review/verification workers.
  - **`git commit` was not probed** and needs `.git/worktrees/<name>`
    (inside the parent repo's `.git` directory) writable — i.e., the parent
    repo mount must switch to RW, or at minimum a targeted RW bind of
    `<parent>/.git/worktrees/<name>`, for any lane that commits from inside
    the jailed worktree. Re-probe before Plan 2 relies on jailed commits.
  - **`safe.directory` handling:** include `-c safe.directory=*` (or
    equivalent) defensively on jailed git invocations, but it was proven
    *not required* under this jail's actual mapping mode
    (`unshare --map-root-user`, which identity-maps the real UID to
    namespace UID 0 and therefore preserves ownership matching for git's
    dubious-ownership check). Do not assume this holds if Plan 2 ever
    changes the UID-mapping mode away from `--map-root-user`.
  - **`HOME`:** no special handling needed for `status`/`diff`. Revisit if
    a jailed lane needs to write git config or perform operations that
    touch `~/.gitconfig`.

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

### Update (2026-07-08, post-CI): mount-namespace capability probe added

CI run `https://github.com/corruptmemory/ringer/actions/runs/28974880563`
showed `TestUnshareJailRunsCommand` FAILING on `ubuntu-latest` (GitHub
Actions) with `unshare: cannot change root filesystem propagation:
Permission denied`, while `macos-latest` (which skips the whole jail
suite — no Linux user namespaces) and the store multi-process smoke (S3,
above) both passed. The three checks this section originally described
(binary present, `unprivileged_userns_clone`, `/etc/subuid`+`/etc/subgid`)
all reported `OK` on the GitHub runner too, so the live test didn't skip
and instead crashed against a namespace that can't actually perform the
mount operations the jail needs — the same failure mode a hardened
production host could hit silently in the real product.

`CheckUnsharePreflight()` now adds a fourth check: `MountNSUsable`. When
`UnshareFound` is true, preflight actually invokes `unshare` with the same
flag set `UnshareJail.Command()` uses (`--fork --pid --mount --map-auto
--map-root-user --setuid 0 --setgid 0`), running `true` in place of the
real bash script. If that invocation fails, its combined output is
appended to `PreflightResult.Errors` (so `OK()` becomes `false`) instead
of only being caught by a live jail run doing real work. This is cheap
(runs `true`, side-effect-free) and only executes during preflight, which
the product invokes when a task actually needs a jail.

`unshare_test.go`'s `TestUnshareJailRunsCommand` already routed through
`requireUnshare(t)` → `CheckUnsharePreflight()` → `t.Skipf(...)` on
`!OK()`; no test-side change was needed beyond this preflight fix — the
existing skip-guard now correctly fires on hosts like GitHub's
`ubuntu-latest` runners where the mount-namespace probe fails, while it
continues to run (and pass) live on this machine and any other host where
the probe succeeds.
