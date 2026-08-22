# memory — Artifact Lifecycle & Retrieval (chef-derived)

| | |
|---|---|
| Status | Implemented (v0.5: brief session bootstrap) |
| Domain | memory |
| Version | 0.5.0 |

## Purpose

WARD's persistent, retrievable state so a new session/run can continue without
replaying prior conversations. The chef lifecycle (`propose → promote →
supersede`), retrieval, provenance, and conflict detection — kept, but with a
**reduced taxonomy** and **ceremony that scales with concurrency** to fix the
qwen-auth feedback (over-engineered for small/solo work).

## What's kept from chef

- **One primitive: the artifact** with `id`, `kind`, `summary`, `content`,
  `tags`, `status`.
- Lifecycle: `propose` (status `proposed`) → `promote` (`accepted`, idempotent
  on already-accepted, rejects superseded) → `supersede` (optional successor,
  recorded reason).
- **Dedup** via content-hash id; `used_count`/`last_used` reuse tracking.
- **Retrieval**: FTS5 hybrid search with term-drop relaxation, status-ranked
  (accepted before proposed, `~` flag), `context` compact builder, `zig-zag`
  assembly, `--digest` cache-stability, `list`/`stale`/`overview`.
- **Project lenses** (`--project` / `WARD_PROJECT`): filter on search/list,
  overlay on context.
- **`claim`** topic reservation with TTL + conflict detection (coordination-001).
- **`handoff` + `resume`**: structured session-end report + one-command resume;
  `resume` surfaces handoff, pending, stale, context, claims.
- **`verify_cmd`/`verify_status`/`verify_at`** columns (see verification.md).
- **`incomplete`** structured field on handoff (session-protocol-003) — file
  paths + line numbers + why.
- **TTL/expiry *metadata***: the `expires_at` column and surfacing (e.g. in
  `activity`) are kept from chef, but automatic retirement-on-expiry is **deferred
  to post-v1** (see verification.md scope boundary). v1 does not silently
  supersede artifacts on a timer; only verify-triggered staleness queues a
  supersede.

## What's changed and why

- **Reduced artifact taxonomy.** chef's 8 kinds (`fact, decision, plan,
  procedure, failure, discovery, memory, script`) are over-engineered for small
  tasks (qwen-auth: "~5 facts worth remembering"). WARD collapses to a small
  optional set, e.g. `fact | procedure | claim | note` (claim is chef's
  `plan`+`claim:` convention; `note` covers memory/handoff). Kinds remain a
  column but are no longer a forced 8-step machinery; an artifact with no kind
  defaults to `note`.
- **Ceremony scales with concurrency (flow.md step 6).** Two paths:
  - *light* (solo/sequential): writes are **auto-accepted** (chef `put`
    semantics) — no promote step; progress tracked via `ward tick`; verify is
    optional but recommended. `ceremony_level = 'light'` recorded on the
    artifact.
  - *full* (concurrent / shared-state contention): `propose → promote →
    supersede` is required; `claim` reservations + conflict detection mandatory
    before touching shared state; approval gates enforced. `ceremony_level =
    'full'`.
  The engine picks the path from the DAG contention score (orchestration.md),
  not a constant.
- **Verify is a routing precondition, not just display.** A `stale`/`error`/
  `unknown` artifact contributes no "known pattern" signal to the router; a
  previously-`accepted` artifact found `stale` is queued for
  `supersede --reason "stale per verify"`. (verification.md owns the mechanics.)
- **`supersede` reason vocabulary grows** to include `stale per verify` and
  `superseded by newer run` (ciao result capture), so retirement is auditable.
  v0.5 adds `drift` (tick --heal; verification.md item 7).

## Session bootstrap: `ward brief` (v0.5)

One command at session start, replacing the old "run tick, then context" two-step:

1. Runs the tick sweep live (re-verify local artifacts, free expired claims).
2. Reports prior knowledge for an optional topic — compact pointers only
   (id/kind/summary/tags/verify_status), never full content.
3. Surfaces open runs (`running` / `awaiting_approval`), active claims, the
   dispatch pool (open tasks with floors — broker.md §4), and store health
   (accepted/verified/proposed counts).
4. Ends with imperative next actions (`ward run approve <id> <node>`,
   `ward task next --by <name> --max-tier <budget>`, "reuse verified context",
   "topic X is claimed by Y", or "store clean: proceed").

`--json` emits the full structured report. Brief is the machine-readable answer
to "where do I pick up?" without replaying history — the injection point every
future agent session starts from.

## Retrieval rules (carried from chef cli-001)

- `search`/`list`/`context` never print full content; `get` is the only full-
  content command and bumps `used_count`.
- Superseded hidden by default; `list --status` exposes any status.
- Deterministic, parseable, `--json` global, one-line errors.

## Open questions / risks

- **Exact kind set for v1.** Proposal: `{fact, procedure, claim, note}`. Should
  `failure` and `discovery` survive as first-class (chef found them valuable for
  "why" capture)? *Lean: fold `failure`/`discovery` into `note` with a `tags`
  discriminator, keep schema flexible.* Defer final call.
- **Auto-accept vs. always-verify in light mode.** If solo work auto-accepts,
  do we still run `verify_cmd`? Proposal: run verify lazily on `resume`/before
  routing, not on every write. Risk: light-mode memory can still go stale; the
  verify gap reopens unless `resume` re-anchors. Mitigation: verify-on-read for
  any artifact the router will consume.
- **`claim` semantics across concurrent runs.** chef claims are advisory
  (agents voluntarily check). Under `full` ceremony should a claim be *enforced*
  (block overlapping writes)? Proposal: enforce only at shared-state nodes;
  advisory otherwise. Open.
- **Handoff `incomplete` structure.** Schema: `{summary, files[], lines[],
  why, attempted_at}`. Should `why` be free text or enum? Proposal: free text
  (short); structured enough to surface, not so rigid it's unused.
