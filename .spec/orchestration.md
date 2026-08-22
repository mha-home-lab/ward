# orchestration — DAG, Approvals, Resume (ciao-derived)

| | |
|---|---|
| Status | Implemented (v0.5: task workflows, reject dossier) |
| Domain | orchestration |
| Version | 0.5.0 |

## Purpose

Execute multi-step agent workflows as a DAG with approval gates and restart-
safe run state, ported from ciao. WARD keeps ciao's execution model but moves
durable state into the unified SQLite store (storage.md) and delegates model
selection to the router (routing.md) instead of a hard-coded provider.

## What's kept from ciao

- **DAG node kinds**: `channel` (agent/skill work, optionally `wait`),
  `approval` (halt for human decision), `test` (run a skill script, fail→
  `rejected`).
- **`BuildIndex` / `Validate`** (`graph.go`): acyclic check, exactly-one
  start node (indegree 0), `Next`/`Prev` maps, topological traversal.
- **Channel-based agent I/O** (`engine.ProcessAgent`): an agent reads the next
  `pending` item from its input channel, runs its skill(s), writes result to
  its output channel, marks input `processed`, advances the run.
- **Approval gates**: engine creates an `approval` item, sets status
  `awaiting_approval`, prints `approval_id`; `approve`/`reject` resume/reject.
- **Crash recovery / resume**: a run can be re-driven from its persisted
  `current_node` + `completed_nodes`. `workflow resume <run_id>` (and automatic
  resume on next invocation if a run is `running`).
- Run **history events** (`RunEvent`) appended at each transition for audit.
- **`wait`** semantics: a channel node can block advancement until its emitted
  item is `processed`.

## What's changed and why

- **State medium: YAML files → SQLite.** ciao stores runs under
  `.ciao/state/runs/<id>.yaml` and channel items under `.ciao/channels/<name>/
  <nnnn>.yaml`. WARD stores them in `runs` / `channel_items` tables
  (storage.md). Reason: one transactional store lets the router join memory +
  run state in a single query, and WAL mode gives the same restart-safety
  without N file writes per advance. `SaveRun` becomes an `UPSERT`; `LoadRun`
  a `SELECT`.
- **Model selection via router, not hard-coded.** ciao's `skillrunner/llm.go`
  calls `gemini-2.0-flash` directly. WARD's executor asks the router for
  `{tier, model}` per node (routing.md) and calls the provider through a
  pluggable client. The agent definition's `model` field becomes the router's
  *hint/default*, not a constant.
- **Ceremony coupling.** The engine reads the `ceremony_level` computed from DAG
  contention (flow.md step 6) to decide whether approval gates are mandatory and
  whether artifact writes go through full lifecycle. ciao had no such notion.
- **Git snapshot dropped as a run-log.** ciao called `git.Snapshot` on every
  state change. WARD keeps git only as an *optional verification backend*
  (verification.md), not as orchestration noise.
- **Agent I/O stays channel-based** but channel items are now DB rows, so
  "dedup on resume" is a `SELECT ... WHERE run_id = ?` instead of a file scan.
- **Task workflow generation (v0.5).** `orchestration.TaskWorkflow(taskID,
  title, kind, run, verifyCmd)` builds a runnable single-node DAG
  (`start → work → done`) from a dispatch-pool task; `Workflow.Save` writes YAML
  and re-validates by loading it back (a saved workflow must be runnable). The
  work node carries the task's `run:` so execution, routing, and auto-capture
  behave exactly as for hand-authored workflows. This is the bridge that lets a
  pulled pool item reach the engine without YAML authoring (broker.md §4).
- **Reject dossier (v0.5).** When the escalation budget is spent — both the
  in-loop path (`failNode`) and the routing-reject path — the engine writes a
  store-local accepted artifact (`kind: error`, tags `dossier` +
  `reject:<runID>`) synthesized ONLY from evidence already collected: the tier
  path from persisted `routing_decisions` (with verified context ids), and the
  per-attempt transcript from `run_events`. It never runs new commands or
  invents diagnosis. The human is the final tier and receives the same evidence
  packet the router had. Read back via `ward reject <run>` / `ward explain`.
  **Invariant:** the dossier must NOT carry the bare node id tag — an accepted,
  local, node-tagged artifact would count as a memory hit for that node's
  future runs, turning a failure transcript into fake knowledge. Covered by
  `TestRejectDossierAndExplain`.
- **Event log reader.** `Store.LoadEvents(runID)` returns ordered audit events;
  `ward explain <run> [node]` joins decisions + events + live re-read context
  status into one evidence chain (observer only — never feeds routing).

## Node semantics (carried)

- `channel`: emit `work_item` to `node.Channel` (or `node.Name`); if `wait`,
  pause until `processed`; then advance to ready successors (all predecessors
  `completed`).
- `approval`: ensure one `approval` item per run; if not `processed`, set
  `awaiting_approval` and return (persist). On approve, advance.
- `test`: run `.ciao/skills/<node>.sh` (or SQLite `skills` row); non-zero →
  `rejected`; record `test_passed`/`test_failed`/`test_skipped`.

## Open questions / risks

- **Channel item representation.** ciao used one YAML file per item with a
  monotonic filename. In SQLite, `channel_items` uses a synthetic `id`
  (`run_id`+counter). Does any consumer rely on filename ordering? Proposal:
  order by `created_at`/`id`; no filename dependency. Open.
- **Automatic resume on boot.** Should `ward` auto-resume an in-flight `running`
  run when invoked, or require explicit `ward resume`? ciao required explicit
  `resume`. Proposal: explicit `resume` for safety, but `ward run` detects a
  stale `running` run and warns. Open.
- **Skills as shell scripts vs. in-DB.** ciao keeps `.sh` under `.ciao/skills`.
  WARD can keep files (simpler, executable) and store a cached copy in the
  `skills` table for portability. Keep file-based execution; DB is cache.
- **Contention score source.** How exactly to compute shared-state contention
  for ceremony scaling — from workflow def file-declarations, from git, or
  agent-declared touched-sets? (See storage.md `node_state` cache.) Open.
- **How a node's touched file-set is discovered (explicit).** The contention
  score — and therefore ceremony scaling — is meaningless without knowing which
  files each node reads/writes, but *neither* ciao nor the workflow YAML declares
  this. Concrete options: (a) statically parse the agent/skill scripts for
  referenced paths, (b) use `git` to diff the worktree before/after each node,
  (c) require agents to declare a `touched` list in their output item. This is
  the missing input to the entire ceremony-scaling mechanism (routing.md is the
  consumer) and must be resolved before `full` ceremony can be enforced rather
  than advisory. Picked orchestration.md as the home for this question because
  the file-set is produced during execution; routing.md only consumes the score.
- **`watch` daemon** (ciao auto-processes pending items) — keep as `ward watch`
  but must respect the router's model choice and concurrency limits. Deferred
  priority, not blocked.
