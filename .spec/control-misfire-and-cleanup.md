# Control: WARD-itself misfire fix + scoped port-session cleanup

## Purpose

Two frictions block an otherwise-clean one-off knowledge-port session. First,
`warnIfMisplaced` fires on **any** `portable:*` tag — not specifically
`portable:ward` — so every routine off-pool portable capture filed outside
ward's own store gets a false "this request is tagged for WARD itself"
warning, exactly the common case the v0.10.1 ship-workflow chain made routine.
Second, a one-off port session still ends manually (`init --no-agents-md` at
the start, `rm -rf .ward` at the end) — the thin v0.10.1 workflow is missing
its closing bookend.

## Part A — fix the `warnIfMisplaced` misfire

**Root cause:** the trigger `portableTopicName(t) != ""` is true for any
`portable:*` tag, not specifically `portable:ward`. The function's own doc
comment says it guards "content about WARD itself", but the code matched the
general portable mechanism marker.

**Fix:** narrow the condition so only actual ward-itself tags (bare `ward` or
`portable:ward`) are treated as misplaced ward requests:

```go
if t == "ward" || portableTopicName(t) == "ward" {
    wardish = true
}
```

The absolute-path comparison below it (`store.ProjectHome("ward")` vs
`store.Home()`, both absolutized) is correct and unchanged.

## Part B — `skill-sync --cleanup` for one-off port sessions

A fresh flag on the existing `skill-sync` command, closing the loop:

- **Explicit opt-in, not inferred** — `--cleanup` is only honored when the
  agent passes it, matching the D0.3 pattern of explicit trust declarations.
- **Runs only after a successful sync** — it is invoked at the command's
  success sinks, never on an error return mid-loop, so a failed sync's
  evidence is never deleted out from under the agent.
- **Safety guard — refuses on real history.** Before removing the local store
  it requires the store contain only what a port session could produce: zero
  rows in both `tasks` and `runs` (new `store.HasTaskOrRunHistory()`). If
  either table is non-empty the store is a real project, not a scratch port
  session, and `--cleanup` refuses with a clear message and leaves the store
  untouched.
- **Safety guard — refuses on a fully-failed sync.** If every portable topic
  was skipped (all sources cheat-sheet, nothing synced), `--cleanup` refuses
  rather than deleting the evidence of a failure.
- **Visible, not silent.** The removal is reported: a `cleanup.removed` /
  `cleanup.path` sub-document merged into the single `--json` output (one JSON
  document, not two), and one human line in text mode.

## What's kept / changed

- **Changed** `internal/cli/root.go` `warnIfMisplaced`: the one-condition
  narrowing above; doc comment aligned with the narrower meaning.
- **Changed** `internal/cli/sync.go` `skill-sync`: new `--cleanup` flag; a
  `cleanupLocal` closure that runs at success sinks, guards on task/run
  history and all-skipped, closes the store, removes `store.Home`, and returns
  a JSON sub-document merged into the existing output.
- **New** `internal/store/store.go` `HasTaskOrRunHistory()` — counts rows in
  `tasks` and `runs`.
- **New tests** `TestWarnIfMisplaced` (ordinary portable does not misfire;
  `portable:ward` does) and `TestSkillSyncCleanup*` (successful removal,
  task-history refusal preserves store, all-skipped refusal preserves store).

## Deliberately NOT built

- No auto-detection of "is this a port session" — explicit `--cleanup` only.
- No `init`-owning mega-command — `init --no-agents-md` is already one call.
- No cleanup of anything but the local `.ward` directory — never touches the
  global skills directory just synced to.
- No change to the relative-vs-absolute path guard (it was already correct).

## Verification gate

```bash
go test ./internal/cli/... -run 'TestWarnIfMisplaced|TestSkillSyncCleanup' -v

# misfire regression: ordinary portable capture, non-ward store -> no warning
ward memory put --local --tags portable:bash --summary "s" \
  --content "the pattern ... because X" --verify-cmd true 2>&1 \
  | grep -v "tagged for WARD itself"

# still fires for the case it exists for
ward memory put --local --tags portable:ward --summary "s" \
  --content "the pattern ... because X" --verify-cmd true 2>&1 \
  | grep -q "tagged for WARD itself"

# cleanup removes a one-off session's store
ward skill-sync --cleanup --json | jq -e '.cleanup.removed == true'

# cleanup refuses on task/run history, leaving the store untouched
ward skill-sync --cleanup 2>&1 | grep -q "refuses to remove"
test -d .ward
```