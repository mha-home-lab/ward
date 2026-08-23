# runner seam — Draft

| | |
|---|---|
| Status | **Draft** — deferred design, deliberately unbuilt |
| Domain | adapter / execution |
| Origin | openai.md public review (#7): "the adapter is currently an opencode orchestrator with an abstract router on top" |

## Purpose
Ward routes provider-independently (cheap/mid/strong) but executes every
prompt node through one binary (`opencode`) with hard-coded model names. The
router's abstraction is not matched by an execution abstraction. A real Runner
seam would let the same pool/routing/verification control plane drive codex,
claude, gemini CLI, plain shell, or any future worker — making Ward a
coordinator OF agents rather than a wrapper around ONE agent harness.

## Signals
- A second execution backend is actually wanted (e.g. driving codex against
  the same pool).
- Model names change under a stable tier contract (mapping churn).
- Fleet lanes need heterogeneous workers (openai.md P2 experiment).

## Design sketch
1. `Runner` interface: `Run(req ExecutionRequest) (ExecutionResult, error)`
   where ExecutionRequest carries repo root, prompt, tier, and timeout hints.
2. `opencode` becomes one implementation; `shell` already effectively exists
   for `run:` nodes and should be expressible through the same seam.
3. Tier→worker mapping moves to configuration (store table or config file),
   not compiled constants.
4. Ward never spawns a competing agent loop implicitly: the worker binary is
   chosen per lane/task, visible in `explain` output.

## What's kept
Router purity untouched (Runner selection happens after Route). The trust
boundary applies to every Runner equally. No behavior change for existing
single-backend users.

## Open questions
- Does `run:` (shell) unify with Runner, or stay a separate node kind?
- Where does tier→model config live so stores stay portable?
- Is per-node runner choice needed, or per-workflow/per-lane?
