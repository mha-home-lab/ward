# Spec: anti-windup claim expiry — validate the built mechanism (gap = age telemetry)

## Honest framing: the mechanism is already built

Audited against the code. `SweepExpiredClaims` exists, is wired into both
`tick` and `brief`, is printed, and is tested:

- `store.SweepExpiredClaims()` — `internal/store/artifacts.go:120`. Marks the
  claim artifact `superseded`/reason `expired` and clears `claim_topic` (the
  unique slot), so an expired un-released claim can never block re-claim.
- Runs in `ward tick` — `internal/cli/tick.go:103` (prints `expired claims freed=%d`).
- Runs in `ward brief` — `internal/cli/brief.go:68`, surfaced as
  `claims_expired` (`brief.go:72`) and printed as `expired-claims-freed=%d`
  (`brief.go:307`).
- Tested — `TestSweepExpiredClaims`, `internal/store/store_test.go:187`.

The prior version of this spec falsely claimed brief "reports
expired-claims-freed=0 every sweep" and that the sweep was new. That is wrong
on both counts. There is no separate `claims` table and no "task state" to
reset: in Ward a claim is an artifact row (`kind='claim'` + `claim_topic`),
so "freeing" means supersede + clear slot, and the topic is immediately
re-claimable.

## The actual remaining gap

Not the sweep mechanism — the **age telemetry**. `brief` reports freed counts
and stale claim IDs/holders, but with a hardcoded age string (`"mins_aged":
"30+"`, `internal/cli/brief.go:156`) rather than a computed age. That work is
tracked separately in `.spec/control-claim-age.md`. Beyond it, the
anti-windup mechanism needs no code.

## Verification gate (regression run, no build expected)

```bash
# Mechanism is built + tested; gate is proof, not work.
go test ./internal/store/... -run TestSweepExpiredClaims -v

# Integration, live:
ward brief --json | jq '.claims_expired'   # numeric, >= 0
ward tick | grep 'expired claims freed'    # line present
```

Expected outcome of this spec: **no production code change**. If a sweep bug
is found while running the gate, fix that; otherwise close with the
claim-age spec delivering the only remaining deltas (`active_stale_claims`,
`longest_claim_age_hours`).