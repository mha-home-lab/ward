# Spec: auto-plan / project-manager function for ward

## Purpose
Battlefield request (`portable:pm`, tags `ward,auto-plan,pm`): give ward an
autonomous sprint-planning / task-pool orchestration capability — take a goal or
a body of work and produce a structured, claimable task pool with sensible
tier floors, tags, and acceptance gates, rather than the human authoring every
`ward task add` by hand.

This is evaluated, not yet built. The reasoning below is why it is a *narrow*
primitive, not a full autonomous PM.

## Signals (what good looks like)
- `ward plan "<goal>" [--from <spec|dir|issue>]` produces N tasks in the CURRENT
  store (or `--project <name>`) each with: a concrete title, a tier floor, topic
  tags, and a NON-TRIVIAL `--verify-cmd`/`--run` (never `true`, per the
  neverphantom rule). The pool is immediately `ward task next`-able.
- Decomposition is deterministic and reviewable: the plan is a spec artifact
  (`ward doc`/artifact) so a human can veto before `task run` executes anything.
- Planning NEVER auto-executes: it only populates the pool. Execution stays the
  existing `task run` engine (routing, live verify, escalation). This preserves
  the trust boundary — the planner is a producer, not a runner.

## What's kept / changed
- New `ward plan` command in `internal/cli/` that:
  1. gathers context from the stated source (spec file, repo dir, or free text),
  2. decomposes into tasks (calls a model at `strong` tier via the existing
     adapter seam — no new execution path),
  3. stamps each task with the provenance/anti-phantom gate,
  4. writes them via the SAME `store.CreateTask` used by `task add` (so the
     `--project` routing + misplacement guard apply — a ward plan filed from
     project X still lands in ward's store when `--project ward` is given).
- No change to routing/verification/the engine. The planner is pure
  task-generation; the engine remains the only thing that executes.

## Deliberately NOT built (rejected, per prior reviews)
- **Autonomous execution loop / self-driving agent**: the openai.md review is
  explicit — Ward's value is the *control plane* (verified memory, coordination,
  escalation), not competing with the agent harness. `plan` must stop at a
  populated pool, not start running.
- **Context-window-aware planning**: Ward cannot see the LLM context window
  (claude context review). Any "plan until context is full" heuristic is
  unverifiable ceremony — rejected.
- **Natural-language planning from bare prose without a stated source**: too
  unconstrained; require an explicit source so plans are auditable.

## Open questions
- Decomposition quality: how much structure to demand in the model prompt before
  accepting a plan (a plan full of `true`-gated tasks is worthless). Gate:
  reject any generated task whose verify_cmd is trivial (reuse `isTrivialVerify`).
- Should `plan` also file a `spec` artifact capturing the decomposition rationale
  for later `explain`? Likely yes — keeps the plan auditable.

## Decomposition (implementation tasks, each with a real gate)
1. `internal/cli/plan.go`: `ward plan` command skeleton + flag parsing.
   Gate: `go test ./internal/cli/ -run TestPlan`.
2. Planner prompt + adapter call at `strong` tier; parse model output into tasks.
   Gate: fixture-based test asserting trivial-gated tasks are rejected.
3. Integration with `store.CreateTask` + `--project` routing so plans land in the
   intended store. Gate: `ward plan --project ward ...` writes to ward's store in
   a test.
