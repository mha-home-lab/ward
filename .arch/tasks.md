# WARD — v1 Task Breakdown

| | |
|---|---|
| Status | Draft (v1 planning) |
| Domain | tasks |
| Version | 0.1.0 |

## Scope (frozen v1)

The complete v1 build. Anything not listed here is **v2 or later** (see
`blueprint.md` non-goals: native LLM provider SDKs, vector search, multi-user
auth, MCP server, export/import, workspaces/git-worktree, TUI, TTL auto-
supersede, claim locking, auto-resolution of incomplete work).

This file sequences the build so that the three design decisions which block
schema/code are **forced to a default in Phase 0** before any dependent spec is
implemented, rather than discovered mid-build.

## Spec index

| Spec | Title | Phase introduced |
|---|---|---|
| `storage.md` | Unified SQLite schema | P1 |
| `memory.md` | Artifact lifecycle & retrieval | P2 |
| `orchestration.md` | DAG, approvals, resume | P3 |
| `verification.md` | Claims vs real repo state | P4 |
| `routing.md` | Classifier / model-tier router | P5 |
| `cli.md` | Cobra command tree | P7 (incremental from P1) |
| `flow.md` | End-to-end sequence | — (reference) |
| `blueprint.md` | Architecture blueprint | — (reference) |

## Phase 0 — decisions that block schema/code

Each record: **decided / why / forecloses / revisit-when**. "Leave open" is not
permitted here.

### D0.1 — Contention / touched-file detection mechanism

- **Decided:** adoption of **agent-declared `touched` sets** as the v1 source of
  truth for which files a node reads/writes. The executing agent/skill emits a
  `touched` list (files + read/write intent) in its output `work_item` payload;
  orchestration persists it onto the node's run row (see D0.2). The contention
  score (branching factor + overlap of `touched` sets across parallel/active
  nodes) is computed from these declared sets, never from the filesystem.
- **Why:** agrees with the explicit lean — the other two options are non-starters
  for v1. (a) *Static parsing of arbitrary skill scripts* is unbounded scope:
  scripts can shell out, `find`, `xargs`, build ephemeral files; no parser is
  honest. (b) *git-diff-based detection* only knows contention **after** the node
  already ran, which is too late to route the node's own tier or to raise
  ceremony *before* the contested write happens. Agent-declared `touched` is the
  only input available at routing time and is honest about what the agent intends
  to touch.
- **Forecloses:** automatic/script-based or git-based contention detection in v1.
- **Revisit-when:** if agents reliably fail to emit accurate `touched` sets (data
  shows chronic under-declaration causing unsafe `light` ceremony), add git-diff
  as a *post-hoc corroboration* of the declared set — but only to **flag**
  mismatch, never as the primary routing input (it remains too late to route the
  node itself). This is a P6/P7 hardening item, not a Phase 0 reopen.

### D0.2 — Contention persistence / `node_state`

- **Decided:** contention scoring inputs **are persisted**, via two concrete
  homes, and the "in-memory only / not a separate table" wording in
  `storage.md` is superseded:
  1. **Per-node execution state lives in a `run_nodes` table** (one row per DAG
     node per run) carrying `node`, `run_id`, `status`, `touched` (JSON), and
     `ceremony_level`. This aligns with `blueprint.md`'s `run_nodes` and replaces
     the whole-run `runs.current_node` + `pending_nodes`/`completed_nodes` JSON
     slices from `storage.md`'s sketch. Per-node rows are what make fan-out width
     and shared-state overlap *queryable* for the router (routing.md signal #3),
     which the JSON-slice design cannot do efficiently.
  2. **The router's decision inputs are persisted** in `routing_decisions`,
     extended with a `contention_inputs` JSON column (`branching_factor`,
     `shared_overlap`, `touched_files`). This keeps routing.md's audit/learning
     goal ("was this run's ceremony level justified in hindsight?") achievable
     without a third normalized `node_state` table.
- **Why:** routing.md's audit log is useless if the *inputs* to the decision
  evaporate; storage.md's in-memory-only approach makes hindsight analysis
  impossible. A per-node `run_nodes` row + a JSON inputs blob on the decision is
  the minimal persistence that satisfies both. No dedicated `node_state` table is
  introduced (storage.md's instinct stands — it's just materialized per node
  instead of computed-then-discarded).
- **Forecloses:** a separate normalized `node_state` table; whole-run blob
  execution state.
- **Revisit-when:** if a future learned router needs the *historical DAG topology*
  (not just the scored inputs) to retrain, promote `contention_inputs` into a
  normalized `node_graphs` table. Out of v1 scope.

### D0.3 — Verify sandboxing / trust boundary

- **Decided:** `verify_cmd` (and the structured `grep`/`build`/`test`/`hash`
  checks in verification.md) **executes only for artifacts authored in the
  current local session/repo** — i.e. artifacts whose provenance is local
  (`source_session`/`created_by` matches the local agent/session, or the artifact
  was created by `put`/`propose` in this store). Artifacts that are *imported*,
  *synced*, or otherwise lack local provenance are **never auto-executed**; they
  are stamped `verify_status = unknown` and must be explicitly trusted (a human
  runs `ward verify --trust <id>`, or the artifact is re-authored locally) before
  their check runs. In `full` ceremony mode (multi-agent), an artifact promoted by
  agent A is still "locally authored" relative to this store, so agent B's
  `resume` *may* execute it — the boundary is store-local, not per-agent.
- **Why:** verification.md correctly flags this as a code-execution trust
  boundary, not hardening. Once artifacts are shared across agents/sessions, an
  untrusted `verify_cmd` is arbitrary shell execution on every `resume`. The
  conservative, implementable default is "execute only what this store itself
  authored"; cross-store/imported artifacts are guilty-until-trusted.
- **Forecloses:** auto-executing imported/synced artifacts' verify commands in v1.
- **Revisit-when:** if a signing/allowlist mechanism lands (signed artifacts from
  trusted authors), the boundary can widen to "signed + allowlisted" without
  per-artifact human trust. Out of v1 scope.

### D0.4 — Vocabulary reconciliation (judgment call, flagged open to revision)

The settled `blueprint.md` uses different names than the `.spec/*.md` files
(`run_nodes` + `verifications` + `gap` tables; verbs `know`/`gap`/`task`/`doctor`).
To unblock schema work, v1 **forces these defaults** and treats them as open to
revision (not Phase-0-must-close, but flagged per the spec convention):

- **Execution state:** use `run_nodes` (per D0.2), not `storage.md`'s whole-run
  `runs` slice design. `storage.md` §"Proposed Schema Sketch" must be updated to
  drop the `runs.pending_nodes`/`completed_nodes` columns in favor of `run_nodes`.
- **Verification storage:** keep **artifact-column** `verify_cmd`/`verify_status`/
  `verify_at` (verification.md), **not** a separate `verifications` table. The
  blueprint's `verifications` table is an alternative; flagged open.
- **Incomplete-work storage:** keep the **structured `handoff --incomplete` field**
  (memory.md / flow.md step 8), **not** a separate `gap` table. The blueprint's
  `gap` table is an alternative; flagged open.
- **CLI verbs:** adopt `cli.md`'s tree (`memory` subtree + `ward route`/`verify`/
  `tick`/`run`/`resume`/`approve`/`reject`), **not** the blueprint's `know`/`gap`/
  `task`/`doctor` verbs, for v1. Flagged open to revision.

These are defaults so P1 can start; they do not change behavior, only naming.

## Build phases

Each phase is a crisp checkpoint. Exit criteria are commands/assertions that
prove done, in the spirit of chef's `tasks.md`.

### P1 — Storage foundation (`storage.md`, D0.2, D0.4)

- **Goal:** one `ward.db`, additive migrations, FTS5, id-hash dedup, and the
  tables the later phases depend on — `artifacts` (with `verify_*` + `ceremony_level`
  + `project` + `expires_at`), `run_nodes` (per D0.2), `channel_items`,
  `routing_decisions` (with `contention_inputs`), `workflows`/`agents`/`skills`.
- **Implements:** `storage.md` (whole file), `D0.2`, `D0.4`.
- **Exit criteria:**
  - `ward init` creates `.ward/ward.db` at `user_version = 1`; re-run is
    idempotent.
  - FTS5 index builds (or clean LIKE fallback if FTS5 absent) with insert/update/
    delete triggers.
  - Content-hash dedup verified: re-`put` of identical content returns the same id
    and does not reset `used_count`.
  - `run_nodes` / `channel_items` / `routing_decisions` tables exist with the
    D0.2 columns.
- **Depends on:** D0.2, D0.4.

### P2 — Memory core lifecycle (`memory.md`)

- **Goal:** chef-derived lifecycle + retrieval against `ward.db`; light/full
  ceremony recorded as a column + flag (auto-accept vs `propose`), but the
  *contention trigger* is not yet wired (defaults to `light` until P5).
- **Implements:** `memory.md` "What's kept from chef" (lifecycle, retrieval,
  project lens, `claim`, `handoff`/`resume` surface, `verify_*` columns, TTL
  *metadata*), reduced taxonomy (`fact|procedure|claim|note`), `incomplete`
  structured field.
- **Exit criteria:**
  - `propose`→`promote`→`supersede` works; `promote` idempotent on accepted,
    rejects superseded; `supersede --reason` recorded.
  - `search` (FTS5 + term-drop), `list`, `get` (bumps `used_count`, provenance),
    `context` (no full content), `stale` all function; accepted ranks above
    proposed (`~` flag).
  - `put` auto-accepts (`light` ceremony); `propose` stays `proposed`.
  - `handoff --incomplete '<json>'` stores `{summary,files,lines,why,attempted_at}`.
- **Depends on:** P1 (storage). D0.1 not yet consumed (ceremony trigger lands P5).

### P3 — Orchestration core (`orchestration.md`, D0.1)

- **Goal:** ciao DAG execution against `run_nodes`/SQLite, approval gates, channel
  I/O, crash-safe resume; **capture `touched` sets** (D0.1) into `run_nodes`.
- **Implements:** `orchestration.md` "What's kept from ciao" + "What's changed"
  (SQLite medium, router picks model, git-snapshot dropped), D0.1 capture.
- **Exit criteria:**
  - `ward run <wf>` loads/validates a DAG (Kahn: one start, acyclic), executes
    channel nodes end-to-end, halts at `approval`, prints `approval_id`.
  - `ward approve <id>` / `ward reject <id>` resume/reject; run reaches
    `completed`.
  - `ward resume <run_id>` recovers a run killed mid-advance (reload `run_nodes`,
    continue from last incomplete node).
  - Agent/skill output payload carries a `touched` list persisted to `run_nodes`.
  - **Observation only (not acted on in v1):** after each node, WARD logs the delta
    between the agent-declared `touched` set and the git-diff-observed changed-file
    set to `run_nodes`/the decision log, so D0.1's revisit condition ("chronic
    under-declaration causing unsafe `light` ceremony") can be judged on evidence
    rather than vibes.
  - Base execution treats `ceremony_level` as `light` (contention trigger wired P5).
- **Depends on:** P1 (storage, `run_nodes`), P2 (memory for result capture). D0.1
  (capture), D0.2 (`run_nodes`).

### P4 — Verification engine (`verification.md`, D0.3)

- **Goal:** make `verify` real — execute checks under the D0.3 trust boundary,
  stamp `verify_status`, queue verify-triggered supersede.
- **Implements:** `verification.md` §"What's changed" items 1–5 (routing
  precondition, structured check types, staleness, freshness TTL, scope boundary),
  D0.3.
- **Exit criteria:**
  - `ward verify` runs checks **only** for locally-authored artifacts; imported
    artifacts are stamped `unknown` and not executed.
  - A `verified`/`stale`/`error` artifact gets the correct `verify_status` +
    `verify_at`; `stale` `accepted` artifact is queued for
    `supersede --reason "stale per verify"`.
  - `verify_kind` discriminators (`shell`/`grep`/`build`/`test`/`hash`) execute as
    specified; freshness window demotes stale `verify_at` to `unknown`.
- **Depends on:** P1 (verify columns), P2 (artifacts + supersede). D0.3.

### P5 — Routing + ceremony scaling (`routing.md`, D0.1, D0.2)

- **Goal:** deterministic tiering consuming real signals; wire ceremony_level
  selection into orchestration; persist `routing_decisions` with `contention_inputs`;
  enforce retry budget.
- **Implements:** `routing.md` Signals 1–4, Tiering policy, Escalation rules,
  Output contract; D0.1 (consume `touched` overlap), D0.2 (persist inputs),
  routing.md "Retry budget" cap.
- **Exit criteria:**
  - `ward route <node>` prints `{tier, model, ceremony_level}` + the four signals
    used (memory hit, verify_status, contention, task type).
  - A `ward run` actually selects the model per node via the router (not hard-coded).
  - Under high contention (parallel `touched` overlap), `ceremony_level` flips to
    `full` and the router escalates the tier.
  - Cheap-failure escalation is capped (max 2 escalations/unit); `strong` failure
    terminates the unit (run `rejected` / route-to-human), no infinite loop.
  - `routing_decisions` rows record `contention_inputs` for hindsight audit.
- **Implemented (v1 slice):**
  - `internal/routing` is a pure function (no LLM call); `ward route` shows signals + tier.
  - **Verify is a live gate, not a stored column (thesis fix 2026-08-22):** the
    engine runs `verification.Run` against the repo *before* every `Route` call
    and persists the result; only `status=="verified"` counts as a memory hit.
    `ward router --seed` goes through this path (greps README.md for "OIDC");
    `ward router --seed-stale` greps a pattern that cannot match, so cheap does
    NOT fire. Covered by `engine_test.go` (passing vs failing live grep).
  - `ward run` selects per node via the router; nodes with a `run:` shell command
    are **executed** by the engine (real adapter). `ward router
    --workflow workflows/parallel-demo.yaml --auto-approve` shows two unordered
    siblings sharing a file → `build-b` escalates to `strong`/`full` (contention
    between *unordered* nodes only — the D0.1 false-positive trap).
  - Escalation budget (max 2) → `REJECT`. `routing_decisions` records
    `contention` + `contention_inputs` JSON for hindsight audit.
  - `memory put` light ceremony auto-accepts; `memory get`/`supersede`, `verify
    --all`/`--trust`, structured `handoff --incomplete`, and `tick` (drift sweep)
    implemented. `go.mod` deps direct; `PRAGMA journal_mode=WAL` on open.
  - Table tests: `routing_test.go` (Route truth table), `workflow_test.go`
    (Validate: cycle / two roots / approval-without-channel), `verify_test.go`
    (local vs imported, hash drift), `engine_test.go` (live verify-on-read,
    contention on unordered siblings).
- **Depends on:** P2 (memory + verify_status real), P3 (`touched` sets + run_nodes),
  P4 (`verify_status` must be real before routing trusts it). D0.1, D0.2.

### P6 — Coordination: handoff/incomplete + claims (`memory.md`, `flow.md` step 8)

- **Goal:** structured incomplete surfaced on `resume`; `claim` reservations +
  conflict detection engaged under `full` ceremony.
- **Implements:** `memory.md` `claim` semantics (enforced at shared-state nodes under
  `full`), `resume` surfacing `verify_status` flags + `incomplete`; `flow.md` step 8.
- **Exit criteria:**
  - `ward resume` shows `incomplete` prominently (files/lines/why) and cached
    `verify_status` ✓/✗/⚠ on context artifacts.
  - Under `full` ceremony, a `claim` on a topic with active overlap is rejected/
    warned before the contested write; advisory otherwise.
- **Depends on:** P2 (handoff/incomplete/claim), P4 (verify flags on resume), P5
  (ceremony_level → claim enforcement). D0.1 (shared-state nodes).

### P7 — CLI finalization + acceptance (`cli.md`)

- **Goal:** finalize the `cli.md` tree, global `--json`/`--digest`/error hygiene,
  and a session-A→B acceptance script locking the success criterion (flow.md:
  memory surfaces prior knowledge, verify re-anchors it, routing trusts only
  verified).
- **Implements:** `cli.md` (command tree, flag conventions), D0.4 verb set.
- **Exit criteria:**
  - Every command in `cli.md`'s tree exists; global `--json` parseable; one-line
    errors (`no artifact <id>`, `error: <detail>`).
  - A `ward-tasks-verify` script (chef `verification-001.sh` analogue) passes:
    session A stores knowledge → session B retrieves it without replay; a stale
    claim is caught by `verify` and **not** trusted by `route`; crash recovers via
    `resume`.
- **Depends on:** P1–P6. D0.4.

## Done definition

A phase is done when:
1. Its exit-criteria commands assert true / exit 0.
2. The spec it implements is phase-labeled and matches the build.
3. No earlier phase regresses (re-run P1–P4 checks after P5–P7).

## References

- `blueprint.md` — component map + v1 non-goals (TTL auto-supersede deferred).
- `flow.md` — end-to-end sequence; verify-gap closure at step 3; ceremony at step 6.
- `storage.md`, `memory.md`, `orchestration.md`, `routing.md`, `verification.md`,
  `cli.md` — the per-domain contracts implemented by P1–P7.
- chef `.specs/tasks.md` — granularity/format convention followed here.
