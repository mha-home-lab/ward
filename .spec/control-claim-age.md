# Spec: computed claim age in brief (replace the hardcoded "30+")

## Honest framing: one small, precise gap

Audited against `internal/cli/brief.go`. Everything around stale claims already
exists: `StaleClaims(30)` (`brief.go:150`), per-claim rows with id / holder /
title (`brief.go:154-158`), the "STALE CLAIM" next-action line (`brief.go:160`),
and JSON output (`brief.go:213`). The ONLY gap is that the age is a literal:

```go
"mins_aged": "30+",   // brief.go:156 — hardcoded string, not computed
```

Ward already stores `claimed_at`/`expires_at` on claim artifacts
(per `ClaimTopic` + `GetArtifact`), so the true age is derivable without a
schema change.

## Fix (the whole spec)

In `internal/cli/brief.go`, replace the literal with the minutes elapsed since
the claim's creation:

- The stale-claim loop calls `GetArtifact` per id; use its `CreatedAt`
  (or `ExpiresAt` minus TTL) to compute `min(now - created_at)`.
- Emit `mins_aged` as a real number string, and add `age_hours` to the
  `brief --json` stale_claims objects.
- Human output gains the age in the STALE CLAIM line:
  `STALE CLAIM task-... held by ox-alpha (age=144h) ...`.

Exact display format is the implementer's call; the invariant is that the
number is **computed from state**, never a constant.

## What's kept / changed

- **Changed**: `brief.go` stale-claim emit path — computed age instead of
  `"30+"`. Only place touched.
- **Kept**: `StaleClaims(30)` threshold, holder/title lookup, next-action
  generation, JSON shape.

## Verification gate

```bash
# Unit
go test ./internal/cli/... -v

# CLI
# 1. claim a topic, wait 1min (or set CreatedAt in the past via store)
ward brief --json | jq '.stale_claims[0].mins_aged'
# Expected: a numeric string >= 0, NOT the literal "30+"
ward brief | grep "STALE CLAIM"   # line shows a computed age
```