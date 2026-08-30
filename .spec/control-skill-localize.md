# Spec: feedforward skill localization pipeline (close the knowledge loop)

## Purpose
Portable lessons (`portable:*` tags) enter as `Local=false` (imported). The router **only** votes cheap for `Local=true` AND `VerifyStatus=verified`. Imported knowledge sits in context but never votes cheap — high token cost on expensive models for already-solved problems. The loop must close: Global → Local Candidate (add VerifyCmd) → Live Verify → Verified Local Artifact → Votes for Cheap.

## Signals (what good looks like)

- `ward skill install <topic> --verify-cmd "<cmd>"`:
  1. Reads skill from global store (`~/.config/opencode/skills/` or remote).
  2. Creates a local artifact in repo's `.ward` DB with `Local=true`, provided
     `--verify-cmd` (mirrors `ward memory put --local`, but sourced from the
     skill rather than typed by hand).
  3. Immediately runs `verification.Run()`; if passes → `VerifyStatus=verified`
     (the artifact can now vote cheap on its topic).
  4. If verify fails → `VerifyStatus=error`, artifact exists but doesn't vote
     cheap; explicit error returned.
- `ward skill list-global` — lists available portable skills from global store.
- Post-install proof (all existing commands — nothing invented below):
  - `ward memory list --status accepted --json` shows the new artifact with
    `"Local": true` / `"VerifyStatus": "verified"`.
  - The pure router maps (memory hit + verified) → cheap:
    `ward route test --kind test --memory-hit --verify-status verified` prints
    `tier=cheap`.
  - In a real run, `ContextForTask` + `routing.Route` pick up the tag-scoped
    verified hit the same way (orchestration-side lookup — the CLI `ward route`
    is node-scoped, not tag-scoped).

## Decisions (resolved before code)

- **Multi-source chips**: a chip's `SKILL.md` can cite several source artifact
  ids (`chipSourceIDs` parses a list). `install` is **not** a reification of
  those sources: it always creates **one** local artifact per chip, and its
  `--verify-cmd` is user-supplied and independent of the chip's original
  sources. It is a fresh local claim *about the topic*, not a literal copy —
  the chip's sources stay untouched in their home store. This keeps the
  verify gate honest (the gate checks the claim, not some other store's
  artifacts) and the 1:1 mapping unambiguous.

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
id=$(ward skill install portable:control-antiwindup \
     --verify-cmd "go test ./internal/store/... -run TestSweepExpiredClaims" --json \
     | jq -r '.id')          # install prints the new artifact id
ward memory get "$id" --json | jq -e '.Local == true and .VerifyStatus == "verified"'
# Expected: true — a verified LOCAL artifact now exists for the topic
ward route test --kind test --memory-hit --verify-status verified | grep 'tier=cheap'
# Expected: the pure router maps a verified memory hit to the cheap tier
# (this is the vote the new artifact enables on future runs)
```