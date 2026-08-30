# Spec: autonomous adaptation via skill sharpening (adaptation loop)

## Purpose
Local skills (`Local=true`) degrade silently as codebase evolves. No scheduled re-verification = no adaptation. `ward skill sharpen` re-evaluates all local skills against current repo state, updating `VerifyStatus` or demoting stale assets before they poison routing.

## Signals (what good looks like)
- `ward skill sharpen --all`:
  1. Iterates all artifacts where `Local=true`.
  2. Runs `verification.Run(artifact, repoPath)` in parallel.
  3. If `res.Status != artifact.VerifyStatus` → updates DB (`verify_status = res.Status`, `verified_at = now()`).
  4. Returns matrix: `{ID, OldStatus, NewStatus, Changed}`.
- `ward skill sharpen --topic <tag>` — scopes to artifacts with that tag.
- Output includes summary: `sharpened=X, degraded=Y, stable=Z`.
- Can be scheduled (cron, CI) — no interactive prompts.

## What's kept / changed
- **New**: `internal/skill/sharpen.go` — `SharpenAll(repoPath) -> ([]SharpenResult, error)`.
- **New**: `cmd/skill.go` — `skill sharpen [--all] [--topic <name>] [--json]`.
- **Kept**: `verification.Run` logic, `Local` flag, `VerifyStatus` column.
- **Changed**: `verification.Run` must be fast enough for batch (parallel, timeout per artifact).

## Deliberately NOT built
- No auto-demote to `proposed` on repeated failures — that's a policy decision, not a sensor.
- No "skill retirement" — stale skills stay visible but marked `error`.

## Verification gate
```bash
# Unit
go test ./internal/skill/... -run TestSharpenAll -v

# E2E
ward skill sharpen --all --json
# Verify output: array of {id, old_status, new_status, changed}
# Verify DB updated: verify_status matches live verify for all local artifacts
```