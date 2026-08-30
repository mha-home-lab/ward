# Spec: closed-loop KPI telemetry & scorecard (measure the thesis)

## Purpose
Ward's thesis is "verified memory enables cheaper routing." The controlled variable is **cheap-hit rate** (γ_cheap = successful cheap-tier runs / total runs). This is never measured, logged, or fed back. No telemetry = no control.

## Signals (what good looks like)
- `ward scorecard --window 7d` renders a dashboard with:
  - **Cheap-Hit Rate** γ_cheap = successful cheap runs / total runs (target >30%)
  - **Escalation Rate** ε = escalated runs / total runs (target <20%)
  - **Verification Pass Rate** = verified runs / total runs (target >85%)
  - **Mean Escalations/Task** (target <1.0)
  - **Drift Rate** = drifted artifacts / local artifacts (target <5%)
  - **Claim Age p50/p95** (target p95 < 4h)
- `ward scorecard --json` outputs machine-readable for CI/alerting.
- Data comes from `routing_decisions` table (already exists — just needs aggregation).

## What's kept / changed
- **New**: `internal/scorecard/aggregator.go` — reads `routing_decisions`, computes KPIs over time window.
- **New**: `cmd/scorecard.go` — CLI command with `--window`, `--json` flags.
- **Kept**: `routing_decisions` table (already has `tier`, `memory_hit`, `verify_status`, `run_id`, `created_at`).
- **Changed**: `engine.go` — ensure every routing decision persists `execution_success` (derived from run outcome) and `escalated` boolean.

## Deliberately NOT built
- No real-time streaming dashboard — CLI command is sufficient for control.
- No automatic gain tuning — KPI visibility is step 1; tuning is step 2.

## Verification gate
```bash
# Unit
go test ./internal/scorecard/... -run TestAggregator -v

# CLI
ward scorecard --window 24h
# Verify table renders with columns: Metric | Value | Target | Status (OK/WARN)
# With no data: graceful "no routing decisions in window" message.
```