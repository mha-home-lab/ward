# Spec: routing-KPI telemetry (cheap-hit / escalation / verification rates)

## Honest framing: grounded, but must not collide with the existing scorecard

Audited: `routing_decisions` exists and is populated.

- Schema — `internal/store/store.go:334`: `run_id, node, tier, model,
  ceremony_level, memory_hit, verify_status, contention, escalated_from,
  reason, contention_inputs, created_at`.
- Writes — `AddRoutingDecision`, `internal/store/artifacts.go:447`, driven by
  the router/orchestrator (`engine.go:312` updates `verify_status` to `passed`
  post-run).
- Reads — `AllRoutingDecisions` (artifacts.go:459) and
  `RoutingDecisionsForRun` (artifacts.go:485) already give recent decisions.

**Do NOT reuse the `ward scorecard` command.** That command already exists and
means something else: outcome-based engineer performance
(`internal/cli/scorecard.go` — done/bounced/rejected/holding per agent). The
routing KPIs command must have its own name. Proposal: `ward kpis [--window]`.
The two are complementary: `scorecard` = engineer performance from pool
outcomes; `kpis` = memory-hit / routing-control telemetry from
`routing_decisions`.

## What is actually missing

1. No CLI surface that aggregates `routing_decisions` into the thesis' control
   variables (cheap-hit %, escalation %, verification pass %).
2. No per-decision **success** signal: `routing_decisions` has `verify_status`
   (pre/post-run gate) but no `execution_success` column. Cheap-HIT means
   "tier=cheap AND ran successfully" — success must come from joining run
   outcomes (the `runs`/`run_events` tables) or a new nullable column on
   `routing_decisions`. **Decision made**: take the additive-column route — a
   nullable `execution_success` on `routing_decisions`, stamped by the
   orchestrator/engine at run completion. Cheaper to query than a join and
   consistent with `verify_status` already living on the row.

## Signals (what good looks like)

- `ward kpis --window 7d` renders:
  - Cheap-hit rate = (tier=cheap AND success) / total decisions
  - Escalation rate = (escalated_from IS NOT NULL) / total decisions
  - Unverified-route rate = routes taken with `memory_hit=0` / total
    (proxy for "the memory did not pay for the task")
  - Verification pass % = decisions whose run left `verify_status='verified'`
- `--json` machine-readable; graceful empty state when the window has no rows.
- **Explicitly not** touching `internal/cli/scorecard.go` or its semantics.

## What's kept / changed

- **New**: aggregation over existing `routing_decisions` (via
  `AllRoutingDecisions`) + an `execution_success` signal.
- **New**: `ward kpis` command (distinct name; never `scorecard`).
- **Changed**: additive nullable `execution_success` column on
  `routing_decisions`, stamped at run completion.
- **Kept**: `routing_decisions` schema and write path (`AddRoutingDecision`).

## Deliberately NOT built

- No streaming dashboard; CLI + `--json` is enough for control.
- No gain tuning — visibility first.

## Verification gate

```bash
# Aggregation correctness
go test ./... -run TestKPIAggregation -v   # (test file added with the impl)

# CLI
ward kpis --window 24h
# Expected: table/JSON with cheap-hit %, escalation %, verify pass %; empty
# window yields graceful "no routing decisions in window", not a panic.

# Collision guard (must keep passing; outcome-based command unchanged)
ward scorecard      # still engineer scorecards
ward scorecard --json
```