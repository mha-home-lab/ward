# NEXT-SESSION CHARTER — "clean solo tool"

Read this after `ward brief`. Mission: **consolidate**. Ward is capable and
proven; it is not yet *clean*. This session ships zero new subsystems. Polish,
test, document, release.

Ward is a **solo** loop tool. Parallel agent dispatch (`ward fleet`,
`scripts/fleet-launch`) was built, measured against solo in R12/R13, and
**retired** — solo dominated at every scale tested (solo 19.9 min vs fleet
44.7 min, quality parity). The claim mutex still prevents two sessions from
grabbing the same task across resumptions, but there is no fleet, no estates,
no parallel dispatch.

## P0 — release engineering
- Makefile: `build` (ldflags version stamp), `test`, `fmt`, `vet`, `install`.
  Kill the hand-built-binary-at-/usr/local/bin era.
- Tag v0.8.0. CHANGELOG.md distilled from tasks.md release history.

## P0 — command-surface audit (every subcommand, no exceptions)
- `--json` returns valid JSON always (empty = [], never null).
- Flag parity (`-n` vs `--limit`) consistent everywhere.
- One-line errors; help text has an Example line each.
- Sweep: timeline, wave, scorecard, skill pack/check, skill-sync, explain,
  reject, capture — these were built fast and live as research commands, not
  the solo path.

## P1 — tests for the fast-built surface
Table-driven tests against temp stores for every command above. The gate is
`make check` green (`gofmt -l .` empty + vet clean + `go test ./...`).

## P1 — README as product
Quickstart (5 commands) → Concepts (brain / pool / chips / oracle) →
Reference (command table) → Ops (the solo loop: brief → task → tick →
memory handoff). Deep truth stays in `.spec/`; history stays in
`.arch/tasks.md`.

## P2 — drafts only, NO code
Design docs (Draft status) for the three deferred ideas:
flake quarantine (needs verify-history schema), scoped topic-vouching
(bee2477e), outcome-driven trust re-grading (2092335d). Nothing else.

## Non-goals (remain tossed unless Mohamed reopens)
MCP substrate · federation · watch daemon · policy ceilings · cost accounting
· parallel dispatch / fleet.

## How to work
- Dogfood everything on real stores: mdq / donate-fair / secure-bank pools
  are drained and healthy.
- The solo loop: `ward brief` → pull a task with `ward task next` → implement →
  prove it with `ward task run` (captures a sidecar as evidence) → `ward task
  done` (gated on that evidence) → `ward memory handoff`.
- Portable lessons get `portable:<topic>` tags + `ward skill-sync`.
- Close with `ward memory handoff` in every store you touched.

## Definition of done
Fresh clone → `make && make test && make install` → `ward init` in a throwaway
dir → `brief` → seed one task with a falsifiable check → `task run` closes it
with captured evidence → `task done` accepts it because the sidecar exists. If
any step surprises you, that is the bug to fix before anything else.
