# Spec: anti-windup claim expiry & enforcement (close the claim integrator)

## Purpose
The claim system allocates work to agents with a TTL (`expires_at`). When agents crash or abandon tasks, claims remain `claimed` indefinitely — the integrator winds up, saturating the pool with stale locks while `brief` reports `expired-claims-freed=0` every sweep. This is the textbook integrator windup: capacity reports healthy while actual throughput is zero.

## Signals (what good looks like)
- `ward tick --sweep` (and `brief`) calls `SweepExpiredClaims` which:
  - Atomically frees claims where `status='claimed' AND expires_at < now`
  - Resets the associated task to `open` so it re-enters the pool
  - Returns count of freed claims
- `ward brief` prints: `expired-claims-freed=N`, `active-stale-claims=M`, `longest-claim-age=Hh`
- `ward brief --json` includes: `expired_claims_freed`, `active_stale_claims`, `longest_claim_age_hours`

## What's kept / changed
- **New**: `store.SweepExpiredClaims()` — atomic UPDATE with `RETURNING` (or two-statement: UPDATE then SELECT) that frees expired claims and resets their tasks to `open`.
- **New**: `tick.go` calls `SweepExpiredClaims` before any other sweep; logs freed claim IDs.
- **Changed**: `brief.go` — computes `expired_claims_freed` (from last sweep), `active_stale_claims` (claims where `status='claimed' AND expires_at < now`), `longest_claim_age_hours` (max of `now - claimed_at` for active claims).
- **Kept**: claim creation, atomic claim (`TakeTask`), TTL on claim creation.

## Deliberately NOT built
- No claim heartbeats / agent liveness pings — over-engineering. TTL + sweep is the honest, auditable bound.
- No "soft" grace period — TTL is the contract; expired = free.

## Open questions
- Should `SweepExpiredClaims` also free claims with `expires_at=NULL`? No — `NULL` = no expiry = permanent until human drops.

## Verification gate
```bash
# Unit
go test ./internal/store/... -run TestSweepExpiredClaims -v

# Integration
ward debug insert-claim --task-id "test-windup-01" --ttl "-1h" 2>/dev/null || true
ward tick --sweep
ward brief --json | jq '.expired_claims_freed, .active_stale_claims'
# Expected: expired_claims_freed >= 1, task "test-windup-01" state == "open"
```