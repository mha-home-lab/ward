# Spec: topic-scoped heal — the tiny real delta behind "skill sharpening"

## Honest framing: there is no separate sharpening loop to build

The prior version proposed a new `SharpenAll` loop + `ward skill sharpen`
command, framed as an "adaptive feedback loop". Audited against the code, that
work **already exists as `tick`**:

- Iterate local accepted artifacts → ✓ `sweepVerify`, internal/cli/tick.go:23-46
- Live-verify each → ✓ `verification.Run` (same loop)
- Update `verify_status` on change → ✓ `SetVerify` inside the loop
- Supersede/demote anything failing → ✓ `tick --heal`, tick.go:82-97 (acts on
  ANY local accepted artifact now `stale`/`error`, not just fresh drift)

So the "adaptation loop" is `ward tick [--heal]`, already covered. Building
`skill sharpen` would duplicate it.

## The only real delta: `--topic <tag>` scoping

`tick` and `brief` re-verify **all** local accepted artifacts. Sometimes you
want to re-verify a single skill/topic's sources (a chip says recompile; you
want a focused heal). That is a one-flag extension to the EXISTING path:

- `ward tick [--heal] --topic <tag>` — restrict `sweepVerify`'s artifact set
  to those matching `<tag>` (reuse `SearchArtifactsTagged`/`ListArtifacts` with
  a topic filter), then run the unchanged heal logic.

No new package, no new command, no parallel runner, no `SharpenResult` matrix.

## Signals (what good looks like)

- `ward tick --heal --topic portable:control-antiwindup` re-verifies only that
  tag's local artifacts, updates their `verify_status`, and supersedes the
  failures — same output shape as `tick --heal` today.

## What's kept / changed

- **Changed**: `tick.go` — optional `--topic` filter feeding `sweepVerify`.
  Only addition.
- **Kept**: `sweepVerify`, `verification.Run`, heal loop, output format.

## Deliberately NOT built

- No `ward skill sharpen` command (it is `tick --heal`).
- No parallelism or status matrix — the existing serial loop is sufficient.

## Verification gate

```bash
# Unit
go test ./internal/cli/... -run TestTickTopicFilter -v   # added with the flag

# E2E
# artifacts on two tags; break one tag's dependency
ward tick --heal --topic <broken-tag> --json
# Expected: only <broken-tag> artifacts in changed[]; other tag untouched
ward tick --heal
# Expected: no remaining change for <broken-tag> (already healed); others still clean
```