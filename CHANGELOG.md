# Changelog

All notable changes to WARD. History and session detail live in
`.arch/tasks.md`; this file is the distilled release view.

## [v0.9.6] — 2026-08-30 — fix: `ward wave --heal` respects the trust boundary (imported artifacts are not drift)

A reviewer's check of the skill roadmap caught what no spec did: `waveCmd`
verified every topic-tagged artifact with `verification.Run`, which by design
returns `"unknown"` for **imported** (`Local=false`) artifacts because their
verify_cmd must never execute. The wave loop treated anything non-`verified`
as drifted and, under `--heal`, **superseded imported artifacts purely because
they were ineligible for live verification** — so a topic mixing localized and
still-imported knowledge would silently lose the imports. Tick's `sweepVerify`
had always guarded `!a.Local`; wave did not.

### Fixed

- **`ward wave` skips `Local=false` artifacts entirely** (tick parity): never
  verified, never drift-counted, never superseded. An import is not drift just
  because it cannot be re-verified here.
- **Wave drift is now `stale`/`error` only** (matching tick's absolute-drift
  definition): an `unknown` result for a LOCAL artifact (e.g. unexecutable
  verification kind) is a config problem, not drift — it is neither counted
  nor superseded.
- Regression gate `TestWaveHealSparesImportedArtifacts` proves both halves:
  an imported-then-verified artifact tagged like a drifted local one survives
  `--heal` untouched, while the real local drift on the same tag is
  superseded with reason `"wave drift"`.

## [v0.9.5] — 2026-08-30 — control-plane: honest sensors, cheap-hit KPIs, skill localization

The reflection control study (`.arch/reflection-control-study.md`) found three
control loops in Ward that were noisy or had no feedback:
(1) `brief` hardcoded every stale claim's age as `"30+"` instead of computing
it; (2) `tick`'s drift counter only counted artifacts that had been verified
and THEN failed, hiding a drifted/error artifact that was never verified;
(3) no telemetry could tell whether the "verified memory enables cheaper
routing" thesis was actually paying off. Released as `ward kpis` (the 
outcome-based `ward scorecard` is untouched). Separately, the thesis loop now
closes at the source: a global `portable:*` skill chip can be localized into a
repo as a live-verified, store-local artifact. The audit also confirmed the
topic-scoped adapt loop already existed as `ward wave <topic> [--heal]`, so
that spec closed with no code.

### Added

- **`ward kpis [--window 24h|7d|2w]`**: routing-control telemetry computed from
  `routing_decisions` — cheap-hit %, escalation %, verify-pass %, memory-miss
  %, with per-window `--json`. Additive nullable `execution_success` column
  (migration v9) stamped by the engine: `1` on a node's done, `0` on every
  failed/rejected attempt (escalate, budget-exhausted, preflight, identical
  failure); decisions never reached by an outcome stay NULL (unknown, never
  guessed). `ward kpis` never conflicts with the existing outcome-based
  `ward scorecard` (engineer performance).
- **`ward skill install <topic> --verify-cmd <gate>`**: localizes a global
  skill chip (`~/.config/opencode/skills/ward-<topic>/SKILL.md`) into THIS
  repo's store as one fresh local artifact (`Local=true`, tag `topic:<topic>`),
  runs the user-supplied gate immediately via `verification.Run`, and stamps
  `verified` (votes cheap on future routes) or `error` (artifact exists but
  never votes cheap; install errors explicitly). A missing gate is rejected.
- **`ward skill list-global [--dir]`**: lists available chips — the install
  surface.

### Fixed

- **Computed claim age in `ward brief`**: stale claims now report real
  `mins_aged` / `age_hours` via `claimAge()` parsing `claimed_at`
  (`2006-01-02T15:04:05Z`, legacy space-separated variant tolerated); the
  hardcoded `"30+"` literal is gone, and the human brief shows the age.
- **Unbiased drift count in `ward tick`**: `sweepVerify` counts EVERY local
  accepted artifact whose live re-verify returns `stale` or `error`
  (absolute), not only transitions from a previous verified state; the next
  action now reads "N local artifact(s) FAILED live verification".

### Release

- Specs closed: `control-claim-age`, `control-drift-sensor`, `control-antiwindup`
  (regression gate only), `control-skill-sharpen` (already built as `ward wave`),
  `control-scorecard`, `control-skill-localize`. `control-experiment-watchdog`
  remains deferred by condition. Build order recorded in `.spec/control-index.md`.

## [v0.9.4] — 2026-08-29 — fix: ward-itself requests filed in the wrong store (cross-project routing)

A recurring coordination failure: an agent working in project X would file a
feature request ABOUT ward into X's `.ward` store, where ward's own agents never
see it. The battlefield auto-plan/PM request sat invisible in `dhda-workspace/.ward`
for this reason. Fixed with explicit cross-project routing.

### Added

- **`ward project register <name> <path>` / `ward project list`**: a registry
  (`~/.config/ward/projects.json`, or `WARD_PROJECT_<NAME>_HOME`) mapping a
  logical project name to its `.ward` directory.
- **`--project <name>` persistent flag** on every command: targets a project store
  by name from any cwd. `ward task add --project ward ...` now lands in ward's
  store regardless of where the agent runs. Resolved via `store.OpenForName`.
- **Misplacement guard**: `ward task add` / `ward memory put` warn (to stderr)
  when an item tagged `ward` or `portable:` is filed into a store that is NOT
  ward's own — the exact confusion this release fixes. Suppressed when
  `--project ward` is given or the agent is already in ward's store.
- Relocated the stranded auto-plan/PM request (`dhda-workspace/.ward` →
  ward's store as `task-e244d012`); originals marked `[RELOCATED ...]`.
- `.spec/auto-plan.md` evaluates the PM feature (narrow: plan = populate the pool,
  never auto-execute) and decomposes it; the relocated task's gate is now a real
  implementation test, not a phantom `test -s` check.

## [v0.9.3] — 2026-08-29 — close the routing≠knowing gap (verified evidence injection)

Evaluated an external battlefield review (openai.md P1#8 + claude context review):
Ward's verified memory changed ROUTING but not what the worker KNEW — a `cheap`
node still received only the original prompt and re-solved the already-verified
problem. Thesis "never re-solve a solved problem" now holds at the knowledge layer.

### Added

- **Verified-artifact evidence injection**: a prompt node with a live-verified
  memory hit receives a delimited `VERIFIED PRIOR CONTEXT` block (id + summary +
  content) appended to its prompt before the adapter runs, so the (cheap) worker
  extends a known solution instead of re-deriving it. The block is OPTIONAL,
  clearly labeled evidence (not auto-applied prior output), respecting the
  anchoring/injection risk flagged in the review.
- **Trust-boundary-preserving**: only store-`Local` artifacts are injected;
  imported (non-local) artifacts remain routing signals only — same guard as
  `verify.Run`. `buildEvidenceBlock` skips non-local content.
- Regression tests: `TestEngineEvidenceInjectedOnMemoryHit` (end-to-end, adapter
  probe captures the prompt) and `TestBuildEvidenceBlockSkipsNonLocal` (trust
  boundary).
- `.spec/evidence-injection.md` records the evaluation and the deliberately
  rejected parts (blind auto-apply, token-% budgeting, NL `context query`).

## [v0.9.2] — 2026-08-29 — UX gaps from real use (doc-claims + two fixes)

Feedback from running v0.9.1 surfaced three gaps; all three closed without new
verifier logic.

### Added

- **`ward doc assert <path> <pattern>`** — registers a documentation/spec/architecture
  claim as a `doc`-kind, store-local, grep artifact (`pattern::path`) and verifies
  it live (exit 1 on failure, so it is scriptable). This is the missing piece for
  catching stale prose: docs become first-class verifiable artifacts, so a README
  whose attack surface drifts from code is caught by `ward verify` / `ward tick` /
  `ward brief` drift — exactly like any other verification. `ward doc verify`
  re-runs every doc claim.

### Fixed

- **`task run <id>` auto-claims** — passing `--by` now claims an unclaimed task
  instead of erroring "pull it first" (TakeTask still rejects another agent's
  claim). One command, not run-then-next.
- **`ward brief` points at the plan when the pool is empty** — if no open tasks
  but `PLAN.md`/`SPEC.md` exist in cwd, the next-action step suggests reviewing
  the plan (the plan is the work source, not the task pool).

### Rejected (intentionally)

- A speculative `ward lint docs` that heuristically mines spec↔code invariants:
  unverifiable pattern-matching. The explicit doc-claim is the honest,
  agent-controlled primitive.

## [v0.9.1] — 2026-08-29 — context management (mechanical reload + mid-task checkpoint)

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
