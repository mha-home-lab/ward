# WARD

**Verify-gated model routing for local coding agents.** WARD routes each unit of
work to the cheapest model that can do it correctly, using *verified prior
knowledge* as the routing signal. A claim is only allowed to vote for the cheap
tier when it is both memory-resident **and** verified against real repo state.
Unverified, stale, or imported artifacts count as a memory MISS and are routed
to a stronger tier. Ceremony scales with actual DAG contention, not a constant.

Thesis, in one line: **never re-solve a solved problem, and never trust a stale
claim** — verification against the repo is a *routing precondition*, not a badge.

## 30-second demo

```bash
ward init                                   # create the sqlite store

# Verify is a LIVE gate, not a stored column. --seed greps README.md for "OIDC"
# (present -> verified -> cheap). --seed-stale greps for a pattern that cannot
# match (live failure -> NOT cheap). The router never stamps a status.
ward router --seed --auto-approve
#   implementation -> tier=cheap model=gemini-2.0-flash  (cheap+verified)
#   cheap+verified success : 1
ward router --seed-stale --auto-approve
#   specification -> tier=mid  (stale/error caught before a wrong route)
#   stale/unknown caught   : 1

# Apply the pure router by hand:
ward route impl --kind test --memory-hit --verify-status verified
#   impl -> tier=cheap model=gemini-2.0-flash
ward route impl --kind test --contention
#   impl -> tier=strong model=gpt-4o ceremony=full
ward route impl --kind test --escalation 3 --memory-hit --verify-status verified
#   REJECT: escalation budget exhausted (max 2)

# A real DAG with contention: two unordered siblings share a file.
ward router --workflow workflows/parallel-demo.yaml --auto-approve
#   build-b -> tier=strong ceremony=full  (contends with done-sibling build-a)

# Run a workflow. Nodes with a `run:` command actually execute (real adapter);
# nodes without one just record a routing decision.
ward run start --workflow workflows/parallel-demo.yaml --auto-approve
#   run completes; build-a/build-b each ran their `run:` shell command

# Maintenance: re-verify everything live and report drift.
ward tick
```

## What is built (v1 vertical slice)

- `internal/store` — SQLite schema (per-node `run_nodes`, not whole-run blobs),
  FTS5 search with term-drop relaxation, artifact lifecycle, WAL.
- `internal/routing` — the **pure** router (no LLM call): `{memory hit, verify
  status, contention, node kind, escalation}` → `{tier, model, ceremony}`.
- `internal/verification` — executes `grep/build/test/hash/shell` **only** for
  store-local artifacts (imported = unknown until explicitly trusted via
  `ward verify --trust`).

> **Trust boundary (both `verify_cmd` and node `run:`).** `verify_cmd` executes
> only for store-local artifacts; imported artifacts are never executed. Node
> `run:` commands are run verbatim via `sh -c` in the repo root with no
> sandboxing — this is a local CLI, so only point it at repos and commands you
> trust. A malicious `run:` (or a `verify_cmd` like `OIDC::../../etc/passwd`)
> has the same local-file access as the user running `ward`.
- `internal/orchestration` — DAG load/validate + engine. Before every route it
  runs `verification.Run` **live** against the repo and persists the result; only
  `verified` counts as a memory hit. Persists agent-declared `touched` sets and
  logs declared-vs-git-diff as an *observation only* (never a routing input).
  Nodes may carry a `run:` shell command that the engine executes (real adapter).
- `internal/cli` — `init`, `memory` (put/get/search/list/promote/supersede/
  handoff), `verify` (`--all`, `--trust`), `route`, `router`, `run`
  (start/status/approve/resume), `tick`, `doctor`, `workflow`. Every command
  supports `--json` and emits one-line errors.

The OIDC-login workflow ships as a **linear** DAG used to exercise the router
end-to-end (including the live-verify gate against `README.md`); it does not
stand in for a full agent run. `ward router --auto-approve` prints the routing
measurement: how often cheap+verified succeeded, how often it escalated, and
whether stale/error artifacts were caught before a wrong route.
