# broker — parallel agent dispatch primitives (v0.4)

| | |
|---|---|
| Status | Draft (v0.4 planning) |
| Domain | broker |
| Version | 0.4.0 |

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

- The claim as **advisory** coordination (chef's coordination-001): voluntary, not
  a hard lock. A conflict is reported (warn, or error under `--strict`); the
  loser backs off.
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
conflict (warn / `--strict` error). Release/supersede sets `claim_topic = NULL`,
freeing the slot so the topic can be re-claimed.

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

### 3. Cross-process escalation handoff (DESIGN ONLY — not implemented this pass)

Today `failNode()` escalates a node *within one Engine's run loop*: it bumps
`escalation`, marks the node `failed`, and the same loop re-picks it. For parallel
agents that breaks, because the retrying agent may be a different process than the
one that failed.

Design:
- Each work item is its own claim topic, e.g. `claim_topic = "<runID>:<nodeID>"`.
- On failure, the **claim is released** (so the slot reopens) **and** the node's
  required tier bumps by one. The bump is already persisted: `run_nodes.escalation`
  is incremented by `failNode` today, and the router maps `escalation` → tier. So
  the *required tier* survives across processes for free — no new column needed.
- The node then re-enters the **pickup pool for ANY eligible agent** (any agent
  whose budget ≥ the new required tier), not necessarily the one that failed.
- The pickup pool / poll loop that actually matches `(requiredTier, agentBudget)`
  and hands out work is the broker service — explicitly deferred (non-goal).

This keeps escalation semantics identical (budget, cap at 2, reject→human) while
making the *work* re-claimable by a different process.

## Output contract

- `artifacts.claim_topic` + `uni_claim_topic` index: the atomicity invariant.
- `routing_decisions` rows now reflect the floor when a node declares `tier:`.
- `run_nodes.escalation` doubles as the cross-process required-tier signal.

## Open questions / risks

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
  un-released expired claim would still block re-claim. Open: a `tick`-style sweep
  that nulls `claim_topic` on expired claims. Deferred (activeClaims already hides
  expired claims from listing; the blocking case is rare and mitigated by release).
- **Pickup/poll loop + agent budget registration** are the next piece (v0.5+),
  built directly on these two primitives. Deliberately not in this spec.
- **Topic granularity for work items.** Using `<runID>:<nodeID>` scopes a claim to
  one node of one run; an alternative is one topic per logical spec. Left to the
  broker design (out of scope).
