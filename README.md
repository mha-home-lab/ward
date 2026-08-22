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

# A FAILED `run:` command is first-class: the node is marked failed (not done),
# escalation bumps, and the SAME node is re-routed at the next tier until the
# budget (2) is spent -> run rejected. No silent success.
ward run start --workflow workflows/fail-demo.yaml --auto-approve
#   work -> failed; run rejected after 3 escalating attempts.

# A real `go test` adapter + grep-verify, then resumed in a SECOND session.
# The run persists its originating workflow file, so resume needs no --workflow.
ward run start --workflow workflows/go-test-demo.yaml          # pauses at review
ward run resume <run_id> --auto-approve                        # second session
#   verify -> tier=cheap model=gemini-2.0-flash context=["<verified id>"]
#   (context is the verified artifact id only, never exec stdout)

# Result capture (flow.md step 7): a successful `run:` node auto-writes a
# store-local accepted artifact (tag = node id) on run/resume, so the NEXT
# session can route it cheap without hand-typed YAML. `ward capture` does the
# same for any node, or inspect what was written.
ward run start --workflow workflows/go-test-demo.yaml --auto-approve   # auto-captures
ward capture --run <run_id>                                        # or capture explicitly

# Model execution (the hands): a node with `prompt:` drives a model at the
# routed tier via opencode's free models — cheap->opencode/hy3-free,
# mid->opencode/mimo-v2.5-free, strong->opencode/nemotron-3-ultra-free.
# Routing decides the tier; the adapter translates it to a real model call.
ward run start --workflow workflows/agent-demo.yaml --auto-approve
#   implement -> opencode runs the prompt at the chosen tier; verify runs go test


# Maintenance: re-verify everything live and report drift.
ward tick
```

> **Schema migrations.** The store opens idempotently: it applies additive
> `ALTER`s (never a silent rewrite) up to `PRAGMA user_version`, so a database
> created by an earlier build keeps working. Each run also persists its
> originating workflow path, so `ward run resume` / `ward run approve` in a new
> process reload the correct workflow without re-supplying `--workflow`.

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
>
> **`memory put` is guilty by default (D0.3).** An artifact written via `put` is
> **not** store-local unless you explicitly cross the trust boundary
> (`ward memory put --local`, or `--by human`). Without it, its `verify_cmd` is
> never executed by `verify`/`tick`/`route`, so an agent cannot gain silent code
> execution by writing a malicious memory entry. Auto-captured artifacts (written
> by `ward run`/`capture` from this store's own successful work) and `router
> --seed` artifacts remain store-local, because they are WARD's own work product.
- `internal/orchestration` — DAG load/validate + engine. Before every route it
  runs `verification.Run` **live** against the repo and persists the result; only
  `verified` counts as a memory hit. Persists agent-declared `touched` sets and
  logs declared-vs-git-diff as an *observation only* (never a routing input).
  Nodes may carry a `run:` shell command that the engine executes (real adapter).
- `internal/cli` — `init`, `memory` (put/get/search/list/promote/supersede/
  handoff/context/stale/claim), `verify` (`--all`, `--trust`), `route`, `router`,
  `run` (start/status/approve/resume), `tick`, `doctor`, `workflow`. Every command
  supports `--json` and emits one-line errors.

  - `ward memory context <query>` — chef's compact injection block: ids, kind,
    summary, tags, verify_status (no full content).
  - `ward memory stale [--days N] [--mark <id>]` — surface stale/error/unknown
    artifacts (and rarely-used ones with `--days`); `--mark` sets one stale.
  - `ward memory claim add <topic> [--by a] [--ttl m] [--strict]` — advisory
    topic reservation (no locking); warns on overlap, errors with `--strict`.
    `claim release <topic>` / `claim list` manage active claims.

The OIDC-login workflow ships as a **linear** DAG used to exercise the router
end-to-end (including the live-verify gate against `README.md`); it does not
stand in for a full agent run. `ward router --auto-approve` prints the routing
measurement: how often cheap+verified succeeded, how often it escalated, and
whether stale/error artifacts were caught before a wrong route.
