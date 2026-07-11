# Provenance & Credits

This repository is a ground-up Go rewrite of **Ringer** and its **Ringside**
HUD. The rewrite replaces the original Python orchestrator and Rust/Tauri
desktop app with a single static Go binary — but the *product* it implements,
and nearly every external contract it honors (the manifest schema, the engine
spawn model, the on-disk state, the Ringside dashboard, the artifact pages, the
evidence-based routing scoreboard), is inherited work. This document records
where it came from and gives credit to the people who built it first.

## The original Ringer & Ringside

**Copyright © Nate Jones Media LLC — Ringer and Ringside.**
Upstream: <https://github.com/NateBJones-Projects/ringer>

The original was created by **Jonathan Edwards** (`justfinethanku@gmail.com`).
Starting 2026-07-02 he built, in order: the Python `swarm.py` orchestrator (soon
renamed **Ringer**), a native **SwarmHUD** (Swift), and then **Ringside** — the
live dashboard, rebuilt as a Tauri v2 app (Rust backend + web frontend) that
polled `~/.ringer/runs/*.json` and showed every live swarm on the machine. The
early development was collaborative and, per the working notes preserved in this
repo's git history, AI-assisted on both the code and the design.

That original codebase — an 8,300-line Python orchestrator, a Tauri HUD, an
embedded HTTP dashboard, and a design plan for zero-LLM live status/report
artifacts — is the foundation this project stands on. The hard part was never
the language port; it was figuring out *what a verified swarm-of-engines
delegation tool should be*. That figuring-out was theirs.

## What this rewrite inherited (and kept deliberately)

The Go rewrite treats the original's product decisions as **frozen contracts**,
not suggestions. Credit for the design of the following belongs to the original
authors; the Go code merely re-implements them faithfully:

- The manifest → tasks → engines → verified-check → single-retry orchestration model.
- **Ringside**, the local dashboard that opens automatically and shows every live run.
- The zero-LLM live **artifact** pages (status, report, index, versioned library).
- The per-attempt **eval log** and the **scoreboard** — evidence-based routing by
  first-try pass rate, computed from your own history.
- The engine spawn contract (`args_template` DSL, token regex, full-access gating)
  and the macOS Seatbelt sandbox lane (`engines/opencode-sandboxed.sh`, kept as-is).

## This repository

**Ringer (Go)** — <https://github.com/corruptmemory/ringer> — is a fork
maintained by **Jim Powers**, rewriting the Python/Rust implementation as one
`CGO_ENABLED=0` Go binary (chi + templ + htmx for the HUD, `modernc.org/sqlite`
for the eval store, a rootless jail for isolation). The motivation was operational:
Python is a deployment landmine and the Tauri lane was a lagging prototype the
original README already steered users away from. The goal was explicitly *not* to
redesign the product — only to swap the toolchain underneath it.

## License

Ringer and Ringside are distributed under the **PolyForm Shield License 1.0.0**
(see [`LICENSE.md`](LICENSE.md)). Per that license, this notice is carried
forward verbatim:

> Required Notice: Copyright Nate Jones Media LLC — Ringer and Ringside
> (https://github.com/NateBJones-Projects/ringer)

## A note on the working notes

Two hand-written working documents from the original development —
`ringside-upgrade-notes.md` (Jonathan Edwards's upgrade log) and
`ringer-live-artifacts-plan.md` (the zero-token artifacts design plan) —
described the now-deleted Python/Rust architecture. Rather than keep stale
technical logs about deleted code in the tree, their essence and credit are
distilled here; the full originals remain available in this repository's git
history for anyone who wants the detail.
