# Transparency & anti-phantom patch

## Purpose
Turn WARD from a "black box that claims to verify" into a "transparent engine
that proves it verified." Addresses the agent feedback: opacity of task
metadata (store is a binary db) and self-attestation (no real proof a check ran).

## Signals
- After `ward task run`, a human/agent can read exactly what executed and why.
- A gated task cannot close while claiming "verified" unless evidence exists that
  the acceptance check actually ran and exited 0.
- Exact no-op phantom gates (`true`, `false`, `:`) are refused at authoring time.

## What's kept
- SQLite stays the system of record for state; sidecar logs are additive evidence.
- The pure router (`internal/routing.Route`) is untouched.
- Manual tasks with no gate still allowed (warned, not rejected).
- Historical runs are never downgraded: evidence is DERIVED FROM DISK, not stored
  in a column, so pre-feature runs are trusted (their DB status is the proof) and
  are never branded "legacy"/unproven.

## What's changed and why
1. **Sidecar logs** (`internal/store/logs.go`): every node `run` writes
   `.ward/logs/<runID>_<nodeID>_<ts>.log` with cmd, exit code, elapsed, output.
   Bypasses the opaque db for human inspection. The write is FATAL: if the
   sidecar cannot be written, the node fails — verification proof is the core
   premise, so a run must never reach "completed" with missing evidence (no
   zombie states).
2. **`ward task show <id>`** (`internal/cli/task.go`): audit window — task
   metadata + run status + gate + exit code + last 15 lines of the sidecar.
   Evidence state is derived from sidecar-on-disk (`backed` vs `pre-evidence`).
3. **`tasks.last_run_id`** (migration v6→v7): maps a task to its most recent run
   so `show`/pre-close can locate evidence without guessing.
4. **Pre-close gate** (`taskRunCmd` and `taskDoneCmd`): when a task declared a
   gate, it only closes if a sidecar exists for the run and shows `exit_code ==
   0`. `ward task done --force` is the explicit, loudly-logged human override.
5. **Hard-reject exact no-op gates** (`taskAddCmd`): `true`, `false`, `:` are
   refused as `verify_cmd`/`run`. We do NOT shell-lint — `echo`/`echo && ...`
   are allowed (the agent's call). Empty gate remains a loud warning (manual work).
6. **Dossier tail** (`engine.WriteDossier`): the reject dossier now appends the
   failure-sidecar tail so a human sees *why* it failed, not just "failed 2x".
7. **AGENTS.md**: protocol forbids no-op `verify_cmd`; adds a GitHub CI bridge
   snippet so high-tier verification polls immutable CI instead of the agent's shell.

## Deliberately NOT done (learned from review)
- No `runs.evidence` DB column: it created state schizophrenia (DB branding vs
  disk truth) and retroactively shamed historical work. Evidence is computed
  from sidecar-file presence at read time.
- No phantom detection beyond exact no-ops: shell-linting `echo` produced false
  positives (`echo building && go test`). Outcome-level enforcement (the
  pre-close gate) is the real guarantee.
- `ward task done` is gated too, with `--force` as the only escape hatch, so the
  transparency guarantee is not optional/circumventable.
