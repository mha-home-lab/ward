# routing — Classifier / Model Tier Router (new)

| | |
|---|---|
| Status | Draft (v1 planning) |
| Domain | routing |
| Version | 0.1.0 |

## Purpose

WARD's core value-add with no analogue in either source project: **route each
unit of work to the cheapest model capable of doing it correctly.** The router
consumes signals from memory (chef prior-knowledge search), verification
(verify result), orchestration (DAG concurrency), and task type, and emits a
`{tier, model, ceremony_level}` decision per work unit. It must never trust an
unverified artifact (verification.md owns that precondition).

## Signals

1. **Memory hit / miss** — result of chef-style `search`/`context` for the
   node's topic. A *verified* hit means "this pattern is known and the repo
   still matches" → cheap is safe.
2. **Verify result** — `verify_status` of any candidate memory artifact
   (`verified` | `stale` | `error` | `unknown`). Only `verified` counts as a
   real hit; everything else is treated as a miss. **This is the verify-gap
   closure in routing.** (verification.md)
3. **DAG concurrency / contention** — branching factor (parallel nodes) and
   shared-state touched-set overlap (orchestration.md). High contention ⇒
   escalate tier *and* raise `ceremony_level` to `full`.
4. **Task type** — derived from the DAG node kind (`test` ⇒ correctness matters
   ⇒ at least `mid`; `review` ⇒ `strong`; `specification`/`decomposition` ⇒
   `mid`), plus optional per-agent `model` hint from the definition.

## Tiering policy (v1 — deterministic, not a model call)

| Condition | Tier | Model example | Ceremony |
|---|---|---|---|
| Known pattern + `verify=verified` + low contention | `cheap` | flash-class | light |
| Memory miss OR `verify≠verified` + low contention | `mid` | mid-class | light |
| `test`/`review` node type | `mid`–`strong` | mid/strong | light–full |
| High contention (parallel/shared-state) | escalate +1 | stronger | full |
| Cheap attempt failed → retry | escalate | mid/strong | (as needed) |

Tiers map to **provider + model** via a configurable `tiers` table (e.g.
`cheap → provider=flash, model=...`). v1 router is a deterministic function of
the four signals — **not itself an LLM call** — to keep cost and latency
predictable and to avoid a recursive routing problem.

## Escalation rules

- **Novel / unverified** → escalate at least one tier above the cheapest.
- **Contested shared state** → escalate and force `full` ceremony (mandatory
  approval + claim checks).
- **Cheap failure** → automatic retry at `mid`; if `mid` fails, `strong`.
  Every escalation is logged to `routing_decisions` with `escalated_from` +
  `reason` so the decision table can be analyzed later (future learning loop).
- **Stale verified artifact used as a signal** is a hard error path: routing
  must not have consumed it; if it did (bug), the run is flagged for review.

## What's kept from ciao

- The *idea* that an agent has a `model` (ciao `AgentDefinition.Model`). In WARD
  the field becomes the router's default/hint, overruled by the computed tier.

## What's changed and why

- ciao hard-coded `gemini-2.0-flash` in `skillrunner/llm.go`. WARD removes the
  constant and introduces the router as the single place that picks a model,
  using memory + verify + concurrency. This is the unification payoff: chef's
  memory and ciao's execution finally inform *which* model runs each step.

## Output contract

`routing_decisions` row per decision:
`tier, model, ceremony_level, memory_hit, verify_status, escalated_from, reason`.
This is the audit trail and the seed for a future learned router (out of v1
scope). The executor (orchestration.md) reads `{tier, model}` to call the
provider and `{ceremony_level}` to decide lifecycle/approval strictness.

## Open questions / risks

- **Tier → model mapping is deployment-specific.** WARD ships a default but the
  user must configure provider keys + model names. Should tiers be named
  (`cheap`/`mid`/`strong`) or numeric? Proposal: named, mapped in config.
- **Measuring "correctly".** v1 escalation on *failure* is observable (test node
  fails, or executor returns error). But "did the cheap model do it *correctly*
  when it didn't error?" needs a verification signal. Proposal: rely on
  downstream `test`/`review` nodes + `verify` to catch silent wrongness;
  log mismatches. True routing-accuracy learning is post-v1.
- **Meta-router temptation.** A learned/predictive router (an LLM that picks
  the model) is attractive but risks recursion + cost. v1 stays deterministic.
  Open: revisit after `routing_decisions` accumulates data.
- **Contention threshold.** What branching factor / overlap triggers `full`
  ceremony? Needs a tuned constant or a config knob. Open.
- **Retry budget / loop protection (explicit).** The cheap→mid→strong escalation
  on failure needs a hard cap (proposal: max 2 escalations per work unit) and a
  guard against escalate→fail→escalate loops. When `strong` itself fails, the
  path must terminate (mark the run `rejected` or route to a human), not retry
  indefinitely. The exact budget and the "all tiers failed" handler are not yet
  fixed. Open.
