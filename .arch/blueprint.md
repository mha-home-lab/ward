# WARD — Architecture Blueprint

## Problem statement

WARD is a from-scratch rewrite that unifies the best ideas from `chef` (Python, agent memory/knowledge store) and `ciao` (Go + Cobra, filesystem-backed workflow orchestrator) into a single Go + Cobra + SQLite CLI. The value proposition is not running both side by side, but building a single system where verified prior knowledge directly gates model-routing decisions: a classifier routes each unit of work to the cheapest model capable of doing it correctly, using chef-style prior-knowledge search as one signal (known pattern + verified state → cheap model; novel or unverified → escalate). Three invariants anchor the design:

1. **Never re-solve a solved problem** — chef's dedup/memory role prevents rediscovery of already-captured knowledge.
2. **Never trust a stale claim** — verification against real repo state (grep/build/test/hash) is a hard precondition for routing, not a nice-to-have.
3. **Ceremony scales with actual concurrency** — the propose→promote→supersede lifecycle is genuinely valuable for concurrent multi-agent work touching shared state, but overhead-negative for solo/sequential work.

These invariants close the failure modes documented in chef's qwen-auth feedback: the verify gap ("status said 'next: add input validation' but validation was already written, just not wired in"), ceremony scaling badly with agent count, no structured home for "attempted but incomplete and why", and over-engineered kind taxonomy for small projects.

## High-level component map

| Layer | Owned natively | Ported from chef | Ported from ciao | Left out & why |
|-------|---------------|-----------------|-----------------|----------------|
| **SQLite store** | `.ward/ward.db` (single file, all runtime state) | Artifact schema, dedup/hash IDs, project lens column | — | ciao's filesystem YAML trees (.ciao/channels, .ciao/state/runs) cannot support routing queries/joins needed by the classifier; SQLite is a superset that also gives crash safety via WAL mode |
| **Cobra CLI** | `ward` command tree (init, task, run, know, verify, gap, route, doctor, handoff, resume, tick) | Command verbs preserved deliberately: put/propose/promote/supersede/search/get/context/stale (from chef) | Workflow defs stay YAML; approve/reject gates; resume crash recovery (from ciao) | MCP TCP server (chef), TUI dash, watcher daemon, pipeline auto-approve (ciao — anti-pattern vs explicit approval gates), git worktree backends |
| **Workflow definitions** | — | — | YAML under `.ward/workflows/*.yaml` with nodes (step|approval|verify), edges; Kahn's validation (one start, acyclic) | ciao's single CurrentNode linearization (no real DAG parallelism); WARD's DB-backed run_nodes enable per-node state machines and fan-out width queryable for the router |
| **Router/classifier** | Designed natively: signals, tiering, escalation rules | — | — | — |
| **Verification** | `verifications` table, structural not opt-in (closes qwen-auth gap) | `--verify` on propose/put, `chef verify` ✓/✗/⚠, tick, handoff --incomplete | — | chef's opt-in-only verify + cached results — WARD makes verification a routing gate |
| **DAG/execution** | DB: runs + run_nodes (one row per node → pending/ready/running/awaiting_approval/done/failed/skipped) | — | ciao advance loop (advance engine, pending_nodes/completed_nodes); node kinds; approval gates; resume-from-persisted-state | ciao's whole-run blob (CurrentNode + PendingNodes slices) — WARD replaces with per-node rows enabling fan-out width query |
| **Memory/knowledge** | Artifacts in SQLite w/ verify contracts, gap table, tiered taxonomy | 5 core artifact kinds + project lens; propose→promote→supersede | — | chef's 8 kinds + full lifecycle machinery for every scale task — WARD core 4 + extended opt-in per project; gap is its own table |
| **State machine** | — | — | — | chef's constant-cycle propose→promote→supersede per run — WARD's ceremony mode (solo|coordinated) derived from live topology (active run registry, DAG fan-out, claim conflicts) |

## Explicit non-goals for v1

- No native LLM calls or provider SDKs in the core binary (router decides; adapters execute; v2 may add native providers)
- No vector/embedding search (FTS5 + BM25 like chef v0.5.2; keep it simple and stdlib-proven)
- No multi-host or multi-user auth, no server daemon (single-user local CLI like chef + ciao)
- No auto-promotion ever (anti-noise principle inherited from chef v0.1)
- No parallel remote execution or sandboxing of adapters beyond local exec timeout
- No GUI or TUI (folded into --json and `ward doctor` / `ward stats`)
- No cross-repo global sync beyond the project lens (single-store-with-lens = sufficient for now)
- No auto-resolution of incomplete work (structured gap table surfaces it; next-action is a field, not auto-computed)
- No MCP server, export/import, workspaces/git-worktree subsystems (deferred to v2; not core to routing)
- No TTL expiry auto-supersede (claims keep TTL; artifact TTL deferred until schema stabilizes)
- No enforcement of claims as locking (advisory only, like chef coordination-001; agents must voluntarily check)

## References

- chef `.specs/architecture.md` — objective, principles, success criterion; spec index convention (blueprint.md, tasks.md, roadmap.md, per-domain `<domain>-NNN.md` with header table `Status|Domain|Version` + `Problem/Requirements/Design/Acceptance Criteria/Non-Goals/References`)
- chef `.specs/session-protocol-003.md` — verify gap design: `--verify`, `chef verify` ✓/✗/⚠, tick, handoff `--incomplete`, resume integration
- chef `AGENT_FEEDBACK.md` (qwen-auth) — four failure modes: verify gap, ceremony scaling, no structured incomplete home, over-engineered taxonomy
- chef `.specs/coordination-001.md` — claims advisory, YAML headers for project status
- ciao `internal/engine/engine.go` — advance loop, approval gates, resume crash recovery
- ciao `internal/workflow/graph.go` — DAG validation: unique names, no self-loops, exactly one start node, acyclic via Kahn's
- ciao `internal/model/types.go` — RunState, WorkItem, WorkflowNode, RunStatus, AgentDefinition