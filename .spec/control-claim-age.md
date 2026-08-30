# Spec: claim age metrics in brief (observability for integrator windup)

## Purpose
Stale claims accumulate silently. `brief` reports `STALE CLAIM` but no age, no count, no severity. Adding claim age makes the integrator windup visible and actionable.

## Signals (what good looks like)
- `brief` output includes for each stale claim:
  - `task-798321b1 (age=144h) — STALE`
  - Summary line: `active-stale-claims=6, longest-claim-age=144h`
- `brief --json` includes:
  ```json
  "active_stale_claims": 6,
  "longest_claim_age_hours": 144,
  "stale_claims": [
    {"id": "task-798321b1", "age_hours": 144, "holder": "ox-alpha"},
    ...
  ]
  ```

## What's kept / changed
- **New**: `brief.go` computes claim age = `now() - ClaimedAt` for each claimed task.
- **Changed**: `brief` output formatting — stale claims show age, summary line added.
- **Kept**: Claim creation, TTL, atomic claim logic.

## Deliberately NOT built
- No automatic alert on claim age > threshold — that's a policy for scorecard/alerting layer.

## Verification gate
```bash
# Unit
go test ./internal/cli/... -run TestBriefClaimAge -v

# CLI
ward brief --json | jq '.stale_claims[0].age_hours'  # >0
ward brief | grep "active-stale-claims"
```