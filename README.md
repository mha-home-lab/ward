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

# Seed a verified, accepted solution for the "implementation" node.
ward router --seed --auto-approve
#   implementation -> tier=cheap model=gemini-2.0-flash  (cheap+verified)
#   cheap+verified success : 1

# Now seed a STALE accepted artifact instead.
ward router --seed-stale --auto-approve
#   specification -> tier=mid (stale caught before a wrong route)
#   stale/unknown caught   : 1

# Apply the pure router by hand:
ward route impl --kind test --memory-hit --verify-status verified
#   impl -> tier=cheap model=gemini-2.0-flash
ward route impl --kind test --contention
#   impl -> tier=strong model=gpt-4o ceremony=full
ward route impl --kind test --escalation 3 --memory-hit --verify-status verified
#   REJECT: escalation budget exhausted (max 2)

# Run the OIDC-login workflow as a real DAG:
ward run start                              # pauses at the first approval
ward run approve <run> approval
ward run resume <run>                       # pauses again at review, then completes
```

## What is built (v1 vertical slice)

- `internal/store` — SQLite schema (per-node `run_nodes`, not whole-run blobs),
  FTS5 search with term-drop relaxation, artifact lifecycle.
- `internal/routing` — the **pure** router (no LLM call): `{memory hit, verify
  status, contention, node kind, escalation}` → `{tier, model, ceremony}`.
- `internal/verification` — executes `grep/build/test/hash/shell` **only** for
  store-local artifacts (imported = unknown until explicitly trusted).
- `internal/orchestration` — DAG load/validate + engine that persists
  agent-declared `touched` sets and logs declared-vs-git-diff as an
  *observation only* (never a routing input).
- `internal/cli` — `init`, `memory` (put/search/list/promote/handoff),
  `verify`, `route`, `router`, `run` (start/status/approve/resume), `doctor`,
  `workflow`. Every command supports `--json` and emits one-line errors.

Run `ward router --auto-approve` to see the routing measurement for the OIDC
workflow: how often cheap+verified succeeded, how often it escalated, and
whether stale artifacts were caught before a wrong route.
