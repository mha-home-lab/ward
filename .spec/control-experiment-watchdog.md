# Spec: deterministic experiment watchdog (no deadlock on unreachable gates)

## Purpose
H2 experiment gate requires "2 consecutive successful opencode smokes" — external dependency, never met in 6 days. Experiment deadlocks: claimed, gate unmet, no fallback, no timeout. Experiments need **internal, deterministic gates** and a **watchdog** that auto-escalates stalled experiments.

## Signals (what good looks like)
- `.spec/simulation.md` gate changed to internal deterministic check: `go test ./... -race` (or similar).
- `ward experiment watchdog --check`:
  - Scans all claimed experiments.
  - If `exp.Status == 'claimed' AND time.Since(exp.ClaimedAt) > 24h`:
    - Executes the experiment's gate command.
    - If gate fails → resets experiment to `open`, logs alert.
    - If gate passes → experiment proceeds (auto-approve next step).
- `ward experiment watchdog --auto` — runs watchdog logic, suitable for cron/CI.
- Alert output: `experiment <id> stalled >24h, gate failed → reset to open`.

## What's kept / changed
- **New**: `internal/experiment/watchdog.go` — `CheckExperimentHealth(ctx, exp) -> (alert, error)`.
- **New**: `cmd/experiment.go` — `experiment watchdog [--check] [--auto]`.
- **Changed**: `.spec/simulation.md` — gate = `go test ./... -race` (deterministic, fast, internal).
- **Kept**: Experiment state machine, falsifier pre-declaration, `RunExperiment` logic.

## Deliberately NOT built
- No complex watchdog scheduling — `--auto` flag enables cron/CI integration.
- No human-in-the-loop approval for reset — reset is automatic; human sees it in `brief`.

## Verification gate
```bash
# Unit
go test ./internal/experiment/... -run TestWatchdogEviction -v

# E2E
# 1. Create experiment with gate that fails
# 2. Claim it, wait (or mock time)
ward experiment watchdog --check
# Expected: experiment reset to open, alert logged
```