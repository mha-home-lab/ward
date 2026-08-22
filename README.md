# WARD

**Verify-gated model routing for local coding agents.** WARD routes each unit of
work to the cheapest model that can do it correctly, using *verified prior
knowledge* as the routing signal. A claim only votes for the cheap tier when it
is both memory-resident **and** verified against real repo state. Unverified,
stale, or imported artifacts count as a memory MISS and route to a stronger tier.
Thesis in one line: **never re-solve a solved problem, and never trust a stale
claim** — verification against the repo is a *routing precondition*, not a badge.

## Install

```bash
go install github.com/mha-home-lab/ward@latest
```

WARD is a single Go binary; it keeps its state in a SQLite store (`.ward/ward.db`
under `WARD_HOME`, defaulting to your home config dir).

## Quick start

```bash
ward init                                # create the sqlite store
ward init --scaffold                     # also write workflows/default.yaml (runnable DAG)
ward run start --workflow workflows/default.yaml --auto-approve
```

`default.yaml` is a linear DAG: `start → test (run: go test ./...) → done`. The
`test` node runs `go test ./...`; if it passes, WARD captures the result as a
store-local, *verified* artifact tagged by the node id. The next time the same
node runs, that verified hit lets it route **cheap** instead of re-doing the
work. A failed `go test` is a first-class failure: the node is marked failed
(never "done"), escalates one tier, and is retried until the budget (2) is spent
— then the run is **rejected**. No silent success.

## Tiers: cheap, mid, rejected

- **cheap** — only when there is a *verified* prior result for the exact work
  (memory hit + verified). The cheapest model does the job; nothing is re-solved.
- **mid** — the default when there is no verified prior result: a memory miss, or
  an unverified / stale / imported artifact. The work is done at a competent tier.
- **strong** — when the node is declared `tier: strong`, or when real DAG
  contention (two nodes touching the same files) forces a stronger model with
  full ceremony.
- **rejected** — after two escalating failures the run is routed to a human, never
  looped forever.

## Trust boundary

`verify_cmd` (attached to a memory artifact) executes **only** for store-local
artifacts — artifacts this store authored itself. Imported or `put`-written
artifacts are **not** store-local by default (`ward memory put` is guilty until
you cross the boundary with `--local` or `--by human`), so an agent cannot gain
silent code execution by writing a malicious `verify_cmd` or `run:`. Node `run:`
commands execute verbatim via `sh -c` in the repo root with no sandboxing — this
is a local CLI, so only point it at repos and commands you trust. Auto-captured
artifacts (written by `ward run` / `capture` from this store's own successful
work) and `router --seed` artifacts remain store-local, because they are WARD's
own work product.

## Commands

- `ward init [--scaffold] [--docs]` — create the store; `--scaffold` writes
  `workflows/default.yaml`; `--docs` writes spec skeletons.
- `ward memory <put|get|search|list|promote|supersede|handoff|context|stale|claim>`
  — the agent memory store. `ward memory claim add <topic>` is an **exclusive**
  reservation: one active claim per topic+project; a conflict is a hard error.
- `ward route <node>` / `ward router [--workflow]` — introspect the router.
- `ward run <start|status|approve|resume>` — workflow lifecycle.
- `ward tick` — re-verify local artifacts live, free expired claims, report drift.
- `ward doctor` — store + environment health (including `legacy_claims`).
- Every command supports `--json` and emits one-line errors.

## Internals / demo

The router is a **pure** function: `{memory hit, verify status, contention, node
kind, escalation}` → `{tier, model, ceremony}`. It never calls a model and never
stamps a status — verification is a *live* gate run against the repo before each
route. To see it directly:

```bash
# Verify is a LIVE gate, not a stored column.
ward router --seed --auto-approve
#   implementation -> tier=cheap  (README.md contains "OIDC" -> verified)
ward router --seed-stale --auto-approve
#   specification -> tier=mid     (pattern cannot match -> NOT cheap)

# Apply the pure router by hand:
ward route impl --kind test --memory-hit --verify-status verified
#   impl -> tier=cheap
ward route impl --kind test --contention
#   impl -> tier=strong ceremony=full
ward route impl --kind test --escalation 3 --memory-hit --verify-status verified
#   REJECT: escalation budget exhausted (max 2)

# A real DAG with contention: two unordered siblings share a file.
ward router --workflow workflows/parallel-demo.yaml --auto-approve
#   build-b -> tier=strong ceremony=full

# Run a workflow with real shell execution + a FAILED run: to see escalation.
ward run start --workflow workflows/fail-demo.yaml --auto-approve
#   work -> failed; run rejected after 3 escalating attempts.

# A prompt node drives a free opencode model at the routed tier; verify runs
# the node's own run: as the verification check.
ward run start --workflow workflows/agent-demo.yaml --auto-approve

# Maintenance: re-verify everything live and report drift.
ward tick
```

> **Schema migrations.** The store opens idempotently: it applies additive
> `ALTER`s (never a silent rewrite) up to `PRAGMA user_version`, so a database
> created by an earlier build keeps working. Each run also persists its
> originating workflow path, so `ward run resume` / `ward run approve` in a new
> process reload the correct workflow without re-supplying `--workflow`.
