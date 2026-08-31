# Spec: close the capture loop for off-pool work

## Purpose
Auto-capture only fires inside `ward task run`. Free-standing agent sessions
(a green-field batch, an exploratory run — like the exercism bash agent 3
run) read from ward's vault but have no path back into it. Confirmed on a
real case: agent 3 found 3 new, genuinely transferable bash traps (nameref
collision, `local -i` init eval-order, tail-vs-full-suite false-green),
reported them to a human, and captured zero of them. The store still shows
exactly one artifact for that run. Compounding requires knowledge to flow
back in from off-pool work, not just out.

## Signals (what good looks like)

1. **Protocol says what to do off-pool.** `agentdoc.go`'s step 6 currently
   reads as "don't hand-type `memory put`" with no counter-case. Add the
   counter-case explicitly: work done outside `ward task run` that surfaces
   a new, generalizable lesson must be captured manually —
   `ward memory put --local --tags portable:<topic> --verify-cmd "<cmd>"` —
   before `ward memory handoff`. This is the one case hand-typing is
   correct, not a violation of step 6.
2. **The lint fires on the path agents will actually use.** Move the
   `portable:*` transferability check out of `captureNode` into a small
   shared helper (`internal/cli/transferlint.go`, e.g.
   `warnIfCheatSheet(tags []string, summary, content string)`), call it from
   both `captureNode` and `memoryPutCmd`. Same warn-only posture as today —
   `capture` still never blocks; this only makes the manual path consistent
   with the automatic one instead of a silent bypass.
3. **A skipped capture becomes visible, not silent.** `ward memory handoff`
   gains a deterministic gap check:
   - New table `handoff_log(id, at, head_sha)` — one row per handoff call.
   - On each `handoff`: read the previous row (if any), compute
     `git log --oneline <prev_sha>..HEAD | wc -l` (new
     `internal/observe/git.go` helper `GitCommitsSince`, same
     never-fails-the-caller pattern as `GitChangedFiles`) and count
     artifacts `CreatedAt > prev.at`. If commits > 0 and new captures == 0,
     print a loud warning line and set `"capture_gap_suspected": true` in
     `--json`. Then write the new row.
   - First-ever call (no previous row): no gap check, just seed the row.
4. **The next session sees it too, not just a human reading a report.**
   `ward brief`'s suggested-next-actions gains one line when the most recent
   `handoff_log` row was flagged: `"previous session may have skipped
   capture (N commits, 0 new artifacts) — review and backfill if real
   lessons were found"`. This is the actual loop-closer: even if the acting
   agent ignores its own handoff warning, the *next* agent's session start
   surfaces it.

## Decisions (resolved before code)

- **Presence/absence only, never a semantic judgment.** The gap signal is
  "commits happened, captures didn't" — a count, not an opinion about
  whether real learning occurred. Whether the gap was meaningful is left to
  a human or the next agent, same boundary the transferability lint already
  draws between "flag the shape" and "judge the content."
- **Warn, never block.** A session that genuinely learned nothing new
  shouldn't be forced to invent a capture to satisfy a heuristic — that
  would manufacture exactly the noise this whole project has avoided.

## What's kept / changed

- **New**: `handoff_log` table (migration), `GitCommitsSince` in
  `internal/observe/git.go`, `warnIfCheatSheet` shared helper.
- **Changed**: `memoryPutCmd` (internal/cli/memory.go) calls the shared
  lint helper; `memoryHandoffCmd` computes and reports the gap;
  `agentdoc.go` protocol text gets the off-pool capture instruction;
  `brief.go` surfaces a flagged prior gap in suggested next actions.
- **Kept**: `autoCapture`/`captureNode` untouched — on-pool capture already
  works correctly.

## Deliberately NOT built

- No model call to infer "did the agent learn something" from a transcript
  or diff — that's the classifier this project has avoided at every gate so
  far. This spec only makes an existing silence audible.
- No hard block on ending a session without a capture — false positives
  (a session that truly found nothing new) are certain, and blocking on a
  heuristic here would train agents to game it with junk captures, which is
  worse than the current silence.
- No retroactive capture of agent 3's three specific lessons by ward
  itself — that's a human/agent action using the now-fixed manual path, not
  something this spec automates.

## Verification gate

```bash
go test ./internal/observe/... -run TestGitCommitsSince -v
go test ./internal/cli/... -run 'TestHandoffCaptureGap|TestMemoryPutLint' -v

# E2E: simulate the exact failure mode
ward memory handoff --json   # seeds handoff_log (first call, no gap check)
echo x >> some_file.go && git commit -am "off-pool change"
ward memory handoff --json | jq -e '.capture_gap_suspected == true'
# Expected: true — 1 commit, 0 new artifacts since last handoff

ward memory put --local --tags portable:test \
  --summary "x" --content "collatz prints exactly 'Error'" --verify-cmd true
# Expected: stderr warns CheatSheet (same signal captureNode already gives)

ward brief | grep -i "may have skipped capture"
# Expected: the flagged gap from the prior handoff surfaces at next session start
```
