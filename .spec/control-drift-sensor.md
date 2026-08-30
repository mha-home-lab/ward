# Spec: unbiased real-time drift sensor (honest sensor, no bias)

## Purpose
Current drift detection only flags `verified -> error` transitions. Artifacts created as `accepted` (never verified) that degrade to `error` are invisible — `drift=0` reported while 87% of local artifacts are broken. The sensor must measure **absolute variance**: any local artifact where `live_verify != stored verify_status`.

## Signals (what good looks like)
- `ward brief --json` reports `drift_count=N` where N = count of local artifacts with `VerifyStatus != live_verify_result` (any mismatch, including `accepted->error`, `unknown->error`).
- `ward tick --heal` re-runs live verification on **all drifted artifacts** and updates their `VerifyStatus` to match reality.
- After `tick --heal`, `brief` shows `drift=0`, `healed=N`.
- Drift sensor runs fast: parallel verification with timeout, skip non-local artifacts.

## What's kept / changed
- **New**: `verification.DetectDrift(artifact, repoPath)` — returns `{Expected, Actual, IsDrifted}` for a single artifact.
- **New**: `verification.DetectAllDrift(repoPath)` — scans all local artifacts, runs live verify in parallel, returns slice of `DriftReport`.
- **Changed**: `tick.go` — uses `DetectAllDrift`; for each drifted artifact, updates DB `verify_status = Actual`, `verified_at = now()`.
- **Changed**: `brief.go` — calls `DetectAllDrift` (or cached result) to compute `drift_count` honestly.
- **Kept**: `VerifyStatus` column, `Local` flag, `verification.Run` logic.

## Deliberately NOT built
- No "drift rate" derivative control yet — that's a future gain tuning, not sensor honesty.
- No predictive drift — sensor is purely reactive/measurement.

## Verification gate
```bash
# Unit
go test ./internal/verification/... -run TestDetectDrift -v

# E2E
# 1. Create local artifact with accepted status, verify_cmd that passes now
# 2. Break its dependency (delete file, change code)
ward brief --json | jq '.drift_count'  # must be 1
ward tick --heal
ward brief --json | jq '.drift_count'  # must be 0
# Artifact's verify_status must now be "error"
```