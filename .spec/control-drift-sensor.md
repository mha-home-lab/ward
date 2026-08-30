# Spec: unbiased drift reporting (count every failing local artifact)

## Honest framing: narrower than previously claimed

The prior version repeated an unsourced stat ("87% of local artifacts are
broken", from the case study reflection) and proposed a new `DetectDrift`/
`DetectAllDrift` scanner + parallel batch loop. That overbuilds what is needed,
because most of it already exists and works:

- `sweepVerify` (internal/cli/tick.go:23-46) already re-runs every
  **local accepted** artifact's verify_cmd LIVE and persists the outcome
  (`SetVerify`) — this is exactly "absolute-variance checking".
- `fail()` already classifies honestly (internal/verification/verify.go:136-143):
  previously-verified → `stale`, never-verified → `error`.
- `tick --heal` (internal/cli/tick.go:82-97) **is already unbiased**: it
  supersedes ANY local accepted artifact whose post-sweep status is `stale` or
  `error`, regardless of whether it drifted this sweep or was a persisting
  zombie from a previous tick. It is not limited to verified→failed.

The one genuine bias: the **reported drift count** (internal/cli/tick.go:38-40)
increments ONLY on `verified -> non-verified` transitions *within this sweep*.
So an artifact that failed a previous tick and is healed now is counted as a
`change` but never as `drift`; and the "drift" headline understates how much of
the local memory is failing. The tree's health metric (brief.go:121-129) has
the same shape (counts only `verified`); it is fine as-is because it reports
absolute verified/accepted/proposed, not a `drift` figure.

## Fix (small)

Make the reported `drift` count absolute, not transition-only: after the sweep
loop, count local accepted artifacts whose live `verify_status` is non-verified
(`stale` or `error`) — both fresh failures this sweep and persisting ones.

- `internal/cli/tick.go` `sweepVerify`: compute `drift` = count of evaluated
  artifacts whose `res.Status != "verified"`, instead of only
  `before=="verified" && res.Status!="verified"`.
- `internal/cli/brief.go:257` message: generalize "previously-verified
  artifact(s) went STALE" to a count of failing artifacts (still instructive —
  it drives the "treat as miss" guidance). No behavior change elsewhere.

`--heal` stays exactly as-is: it already closes the loop over all failing
local artifacts.

## Deliberately NOT built

- No new `DetectDrift`/`DetectAllDrift` package, no parallel batch — the
  per-sweep loop already does the work serially and correctly.
- No "drift rate" derivative — future gain tuning, not sensor honesty.

## Verification gate

```bash
# Unit
go test ./internal/cli/... ./internal/verification/... -v

# E2E (drift must reflect failures regardless of starting status)
# 1. promote a local artifact with a verify_cmd; run `ward tick` (verify passes: drift=0)
# 2. break its dependency; run `ward tick --heal`
#    Expected: drift >= 1 (the failing artifact is counted), then superseded (healed)
# 3. run `ward tick --heal` again on an already-failed artifact
#    Expected: drift counts it even though it was NOT verified-before (error -> still failing)
ward brief --json | jq '.drift'   # counts every non-verified local accepted artifact

# Heal loop regression (already unbiased, must stay that way):
# a local artifact already in "error" (never verified) must be superseded by --heal
```