# Changelog

All notable changes to WARD. History and session detail live in
`.arch/tasks.md`; this file is the distilled release view.

## [v0.10.0] — 2026-09-02 — autonomous porting (density scoring, put dry-run, put/verify reconcile)

Field session porting knowledge to the global vault taught three frictions that
turned "capture -> pack -> sync" into a rewrite loop. This release removes each
so the happy path is one clean pass:

- **The lint was vocabulary-blind.** `Score()`'s generalization trigger was a
  preset word list and the slug-repeat penalty only fired when no preset word
  was present — so a dense, concrete, transferable mechanism that unavoidably
  repeats domain nouns (`config`, `key`, `realm`, `crds`) was expelled as
  "instance-specific", and the graver rewarded puffy prose over mechanism
  density. Transferability now counts **structural density**: a body with ≥ 6
  distinct non-stopword terms and no verbatim/path/argv signal reads as a
  mechanism, contributing to the generalization side and suppressing the
  slug-repeat penalty. The field fixtures (collatz/bowling) still score as
  cheat-sheets; a dense body that ALSO copies exact output still scores as one.
- **No fast feedback at the write.** The score only surfaced deep in
  pack/lint output. `ward memory put --dry-run` now prints the transferability
  score + fired signals + a PASS/FAIL verdict for a `portable:*` capture
  BEFORE writing anything — write it once, correctly, no supersede loop.
- **`put` and `verify` disagreed on "declared".** `put --verify-cmd` stored
  `VerifyCmd` but left `VerifyKind` empty, so `ward verify` reported "no
  verify_cmd declared" for artifacts that provably had one. `put` now defaults
  an unset `--verify-kind` to `shell` when `--verify-cmd` is given, so the
  command is actually runnable by verify/tick (still gated on the D0.3 `Local`
  trust boundary; `put` never executes it).

### Added

- `internal/transferability` `Score()`: `densityFloor` / `hasDensity` /
  `distinctTokenCount` — structural-density generalization signal.
- `internal/cli` `previewTransferability()` + `memory put --dry-run` — write-once
  score preview resolving the first `portable:` tag exactly like the pack gate.
- `memory put` verify-kind default to `shell` when verify-cmd is set without a
  kind — reconciles the put/verify contract.

### Fixed

- Dense, concrete, transferable mechanism bodies are no longer expelled for
  repeating legitimate domain nouns.
- `ward verify` no longer reports "no verify_cmd declared" for an artifact whose
  `put` actually declared one.

## [v0.9.15] — 2026-09-01 — agent-declared recurrence links for portable knowledge

The field report asked for a stronger promotion signal than "this topic has
portable artifacts": it wanted to know when a lesson has genuinely *recurred* —
the same underlying trap surfaced independently in different wording. Semantic
recurrence detection is fragile and misattributes transferability to ward's
opinion of text. So ward does not detect recurrence either: an agent asserts a
recurrence link with `--recurs <id>` on `ward memory put` / `ward capture`,
mirroring how `--verify-cmd` and supersede let the agent declare ground truth
ward never judges. The count of agent-declared confirmations is then a
deterministic, honest promotion signal.

### Added

- **New `recurrences` table (schema v12)** — records one row per
  agent-declared link: `of_id` (the earlier lesson being confirmed),
  `from_id` (the new capture that recognized it), optional `note`, and `at`.
  Many-to-one (several captures may confirm one original), deliberately
  distinct from `superseded_by` (1:1, "this replaces that"). `Store.RecordRecurrence`
  validates both ids exist and rejects self-links; `Store.RecurrenceCount`
  returns how many later captures confirm an artifact.
- **`--recurs <id>` flag** on both `ward memory put` and `ward capture` —
  after the new artifact is written, ward records that it confirms the given
  existing lesson. Ward never decides the link; the agent does.
- **`recurrence_count` in `ward memory get --json`** — a derived view (how
  many later captures confirm this artifact), never a persisted column.
- **`brief` strong-promotion nudge** — an unsynced portable topic whose lesson
  is independently confirmed `>= 2` times (agent-declared links) is listed as a
  "strong promotion candidate(s)" and worth pushing to the global vault.
- **Assistive recurrence autocomplete (non-blocking)** — when a capture does
  NOT supply `--recurs`, ward prints a stderr hint if the new content shares
  `>= 3` distinctive tokens with an existing artifact under the same portable
  topic. It never links anything itself (autocomplete, not detection).

### Notes

- A missed recurrence is a silent miss, never a false gate: the signal only
  moves when an agent explicitly links. This keeps the promotion claim honest
  — it rests on declared confirmations, never on ward's judgment of text.

## [v0.9.14] — 2026-09-01 — claim reservations no longer leak into portable chip sources

Found while wrangling the portable knowledge vault into a complete state: a
**claim reservation carries the `portable:<topic>` tag** it was claimed under,
and because `store.PortableTopics()` / `store.ArtifactsForPortableTopic()`
matched any accepted artifact carrying the `portable:` marker, the active claim
was swept into the chip source set. Each `skill-sync` then printed
`EXCLUDED claim:...: instance-specific` — the claim was bookkeeping (an
exclusive-work reservation), not knowledge, and only the transferability gate
dropped it before it compiled into a chip. Worse, a claim could crowd out real
sources in the fixed `LIMIT 50` source set.

### Fixed

- **`PortableTopics()` and `ArtifactsForPortableTopic()` now exclude
  `kind='claim'`** artifacts. A claim is a booking record, never portable
  knowledge: it neither surfaces a topic in the table of contents nor compiles
  as a chip source. The portable source set is now exactly the accepted,
  non-claim artifacts carrying the marker.

### Tests

- `TestPortableSourcesExcludeClaims`: an accepted claim tagged
  `portable:<topic>` must neither add a topic to `PortableTopics()` nor count as
  a source in `ArtifactsForPortableTopic()`.

Gate: `go build ./...`, `go test ./... -race`, `gofmt -l .` empty, `go vet`
clean.

## [v0.9.13] — 2026-09-01 — misplacement guard compares absolute paths; no false warning inside ward's own store

Follow-up surfaced while capturing portable knowledge from inside the ward repo:
registering `ward` as a project stores an **absolute** path, but the default
current-store `Home()` resolves to the **relative** `.ward`. `warnIfMisplaced`'s
`store.Home() == h` test therefore compared relative to absolute and falsely
warned "filed in the CURRENT project's store (not ward's)" for any
`ward`/`portable:`-tagged item written from inside the ward repo — prompting an
agent to wrongly re-target `--project ward` or relocate a correctly-placed item.

### Fixed

- **`warnIfMisplaced` compares absolute paths** (`filepath.Abs` of both the
  current store and the registered ward home), so writing ward/portable items in
  ward's own store is correctly recognized and produces no warning. The
  tag-detection widening to `topic:portable:` (v0.9.12) had made this false
  positive reach common put/task-add paths.

### Tests

- `TestMisplacementGuardInsideWardStore`: current store path == registered ward
  path plus a `topic:portable:` tag must not warn.

Gate: `go build ./...`, `go test ./... -race`, `gofmt -l .` empty, `go vet`
clean.

## [v0.9.12] — 2026-09-01 — portable tagging is content-based, not prefix-only; pack/sync canonical name + skill-sync nudge

Root cause found while auditing the portable knowledge pipeline: the
`portable:` marker was detected with a STRICT prefix, so the alternate
`topic:portable:<name>` spelling **silently skipped** the transferability gate,
the misplaced-item warning, and `skill-sync`'s source discovery. A lesson tagged
`topic:portable:bash` was effectively invisible to the global vault — the exact
leak the pipeline exists to prevent.

### Changed

- **`portable` is detected on tag CONTENT, not prefix**. `store.PortableTopics()`
  now returns the deduped **topic name** honoring both `portable:<n>` and
  `topic:portable:<n>` (substring `portable:` marker, never strict prefix), and a
  new `store.ArtifactsForPortableTopic(name)` resolves source artifacts under
  either spelling. A single `portableTopicName()` helper backs the
  transferability lint, the misplaced-item warning, and the store — one
  definition, both conventions.
- **Transferability gate fires on tag content**: `skill pack`'s portable check
  uses `strings.Contains(tag, "portable:")`, so a `topic:portable:*` selector
  targeting the global vault is never exempted from the lint.
- **Canonical chip naming** (`canonicalChipTopic`): `ward skill pack portable:bash`
  now writes `ward-bash/SKILL.md` (frontmatter `name: ward-bash`), matching
  `skill-sync` and `findGlobalChip` — fixing the `ward-portable-bash`
  frontmatter/directory mismatch that made a "current chip" resolve to an
  un-updated global file.
- **`ward brief` nudges `skill-sync`**: a verified portable topic with no global
  chip surfaces "N portable topic(s) … not yet pushed to the global vault — run
  ward skill-sync" at session start, closing the capture→pack→sync loop even
  when a session never calls handoff (same shape as the capture-gap nudge).

### Fixed

- `skill-sync` regressed when `PortableTopics()` changed its return contract
  (full tag → stripped name): it searched by the stripped name, matched zero
  sources, and synced **nothing** silently. It now uses
  `ArtifactsForPortableTopic`. `TestSyncSkipsCheatSheetTopic` /
  `TestSyncForceIncludesCheatSheetWithReason` / `TestSyncHonorsTopicPrefixedTag`
  lock the behavior in.

### Kept

- Posture unchanged: never a block, warn-only, transferability as judgment-saved
  for the pack/sync gates.

### Tests

- `internal/store/portable_test.go`: both tag spellings discovered, dedup by
  name, source resolution per topic.
- `TestSyncHonorsTopicPrefixedTag`: `topic:portable:` source syncs to
  `ward-bash`.
- `TestBriefNudgesSkillSyncForUnsyncedPortable`: brief surfaces the nudge.

Gate: `go build ./...`, `go test ./... -race`, `gofmt -l .` empty, `go vet`
clean; the real CLI was smoke-tested (`topic:portable:bash` → `ward-bash` sync,
brief nudge fires).

## [v0.9.10] — 2026-09-01 — close the capture loop for off-pool work

Spec `.spec/control-capture-loop.md`. Auto-capture only fired inside `ward task
run`; a free-standing agent session (a green-field batch, an exploratory run)
read from the vault but had no path back into it, so genuinely transferable
lessons found off-pool were never recorded — compounding required knowledge to
flow back in, not just out. This closes that loop: protocol guidance, a shared
lint on both capture paths, a handoff-time gap detector, and a next-session
surface.

### Added

- **Off-pool capture instruction in the agent protocol**: `agentdoc.go` step 6
  now states that work done outside `ward task run` which surfaces a new,
  generalizable lesson must be captured manually (`ward memory put --local
  --tags portable:<topic> --verify-cmd "<cmd>"`) before handoff — the one case
  hand-typing is correct, not a violation.
- **Shared transferability lint** (`internal/cli/transferlint.go`): the
  `portable:*` cheat-sheet warning moved out of `captureNode` into
  `warnIfCheatSheet(tags, summary, content)`, now fired by **both** the
  automatic capture path and the manual `memory put` path — so a manual
  capture can no longer silently bypass the warning the automatic path gives.
- **`handoff_log` capture-gap check**: `ward memory handoff` compares against
  the previous handoff (`internal/observe` `GitCommitsSince`); when commits
  happened with zero new artifacts, it warns loudly and sets
  `capture_gap_suspected: true` in `--json`. New migration v11 creates the
  `handoff_log(id, at, head_sha, capture_gap, commits)` table.
- **Loop-closer in `ward brief`**: the next session reads the flag persisted on
  the most recent `handoff_log` row and prepends a
  "previous session may have skipped capture (N commits, 0 new artifacts)"
  next-action — so even an agent that ignores its own handoff warning is
  superseded by the next session's start. The flag and commit count are
  persisted on the row so the next brief can read them.

### Kept

- On-pool auto-capture (`autoCapture`/`captureNode`) behavior is unchanged.
- Warn-never-block posture: capture still never blocks, and a session that
  genuinely learned nothing new is never forced to invent a capture.

### Fixed

- The gap check and handoff logging now propagate database errors instead of
  swallowing them: a failed read is treated as "unknown", never as "no gap",
  so a real capture miss can't be silently masked or fabricated.

## [v0.9.11] — 2026-09-01 — brief surfaces skipped captures even without a handoff; the gap counts portable lessons, not all captures

Follow-up to the v0.9.10 capture-loop: the loop-closer only surfaced a gap when
the prior session had actually run `ward memory handoff` (which is what
persisted the flag). A session that read the vault via `brief`/`skill install`
but **stopped without ever calling handoff** left no `handoff_log` row, so the
very gap the feature exists to catch stayed invisible at the next start. This
release makes `ward brief` compute the gap **live**, and sharpens what counts as
a capture so unrelated on-pool work can't mask a real off-pool discovery.

### Changed

- **`ward brief` now does a live gap check** (`detectCaptureGap`, read-only, no
  row written): at session start it compares commits since the **last logged
  handoff** against new portable captures, so a dropped session that never
  called `memory handoff` is still caught — its commits remain visible relative
  to the prior handoff's sha. The persisted flag is kept as a fallback for the
  normal handoff path (whose own handoff closes the live interval at that
  session's sha), so brief reports a gap when **live OR persisted**, using
  persisted counts when the live interval is clean.
- **Gap gates on `portable:*` captures, not all artifacts**:
  `store.CountArtifactsSince` returns both a total and a `portable:*`-tagged
  subcount, and `detectCaptureGap` flags a gap only when `commits > 0 &&
  portable == 0`. An on-pool auto-capture (node-tag, not portable) can no longer
  clear the off-pool discovery gap.
- **Robust repo-root resolution**: the gap check now resolves the repository root
  via `git rev-parse --show-toplevel` from the cwd (`observe.GitRepoRoot`), so it
  works from any subdirectory and doesn't depend on `WARD_HOME` pointing at a
  `.ward` under the repo (previously `filepath.Dir(s.Home)` mis-resolved when
  WARD_HOME was an absolute path elsewhere).

### Fixed

- The loop-closer missed sessions that read the vault but never handed off — the
  exact "agent 3 stopped and reported to a human without handing off" scenario
  from the spec. `TestBriefLiveGapWithoutHandoff` locks it in.

### Kept

- Posture unchanged: never a block, count-never-judgment, warn-only.
- The normal handoff path still surfaces its own gap before the next brief.

## [v0.9.9] — 2026-08-30 — hang-proof live sweep: verify commands are bounded and brief is never silent

`ward brief`'s opening live sweep re-executes every store-local artifact's
verify_cmd with **no timeout**, and `brief` prints nothing until it finishes —
so a single hung gate (`go test ./...`, `docker compose up -d`, a stuck daemon)
made the session bootstrap look frozen for minutes with zero output.

### Changed

- **Verify commands are now deadline-bounded**: `internal/verification` runs
  `shell`/`grep`/`golden` gates via `exec.CommandContext` with a configurable
  timeout (`WARD_VERIFY_TIMEOUT`, a Go duration; default 180s). A gate that
  outlives the deadline is SIGKILLed — along with its whole process group
  (darwin/linux), so no orphaned build/test survives — and the artifact is
  reported as an **error** (or **stale** if previously verified), so the sweep
  always terminates and drift is never silently trusted.
- **`ward brief` is never silent**: a `sweeping: live re-verification…` progress
  line is printed before the sweep (human mode; kept out of `--json` so the
  output stays a single object), so a slow-but-legit verify reads as progress,
  not a frozen command.

### Fixed

- Revolves around the observable failure described in the session: `ward brief`
  hung >60s with no output from any project store whose local artifacts carry
  real build/test gates.

## [v0.9.8] — 2026-08-30 — control-transferability lint: instance-specific cheat-sheets never reach the global vault

Spec `.spec/control-transferability-lint.md` — a deterministic, regex-only
scorer (no model call in the scoring path) that tells a **generalized mechanism
plus why** apart from a **cheat-sheet** (verbatim error/output text, per-exercise
file paths, bare exercise slugs), so portable knowledge carries reasoning, not
instance trivia.

### Added

- **`internal/transferability`** — pure `Score(topic, summary, content)` with a
  `LintResult{Score, CheatSheet, Signals}` shape. `Score = min(gen, 3) −
  min(cheat, 5)`; `Score <= 0 → CheatSheet`. Generalization words
  (`idiom`/`the pattern`/`in general`/`any time`/`whenever`/`because`/`the trap
  is`/`the mechanism`/`the lesson`) add +1 each (capped at +3); cheat-sheet
  signals (verbatim `prints?/outputs?/returns?` quotes, path tokens, `argv[`,
  bare repeated exercise slugs) cost −1 each. Requirement-hard fixture cases
  covered by unit tests.
- **`ward skill lint <chip>`** — resolves a chip's backing sources, re-scores
  the CURRENT artifact content, prints a portable/borderline/cheat-sheet
  scorecard, and exits non-zero when any source is cheat-sheet-scored.
- **Capture warning** — a `portable:*` capture that scores as a cheat-sheet
  prints a non-fatal stderr notice. Local captures are never linted (they are
  legitimately instance-specific).
- **`pack --force --reason`** — auditable escape hatch: records `--reason` on
  the artifact (`override_reason` column, migration v10) so the exception is
  traceable, not silent.

### Changed

- **`ward pack` into the global skills dir / portable bundle** now hard-gates on
  the lint: cheat-sheet sources are excluded from the bundle, and a bundle whose
  sources all score as cheat-sheets is vetoed (never a silent empty cache).
- **`ward skill-sync` is now a hard-gate point** (subagent reviewer finding): it
  previously compiled every accepted `portable:*` source straight into the
  global vault without scoring — the exact leak the lint exists to close. It now
  scores each topic's sources, excludes cheat-sheets from every chip, and skips
  a topic entirely when none survive. `--force --reason` gives the same logged
  escape hatch as pack.
- **Accurate override reporting** (subagent reviewer finding): force-included
  cheat-sheet sources are reported as `FORCED … synced anyway (reason: …)` /
  `force_included_with_reason`, no longer falsely labeled "not synced to the
  global vault".

### Legacy

- **AGENTS.md guidance** — portable chips carry reasoning a teammate can trust
  across projects, not a per-repo answer.

## [v0.9.7] — 2026-08-30 — review phase behind subagents: wave mal-verification, engine read-path provenance, KPI honesty, skill gate coherence

The v0.9.5/v0.9.6 fixes were self-reviewed. Per protocol §9 the review phase now
runs in **fresh-context subagents** (`Review:` trailer names the reviewer, no
more self-review). Two independent reviewers on the control-plane surface —
one on the CLI/verification layer, one on the store/engine data plane — found
four real bugs and two hardening gaps, all fixed here:

### Fixed

- **`ward wave` healed PROPOSED candidates** (reviewer finding): the sweep
  selected by `SearchArtifactsTagged`, which excludes only `superseded`. A
  proposed (review-pending) local artifact whose gate went red was live-verified
  and, under `--heal`, **superseded — pulled out of review before review
  happened**. Wave now acts only on the accepted knowledge surface (`!a.Local &&
  a.Status == "accepted"`, exactly tick's `sweepVerify` guard); `checked` counts
  only artifacts actually live-verified.
- **Engine read path destroyed imported provenance** (reviewer finding): the
  memory-hit loop in `memoryHitForNode` live-verified every accepted candidate
  and stamped `SetVerify(a.ID, res.Status)`. For imports `verification.Run`
  returns `"unknown"` *without executing* — the READ path overwrote an import's
  stored `verified`, i.e. losing the trusted verdict of an artifact it can never
  re-check (trust-boundary mutation on a pure lookup). The loop now skips
  `!a.Local`; read paths never write imports.
- **Cheap-hit KPI inflated by no-op nodes** (reviewer finding): the engine
  stamped `execution_success=1` on *every* `done`, including pure passthrough
  (no run, no prompt) channel nodes — a workflow that did nothing counted as a
  cheap success. Success is now stamped only when a node actually executed a
  check or a prompt (`node.Run != "" || node.Prompt != ""`). NULL stays the
  honest marker for "no executed check": approval-gate pauses, passthroughs,
  abandoned runs — documented on `SetRoutingSuccess`.
- **`skill install` gate/verdict divergence on reinstall** (reviewer finding):
  `UpsertArtifact`'s id is `(kind,summary,content)`-derived with `INSERT OR
  IGNORE`, so reinstalling the same chip reused the row and kept the OLD
  `verify_cmd` while the status reflected the NEW gate — gate and verdict
  diverged. Install now persists the live gate (`SetVerifyCmd`) so the stored
  gate always equals the gate that produced the stored status, and it enforces
  the phantom-gate rejection (`true`/`false`/`:`) that `task add` already had.
- **Wave/skill SetVerify errors were discarded** (reviewer finding): they now
  fail loudly instead of silently continuing with a stale status.
- **v9 migration untested on migrated DBs** (reviewer finding): `TestMigrationFromV1`
  exercised only fresh-DB columns; the `execution_success` column added by the v9
  migration now gets written on a migrated v1 DB, proving `ward kpis` works
  post-migration.
- **Test race** found by the `-race` gate (concurrent Go protocol §4): the
  `captureStderr` helper wrote a pipe into a `bytes.Buffer` while the test read
  it — the buffer is now mutex-guarded per chunk. `go test ./... -race` is
  green end to end.

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
