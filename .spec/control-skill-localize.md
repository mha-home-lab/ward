# Spec: feedforward skill localization pipeline (close the knowledge loop)

## Purpose
Portable lessons (`portable:*` tags) enter as `Local=false` (imported). The router **only** votes cheap for `Local=true` AND `VerifyStatus=verified`. Imported knowledge sits in context but never votes cheap — high token cost on expensive models for already-solved problems. The loop must close: Global → Local Candidate (add VerifyCmd) → Live Verify → Verified Local Artifact → Votes for Cheap.

## Signals (what good looks like)
- `ward skill install <topic> --verify-cmd "<cmd>"`:
  1. Reads skill from global store (`~/.config/opencode/skills/` or remote).
  2. Creates local artifact in repo's `.ward` DB with `Local=true`, provided `--verify-cmd`.
  3. Immediately runs `verification.Run()`; if passes → `VerifyStatus=verified` (votes cheap next route).
  4. If verify fails → `VerifyStatus=error`, artifact exists but doesn't vote cheap; explicit error returned.
- `ward skill list-global` — lists available portable skills from global store.
- After install, `ward brief --json` shows `portable_local_count` incremented.
- Router next run with matching tag assigns `cheap` tier (memory hit = true, verify = verified).

## What's kept / changed
- **New**: `internal/skill/installer.go` — `InstallGlobalSkill(topic, verifyCmd) -> (artifact, error)`.
- **New**: `cmd/skill.go` — `skill install` + `skill list-global` commands.
- **Kept**: Global skill files in `~/.config/opencode/skills/ward-<topic>/SKILL.md`.
- **Kept**: Router purity — only reads `Local` + `VerifyStatus` from store.

## Deliberately NOT built
- No auto-synthesized `VerifyCmd` — human/agent must supply (avoids phantom gates).
- No batch install — one topic at a time for accountability.

## Verification gate
```bash
# Unit
go test ./internal/skill/... -run TestInstallGlobalSkill -v

# E2E
ward skill list-global
ward skill install portable:control-antiwindup --verify-cmd "go test ./internal/store/... -run TestSweepExpiredClaims"
ward brief --json | jq '.portable_local_count'  # increments
ward route --task-tag "control-antiwindup" --json | jq '.tier'  # "cheap"
```