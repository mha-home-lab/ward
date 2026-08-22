# broker — parallel agent dispatch primitives (v0.4)

| | |
|---|---|
| Status | Implemented (v0.4 primitives, v0.5 dispatch pool) |
| Domain | broker |
| Version | 0.5.0 |

## Purpose

WARD v0.3.0 is a usable single-process tool. v0.4 makes the work *dispatchable
across multiple `ward` processes* (parallel agents) that each advertise a
"thinking budget" (a max tier they can serve). Two gaps block that, both found by
code review, not speculation:

1. **Claim acquisition races.** `claim add` does check-then-insert: `activeClaims()`
   SELECTs, then a separate INSERT follows. `SetMaxOpenConns(1)` serializes
   *within one process*, which is why the existing tests pass — but a broker means
   several `ward` binaries sharing one `ward.db`, and the per-process connection
   limit is **not** a cross-process lock. Two agents can both observe "free" and
   both insert.
2. **No declarative node tier.** The router *infers* a tier at route time from
   memory/verify/contention, but a spec author cannot say "this node needs
   `strong`" up front. That declared tier is the actual admission key parallel
   agents match their budget against.

This spec covers only the two primitives everything else depends on: **atomic
claim acquisition** and the **`tier:` field as a routing floor**. The pickup/poll
loop, agent budget registration, and the actual broker service are explicitly out
of scope (see non-goals).

## What's kept

- The claim as an **exclusive reservation** (a lock): the unique index on
  `(claim_topic, project)` enforces "at most one active claim per (topic,
  project)" in the database. A conflict is a hard error (non-zero exit), never a
  warning that proceeds — two `ward` processes cannot both hold the same topic.
- The tier abstraction from `routing.md`: `cheap`/`mid`/`strong`, provider-agnostic,
  mapped to a concrete model only in `adapter.TierModel`.

## What's changed and why

### 1. Claim atomicity — the database enforces the invariant

**Mechanism (lean, stated for revision):** add a `claim_topic` column to
`artifacts` and a **unique index on `(claim_topic, project)`**. For a claim,
`claim_topic` is set to the topic; for every other artifact it is `NULL`. SQLite
treats `NULL`s as distinct, so the index constrains only *active* claims. A claim
acquire is then a **single plain `INSERT`** (not `INSERT OR IGNORE`, not a
pre-check): if an active claim already exists for `(topic, project)`, the INSERT
fails with a unique-constraint violation and the caller treats that as the
conflict — a hard error (`error: claim overlap on <topic>`, non-zero exit),
never a warning that proceeds. Release/supersede sets `claim_topic = NULL`,
freeing the slot so the topic can be re-claimed. `ward tick` also frees claims
once their TTL has elapsed, so an un-released expired claim does not block
re-claim forever.

Why this over the check-then-insert: the invariant ("at most one active claim per
`(topic, project)`") becomes a **database constraint**, correct under concurrent
separate processes, instead of application logic racing against itself. The
migration is additive (`ALTER TABLE ... ADD COLUMN` + `CREATE UNIQUE INDEX IF NOT
EXISTS`), user_version 2 → 3, idempotent like the v2 step.

`claim add` no longer does a SELECT-then-INSERT. It issues the atomic insert and
reports the result. Two processes racing on the same topic: exactly one INSERT
succeeds; the other gets the unique violation.

### 2. The `tier:` field — a FLOOR, not an override

Add an optional `tier:` to workflow nodes (`kind`-level, YAML `tier`). Semantics:

- When **present**, it is the node's **declared minimum tier** (the capability the
  work is known to require). The router treats it as a **floor**: it never routes
  *below* the declared tier.
- It is **not** an override. Verify-gating (`cheap` is forbidden on a memory
  miss / unverified artifact), contention escalation (+1 tier, `full` ceremony),
  and failure escalation (cheap→mid→strong, capped at 2) all still apply **on top
  of** the floor — a `tier: cheap` node is unaffected, a `tier: strong` node can
  still escalate only in the sense that it is already at the top.
- When **absent**, behavior is **unchanged** from v0.3.0 (pure inference). This is
  a regression requirement: every existing routing test must still pass.

Implementation: `routing.Inputs` gains `DeclaredTier`; `Route` applies
`max(chosenTier, DeclaredTier)` before computing ceremony (so a `strong` floor
also forces `full` ceremony, consistent with the existing `t == strong` rule).
`orchestration` passes `node.Tier` into the Inputs. A `strong` floor on a node
that would otherwise route `cheap` (memory hit + verified) yields `strong`.

### 3. Cross-process escalation handoff (v0.4 design → v0.5 realized)

Today `failNode()` escalates a node *within one Engine's run loop*: it bumps
`escalation`, marks the node `failed`, and the same loop re-picks it. For parallel
agents that breaks, because the retrying agent may be a different process than the
one that failed.

Design (now realized via the task pool, §4):
- Each work item is its own claimable unit — realized as a **`tasks` row**, not
  an artifact tag: the pool needs tier floors and status transitions that don't
  belong in the memory store.
- On failure, the item is **released back to open** and its required tier bumps
  by one (`Store.FailTask`: floor cheap→mid→strong, then `rejected`). The bump is
  persisted on the task row itself, so the *required tier* survives across
  processes. Past strong there is no higher tier: the task is rejected for a
  human, never looped.
- The task then re-enters the **pool for ANY eligible agent** (any agent whose
  budget ≥ the new floor), not necessarily the one that failed.

### 4. The dispatch pool (v0.5) — pickup loop + budget admission

The v0.5 slice closes what §"non-goals" deferred. Store-native, no service:

- **Producer:** `ward task add "<title>" [--tier FLOOR] [--kind K] [--run CMD]
  [--verify-cmd CMD]` creates an `open` task. No YAML authoring; flags over NLP
  inference (a sentence is a title, not a parsed spec).
- **Consumer:** `ward task next --by <agent> --max-tier BUDGET` pulls the
  highest-floor open task whose `tier_rank <= budget_rank`. Admission is the
  agent-side of the match (wish 02): a cheap-budget agent never receives a mid or
  strong item; among admissible tasks, highest floor wins (hardest eligible work
  first).
- **Atomicity:** `ClaimNextTask` SELECTs candidate ids ordered by floor DESC,
  then issues a **conditional UPDATE** (`WHERE id=? AND status='open'`) per
  candidate; `RowsAffected == 1` wins. Two concurrent processes can never both
  win the same task — the status transition is the arbiter, same spirit as the
  unique-index claim lock (a lost race moves to the next candidate).
- **Lifecycle:** `ward task done <id>` closes claimed work;
  `ward task fail <id>` releases it one tier higher (§3); `ward task list
  [--status]` inspects the pool.
- **Execution bridge:** `ward task workflow <id>` generates a runnable
  single-node DAG (`start → work → done`, orchestration.md TaskWorkflow) with
  the task's `run:` command, records it on the task, and prints the run command.
  Agents compose: next → workflow → `ward run start`.

Guardrail held: advisory-of-work, exclusive-of-topic. The pool never assigns
work; agents pull, and the atomic pull guarantees one owner.

## Output contract

- `artifacts.claim_topic` + `uni_claim_topic` index: the atomicity invariant.
- `routing_decisions` rows now reflect the floor when a node declares `tier:`.
- `run_nodes.escalation` doubles as the cross-process required-tier signal.
- `tasks` table (storage.md v4): the dispatch pool — floor, rank, status,
  claimed_by/at, escalation, generated workflow path.

## Open questions / risks

- **Agent registry table vs `--max-tier` flag (v0.5 decision).** Budgets are
  currently declared at pull time (`--max-tier`), not persisted in the idle
  `agents` table. A persistent registry pays off only when many agents poll
  repeatedly under one identity; revisit if fleet usage materializes.

- **Legacy claims (operationally resolved, recorded here).** Claims created
  before this migration have `claim_topic = NULL`, so the unique index does
  **not** enforce them and `activeClaims` (which filters `claim_topic IS NOT
  NULL`) does not list them. Operational meaning, made explicit: a pre-v0.4
  claim is **silently non-enforceable** — it will not block a new `claim add`
  on the same topic, and it will not be reported as an active claim. This is a
  **one-time transition gap**, not a recurrent one: every claim created *after*
  the migration carries `claim_topic` and is fully enforced. It is made
  *visible* (not silent) via `ward doctor`, which reports `legacy_claims` = the
  count of accepted `kind=claim` rows with `claim_topic IS NULL`. Backfilling
  old claims (stamping `claim_topic` from their `tags`) is deliberately left
  undone — they predate the broker and the operator can see and clear them.
- **Expiry vs. the index.** The unique index is static; an expired claim keeps
  `claim_topic` set until something clears it. Release clears it explicitly; an
  un-released expired claim would still block re-claim. **RESOLVED (v0.4.x):**
  the tick sweep (`Store.SweepExpiredClaims`) nulls `claim_topic` on expired
  claims so the slot reopens; covered by `TestClaimExpiredThenTickFrees`.
- ~~**Pickup/poll loop + agent budget registration** are the next piece (v0.5+),
  built directly on these two primitives. Deliberately not in this spec.~~
  **DELIVERED (v0.5, §4)** as the store-native dispatch pool.
- **Topic granularity for work items.** Using `<runID>:<nodeID>` scopes a claim
  to one node of one run; an alternative is one topic per logical spec.
  **RESOLVED (v0.5):** work items are `tasks` rows with their own identity;
  topic claims remain for ad-hoc coordination (files, migrations, releases).
