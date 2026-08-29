# Changelog

All notable changes to WARD. History and session detail live in
`.arch/tasks.md`; this file is the distilled release view.

## Unreleased — context management (mechanical reload + mid-task checkpoint)

Closes the gap the consulting review named: reload was 100% protocol-trust
(`ward brief` had to be remembered every time) and there was no mid-task
offload (`capture` only fires at task close). Built from the two feedback
files; the unverifiable parts they suggested (token-% `context status`,
natural-language `context query`) were deliberately rejected.

### Added

- **Mechanical scoped reload**: `ward task next` and `ward task run` now print,
  non-optionally, prior knowledge scoped to the task's tags (via `memory
  context`) and the latest checkpoint. Reload is structural, not agent
  discipline. `task run --json` carries `prior_knowledge` + `latest_checkpoint`.
- **`ward task checkpoint <id> "<summary>" [--verify CMD]`**: a mid-task
  offload that records a partial capture WITHOUT closing the task — the
  sanctioned compaction point for a task long *within itself*. The optional
  `--verify` is executed and its exit code stored, but it never gates (a
  checkpoint is a progress note, not a gate). Shown in `ward task show`
  (text + JSON) and fed back into the next reload.
- `checkpoints` table (v8 migration) — authored mid-session state; distinct
  from run evidence, which stays disk-derived.
- AGENTS.md 4d: mechanical reload + checkpoint rule (deterministic, not
  "compact when you feel full").

### Rejected (from the reviews, intentionally)

- `ward context status` (token-% of the LLM window): Ward cannot see the
  context window; a fabricated percentage is ceremony without act.
- Natural-language `ward context query`: unverifiable retrieval; the existing
  FTS + tag selector already covers targeted lookup.

## [v0.8.0] — 2026-08-29

Consolidation release: zero new subsystems. Release engineering, command-
surface audit, adversarial tests for the fast-built surface, README as
product. Ward is now explicitly a **solo** loop tool.

### Added

- **Makefile release engineering**: `build` (git-derived version stamp via
  `-ldflags`), `test`, `fmt`, `vet`, `install` (`$GOPATH/bin`, override with
  `BINDIR=`), `check` (full gate). Ends the hand-placed-binary era.
- **Command-surface contract, enforced by tests**: every `--json` output is
  valid JSON on empty stores too — collections serialize `[]`, never `null`.
  Table-driven coverage for timeline, wave, scorecard, skill pack / check /
  sync, explain, reject, capture.
- **`Example:` help lines on every subcommand**, and `-n` shorthand parity:
  any command taking `--limit` accepts `-n`.
- **Transparency patch**: `ward task show` opens the run's sidecar log (the
  audit window that should have shipped with `task run`); `ward task done`
  refuses to close a gated task without sidecar evidence proving it ran and
  exited 0; `ward task done --force` records `force-closed` (distinct from
  `done`) so a bypass is never silent or conflated with a verified completion.
  Sidecar evidence is **derived from `.ward/logs/` on disk** — no DB column
  shadows it.
- **CHANGELOG.md** (this file) distilled from the tasklog.

### Fixed

- **Cobra-level errors were silent**: a bad flag or unknown command exited 1
  without printing anything. All failures now print exactly one error line
  (text or JSON).
- **`ward workflow Save` did not stamp its path**, so runs started by
  `ward task run` persisted an empty `workflow_path` and `ward capture --run`
  could never resolve the workflow it needed (R7 fix #2 was unreachable on
  this path). Save now records where the workflow lives.
- **Bounce attribution was structurally dead**: `FailTask` nulled
  `claimed_by`, so the scorecard's bounced/rejected columns could never count
  anything. The last holder's name now survives a bounce (active-claim
  consumers all filter on `status='claimed'`), and open tasks with escalation
  classify as bounces.
- **`ward skill-sync --json` was a silent dry-run** while human mode synced;
  both modes now do the same work, and JSON reports what was pushed.

### Retired

- **`ward fleet` and `scripts/fleet-launch`** — parallel agent dispatch was
  built, measured against solo in R12/R13, and retired: solo dominated at every
  scale tested (solo 19.9 min vs fleet 44.7 min, quality parity), and supervising
  parallel CLI agents cost more than it returned. Ward is now a solo loop
  (`brief → task → tick → memory handoff`). `scripts/fleet-launch` moved to
  `attic/fleet-launch.deprecated`; the `ward fleet` command is deprecated and
  slated for removal once `wave` is split out of `internal/cli/fleet.go`.

## [v0.7.0] — 2026-08-23

Brain-to-chip: `ward skill pack <topic>` compiles gated knowledge into
loadable agent chips (`SKILL.md`); `ward skill check` detects stale sources;
chips carry their oracle-store locator; `ward skill-sync` pushes
`portable:<topic>` knowledge to the global skills directory. Spec:
`.spec/skills.md`.

## [v0.6.0] — 2026-08-23

R&D loop: `ward harvest` telemetry spine (tier distribution, cheap+verified
rate, bounce leaders, dossier themes, drift); explorer/architect protocol
with the self-promotion gate enforced in `memory put`. Spec:
`.spec/research.md`.

## [v0.5.0] — 2026-08-22

Self-consulting tool + dispatch loop: agent-doc injection into `AGENTS.md`
(`ward init`), `ward brief` session bootstrap, the dispatch pool
(`task add/next/run/take/drop/done/fail/workflow`) with tier-floor admission,
`tick --heal` drift healing, `explain` evidence chains, reject dossiers.
v0.5.x–v0.5.z hardened dogfooding: honest capture counts, claim atomicity,
fleet pattern proven on donate-fair.

## [v0.4.0] — 2026-08-22

Parallel-dispatch foundations: database-arbitrated exclusive claims (unique
index, hard conflict error, cross-process busy timeout), declared `tier:` as
a routing floor, `init --scaffold`/`--docs`. Legacy-claim gap surfaced via
`doctor`.

## [v0.3.0] — 2026-08-22

Usable-tool freeze: model adapter wired to real opencode models at the routed
tier; D0.3 trust boundary closed (`memory put` defaults non-local);
claim/context/stale ergonomics; strict pre-columns migration test.

## [v0.2.0] — 2026-08-22

Result capture: auto-write accepted artifacts on run/resume so the next
session routes cheap without hand-typed YAML.

## [v0.1.0] — 2026-08-22

Thesis freeze: verify-gated routing, live verify-on-read, escalation-on-
failure, Context column, idempotent migrations.
