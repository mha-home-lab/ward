# Transparency & anti-phantom patch

## Purpose
Turn WARD from a "black box that claims to verify" into a "transparent engine
that proves it verified." Addresses the agent feedback: opacity of task
metadata (store is a binary db) and self-attestation (no real proof a check ran).

## Signals
- After `ward task run`, a human/agent can read exactly what executed and why.
- A task cannot close while claiming "verified" unless evidence exists that the
  acceptance check actually ran and exited 0.
- Trivial phantom gates (`true`, `echo`) are refused at authoring time.

## What's kept
- SQLite stays the system of record for state; sidecar logs are additive evidence.
- The pure router (`internal/routing.Route`) is untouched.
- Manual tasks with no gate still allowed (warned, not rejected).

## What's changed and why
1. **Sidecar logs** (`internal/store/logs.go`): every node `run` writes
   `.ward/logs/<runID>_<nodeID>_<ts>.log` with cmd, exit code, elapsed, output.
   Bypasses the opaque db for human inspection.
2. **`ward task show <id>`** (`internal/cli/task.go`): audit window — task
   metadata + run status + gate + exit code + last 15 lines of the sidecar.
3. **`tasks.last_run_id`** (migration v6→v7): maps a task to its most recent run
   so `show`/pre-close can locate evidence without guessing.
4. **Pre-close gate** (`taskRunCmd`): when a task declared a gate, it only closes
   if a sidecar exists for the run and shows `exit_code == 0`.
5. **Hard-reject trivial gates** (`taskAddCmd`): `true`, `echo`, `:`, `echo ...`
   are refused as `verify_cmd`/`run`. Empty gate remains a loud warning (manual work).
6. **Dossier tail** (`engine.WriteDossier`): the reject dossier now appends the
   failure-sidecar tail so a human sees *why* it failed, not just "failed 2x".
7. **AGENTS.md**: protocol forbids phantom `verify_cmd`; adds a GitHub CI bridge
   snippet so high-tier verification polls immutable CI instead of the agent's shell.

## Open questions
- Whether `ward task done` (manual) should also require evidence when a gate was
  declared. Deferred: `done` is the human's explicit override; `run` is the gate.
