# WARD

**Verify-gated model routing for local coding agents.** WARD routes each unit
of work to the cheapest model that can do it correctly, using *verified prior
knowledge* as the routing signal. A claim only votes for the cheap tier when it
is both memory-resident **and** live-verified against real repo state.
Unverified, stale, or imported artifacts count as a memory MISS and route to a
stronger tier.

Thesis in one line: **never re-solve a solved problem, and never trust a stale
claim.**

## Quickstart

```bash
go install github.com/mha-home-lab/ward@latest   # or: make && make install
ward init                                        # 1. store + agent protocol
ward brief                                       # 2. what matters right now
ward task add "fix login redirect" --tier mid --run "go test ./..."   # 3. pool work
ward task next --by agent-1 && ward task run task-xxxx                # 4. do it
ward tick --heal                                 # 5. close the knowledge loop
```

WARD is a single Go binary; state lives in a SQLite store (`.ward/ward.db`
under `WARD_HOME`, defaulting to your home config dir).

`init` is self-consulting by default: it writes a marker-delimited protocol
block into `AGENTS.md` (and refreshes existing `CLAUDE.md`/`GEMINI.md`), so
every future agent session is briefed without a human repeating the rules.
Opt out with `--no-agents-md`. `init --scaffold` writes a runnable
`workflows/default.yaml` into **your project** (it is not a file ward ships).

## Concepts

- **The brain** — every project gets a SQLite store of *artifacts*: captured
  results, procedures, field notes, critiques. Artifacts carry a `verify_cmd`;
  only **store-local** ones ever get executed (the trust boundary — an agent
  cannot gain code execution by writing a malicious check). A claim that fails
  its live re-verification is stale: it can no longer vote cheap and `--heal`
  supersedes it outright.
- **The oracle** — verified knowledge in the brain is the routing oracle. The
  router is a pure function: `{memory hit, verify status, contention, node
  kind, escalation}` → `{tier, model, ceremony}`. It never calls a model and
  never stamps a status; verification runs live before each route.
- **The pool** — claimable work items (`ward task …`) with tier-floor
  admission control. An agent's `--max-tier` budget is a ceiling; failure bumps
  a task's floor one tier so stronger agents pick it up; past `strong` it stops
  for a human with a dossier — never looped. Exclusive topic claims
  (`memory claim add`) keep parallel agents out of each other's way.
- **Chips** — compiled views of the brain (`ward skill pack`) formatted as
  agent-loadable `SKILL.md` files. Chips are derived artifacts: regenerate,
  never hand-edit. `skill check` reports staleness when sources drift;
  `skill-sync` pushes `portable:<topic>` chips to your global skills directory
  so any project benefits.

Tiers: **cheap** (verified prior result exists) · **mid** (default on a miss)
· **strong** (declared floor or real DAG contention) · **rejected** (escalation
budget spent → human dossier).

Two honest boundaries, stated plainly:

- **Routing changes who executes, not what the worker knows.** A verified hit
  routes work to a cheaper model; it does not inject prior output into that
  model's context (auto-injection invites anchoring and prompt-injection).
  Knowledge travels through pull surfaces instead: `brief` pointers,
  `memory context`, and compiled chips.
- **The protocol is instruction-following, not mechanical enforcement.** What
  *is* enforced in code: atomic claims (unique index), the trust boundary
  (`verify_cmd` runs only for store-local artifacts), verify-gated completion,
  and workflow-drift refusal on resume. Everything else in `AGENTS.md` relies
  on the agent actually reading it — a real limit, named rather than hidden.

## Reference

| Command | Purpose |
|---|---|
| `ward init [--scaffold\|--docs\|--no-agents-md]` | create the store; inject the agent protocol |
| `ward brief [topic] [--compact]` | session bootstrap: live re-verify, expired claims, knowledge, pool, next actions |
| `ward task add \| next \| run \| take \| drop \| list \| done \| fail \| workflow` | the dispatch pool |
| `ward memory put \| get \| search \| list \| promote \| supersede \| handoff \| context \| stale` | the agent memory store |
| `ward memory claim add \| release \| list` | exclusive topic reservations (hard conflict error) |
| `ward verify <id> [\|--all]` | run an artifact's verify_cmd live (store-local only) |
| `ward route <node>` / `ward router` | introspect the pure router / measure a real slice |
| `ward run start \| status \| approve \| resume` | workflow lifecycle |
| `ward capture --run <id>` | write a verified claim for completed nodes |
| `ward explain <run> [node]` | reconstruct a routing decision's evidence chain |
| `ward reject <run>` | show the reject dossier for an exhausted run |
| `ward timeline [-n N]` | unified activity stream: spans, transitions, captures |
| `ward wave <topic> [--heal]` | regression wave: re-verify everything tagged `<topic>` |
| `ward fleet <store-dir>...` | aggregate telemetry across stores (read-only) |
| `ward scorecard` | engineer performance from pool outcomes |
| `ward skill pack \| check`, `ward skill-sync` | compile/audit/push agent skill chips |
| `ward harvest` | R&D telemetry spine |
| `ward tick [--heal]`, `ward doctor` | maintenance sweeps, health checks |

Every command takes `--json` (valid JSON always; empty collections are `[]`,
never `null`), accepts `-n` wherever `--limit` exists, prints one-line errors,
and documents itself via `--help` examples.

### Verification kinds

`shell`, `build`, `test`, `grep pattern::path`, `hash algo::path`,
`golden expected-file::command` (diffs output against a checked-in file).
All kinds execute only for store-local artifacts.

## Ops

### Regression waves

Tag related work with `topic:<name>`; captures compound automatically across
tasks sharing the tag. Re-prove everything at once:

```bash
ward wave topic:auth            # report drift
ward wave topic:auth --heal     # supersede drifted claims
```

Waves are the standing proof that tagged knowledge still holds — run one
before claiming done.

### Execution semantics

- **`ward task run` is at-least-once.** If an agent dies mid-task, its claim
  can be taken (`task take`) and the work re-executed. Write `run:`/verify
  commands to tolerate re-execution (prefer checks over mutations; make
  mutations idempotent).
- **Runs refuse workflow drift.** Each run persists a semantic hash of its
  definition at start; `run resume`/`approve` against a mutated YAML file is
  refused unless you pass `--allow-drift`.
- **State transitions are checked.** The engine returns errors when run/node
  state or audit events fail to persist — a completed-looking run always made
  it to disk that way.

### Fleets (parallel engineers)

Small greenfield + strong solo model? **Work solo** — measured 2.3× faster at
small scale (R12/R13). Fleets pay when work exceeds one session's context,
lanes are independent, or capability tiers must mix. Launch through the
supervisor, never detached polling:

```bash
scripts/fleet-launch REPO_DIR SPEC_FILE
# SPEC lines:  <lane-name>|<model>|<prompt-file>
# verdict table + REPO/.fleet/<lane>.exit markers; blocks on all children
```

Takeover prompts stay surgical: one job, zero pool commands.

### Multi-project estates

```bash
ward fleet ~/play/a/.ward ~/play/b/.ward    # one row per store
ward skill-sync                             # push portable chips globally
```

## Deep truth & history

Design specs live in `.spec/`; release history and session findings in
`.arch/tasks.md`. Schema migrations are idempotent (`PRAGMA user_version`);
databases from earlier builds keep working.

> Node `run:` commands execute verbatim via `sh -c` in the repo root with no
> sandboxing — this is a local CLI; point it only at repos and commands you
> trust.
