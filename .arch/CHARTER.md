# NEXT-SESSION CHARTER — "clean ecosystem tool"

Read this after `ward brief`. Mission: **consolidate**. Ward is capable and
proven on four repos; it is not yet *clean*. This session ships zero new
subsystems. Polish, test, document, release.

## P0 — release engineering
- Makefile: `build` (ldflags version stamp), `test`, `fmt`, `vet`, `install`.
  Kill the hand-built-binary-at-/usr/local/bin era.
- Tag v0.8.0. CHANGELOG.md distilled from tasks.md release history.

## P0 — command-surface audit (every subcommand, no exceptions)
- `--json` returns valid JSON always (empty = [], never null).
- Flag parity (`-n` vs `--limit`) consistent everywhere.
- One-line errors; help text has an Example line each.
- Sweep: timeline, wave, fleet, scorecard, skill pack/check, skill-sync,
  explain, reject, capture — these were built fast.

## P1 — tests for the fast-built surface
Table-driven tests against temp stores for every command above. The gate is
`make test` green + `gofmt -l .` empty + vet clean.

## P1 — README as product
Quickstart (5 commands) → Concepts (brain / pool / chips / oracle) →
Reference (command table) → Ops (fleet-launch, regression waves, sync).
Deep truth stays in `.spec/`; history stays in `.arch/tasks.md`.

## P2 — drafts only, NO code
Design docs (Draft status) for the three deferred ideas:
flake quarantine (needs verify-history schema), scoped topic-vouching
(bee2477e), outcome-driven trust re-grading (2092335d). Nothing else.

## Non-goals (remain tossed unless Mohamed reopens)
MCP substrate · federation · watch daemon · policy ceilings · cost accounting.

## How to work
- Dogfood everything on real stores: mdq / donate-fair / secure-bank pools
  are drained and healthy — regression waves (`ward wave topic:<t> --heal`)
  must stay green before claiming done.
- Parallel work goes through `scripts/fleet-launch REPO SPEC` (never bare
  nohup polling). Takeover prompts are surgical: one job, zero pool commands.
- Portable lessons get `portable:<topic>` tags + `ward skill-sync`.
- Close with `ward memory handoff` in every store you touched.

## Definition of done
Fresh clone → `make && make test && make install` → `ward init` in a throwaway
dir → `brief` → seed one task with a falsifiable check → `task run` closes it
with captured evidence → `wave topic:x` green → fleet view shows healthy
estates. If any step surprises you, that is the bug to fix before anything
else.
