# Spec: doc-claims (catch stale prose)

## Purpose
Ward re-verifies store-local artifacts that carry an explicit `verify_cmd`, but
never looks at docs/specs/architecture prose. A security lab's documented attack
surface drifting from code is exactly the class of failure that should be flagged.
The gap is not a missing verifier — `verification.Run` already supports
`verify_kind: grep` (`pattern::path`) — it is that docs are not represented as
verifiable artifacts.

## Signals
- `ward doc assert <path> <pattern> [--name TITLE] [--by WHO] [--tags t1,t2]`
  registers a `doc`-kind, store-local, accepted artifact whose `verify_cmd` is
  `pattern::path` and `verify_kind` is `grep`, then runs the check live and
  reports pass/fail (exit 1 on fail, so it is scriptable).
- `ward doc verify` re-runs every `doc` claim (discoverability wrapper over the
  existing sweep; `ward verify --all` already covers it).
- A failing doc claim becomes a stale/error artifact, so `ward tick` / `ward
  verify --all` / `ward brief` (drift) surface it. Registering the invariant
  once is enough; later regressions are caught automatically.

## What is kept / changed
- Reuses `verification.Run` (grep kind), `Store.UpsertArtifact`, `SetVerify`, and
  brief's existing drift reporting. No new verifier, no new DB column.
- `doc` is just an artifact `Kind`; nothing special-cased in the store.

## Deliberately NOT built
- A speculative `ward lint docs` that heuristically mines spec↔code invariants.
  That is unverifiable pattern-matching; the explicit doc-claim is the honest,
  agent-controlled primitive. The user asserts the invariant; Ward enforces it.

## Open questions
- None blocking.
