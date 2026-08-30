# Spec: experiment watchdog (visibility only — no auto-reset)

## Honest framing: this is a process problem, not a missing subsystem

The previous version of this spec claimed WARD has an "experiment state machine",
"falsifier pre-declaration" and "RunExperiment logic" that it proposed to "keep".
That is false. There is **no experiment subsystem in code**:

- `internal/` contains `adapter, cli, observe, orchestration, routing, store,
  verification` — no `experiment` package.
- The only experiment artifact is `.spec/simulation.md`, a **human-run research
  protocol**: pre-declared falsifiers, two-arm design, standing metric battery,
  and a void rule ("contaminated or supervisor-interrupted runs are VOIDED and
  redone, never partially reported"). That is prose discipline enforced by a
  human supervisor, not a Go state machine with Status/ClaimedAt fields.
- The H2 stall was a **real event but a single documented one**: an opencode
  gateway outage on 2026-08-23 that made the pre-declared double-smoke gate
  fail twice. Per protocol the rig was never built, no tokens were wasted, and
  the claim was held for handoff (`.arch/tasks.md` H2 launch log). That is the
  protocol catching an environment failure — the control system itself, not a
  missing one.

So this spec does not fix a code defect. It adds optional process visibility
for a human-run protocol. Build it only if that visibility is worth ~net-new
code for you. That is an explicit judgment call, not a foregone conclusion.

## Policy decision required (do not skip)

An **automatic reset** of a stalled experiment would conflict with
`simulation.md`'s void rule: a supervisor currently decides whether a run is
VOIDED (contaminated, interrupted, or environment-faulted) — and a claimed
experiment may be stalled for legitimate reasons (gate momentarily down, rig
not yet built, claim held for the next session).

THIS SPEC DOES NOT PROPOSE CHANGING THAT POLICY.

It proposes **report-only** visibility: list stalled experiments with age,
gate, and last attempt, and surface the claims that should be re-evaluated.
A human (or the next session's `ward brief`) makes the reset/void decision.

If you want an automatic reset later, that is a separate, deliberate policy
change to the void rule — approve it on its own terms, never as a side effect
of a watchdog script.

## Purpose

For a human-run experiment protocol, keep stalled experiments from silently
rotting: a `ward experiment watchdog --check` command that reports any stale
claim (age, pre-declared gate, last attempt timestamp) so a supervisor can
decide: re-claim, void, or let be. Output only. No state mutation.

## Signals (what good looks like)

- `ward experiment watchdog --check`:

  ```
  STALLED EXPERIMENT CLAIMS
  task-798321b1  H2 solo continuity   claimed by ox-alpha  age=72h
      gate:  opencode run smoke pass x2 consecutive (unmet; last attempt 2026-08-23T21:00Z)
  ```

  Columns: task id, experiment name (from `.spec/simulation.md` or task title),
  claimant, age, gate summary, last attempt.
- Exit code 0 with "no stalled claims" when nothing is stale; non-zero listing
  when there are, so cron/CI can alert without taking action.
- A stall is: claim `status='claimed'` older than a `--min-age` (default 24h)
  whose claimed task predates or exceeds the experiment's documented gate.
- JSON output (`--json`) for machine alerting.

## What's kept / changed

- **The diagnosis, honestly**: this is net-new code, not an addition to an
  existing subsystem. There is nothing to "keep" because no experiment
  subsystem exists.
- **New** (if built): `internal/experiment/watchdog.go` — pure query + report
  (reads task claims from `internal/store`, joins gate text from task title/
  `simulation.md`; performs zero writes).
- **New**: CLI command under `internal/cli` — `experiment watchdog --check`.
  No new table required if we reuse the existing task/claim columns
  (`claimed_at`, `expires_at`, `status`) in `internal/store`.
- **Changed**: `.spec/simulation.md` — add one line noting stalled experiments
  are surfaced via `ward experiment watchdog --check` and that reset/void
  remains a supervisor decision. No protocol change.
- **Kept**: the void rule, pre-declared falsifiers, the H2 gate — untouched.

## Deliberately NOT built

- No `--auto` mode, no automatic reset, no auto-esc-salation. This report does
  not mutate experiment or task state. (The prior version proposed auto-reset
  on gate failure; that is dropped unless the policy decision above gets a
  real yes.)
- No new persistence table — the report reads existing claim columns.

## Verification gate

```bash
# Unit
go test ./internal/experiment/... -run TestWatchdogReport -v

# E2E
# 1. Create a task claim older than 24h (existing store API)
ward experiment watchdog --check
# Expected: stalls listed with age + gate; NO task/claim state changed
ward experiment watchdog --check --min-age 999h
# Expected: "no stalled claims", exit 0
# Assert working tree/store unchanged between runs (report is read-only)
```