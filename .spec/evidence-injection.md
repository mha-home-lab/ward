# Spec: verified-artifact evidence injection (close the "routing ≠ knowing" gap)

## Purpose
External review (openai.md, P1 #8) named the single biggest product weakness:
WARD persists verified knowledge and uses it to *route* a node to `cheap`, but the
executing agent still receives only the original `node.Prompt` — so it re-derives
the already-verified solution. "Never re-solve a solved problem" was true at the
routing layer only, not at the knowledge layer.

This makes the verified artifact *available* to the worker as structured evidence
when a prompt node has a live-verified memory hit. Per the same review and the
claude context review, injection is **optional/available**, not blind — the agent
still decides whether to lean on it; we never auto-apply prior model output as fact.

## Signals (what good looks like)
- A prompt node that routes with `memory_hit=true` and a local verified artifact
  receives a delimited `VERIFIED PRIOR CONTEXT` block appended to its prompt,
  containing the artifact id + summary (+ content for store-local artifacts).
- The trust boundary holds: only `Local == true` artifacts have their content
  injected. Imported (non-local) artifacts remain routing signals only — content
  is never handed to the worker (mirrors `verify.Run`'s `if !a.Local` guard).
- The routing decision `context` column still records the verified ids (provenance
  for `explain`/dossiers) — unchanged.
- No model in the loop (pure `run` nodes) is unaffected: evidence is only appended
  to `node.Prompt` paths.

## What's kept / changed
- `Engine.memoryHitForNode` now returns the verified `[]store.Artifact` (not just
  ids); the engine builds `ctxJSON` ids from it. Callers updated.
- New pure helper `buildEvidenceBlock(verified []store.Artifact) string`:
  returns "" when empty; otherwise a fenced block with one line per local artifact
  (`id`, `summary`, `content` trimmed). Non-local artifacts are skipped.
- `Engine.stepNode` prompt path: `prompt := node.Prompt + buildEvidenceBlock(verified)`
  before `adapter.Run(...)`. `run`-only nodes unchanged.
- No new store table; no new CLI surface. `explain`/`dossier` already read the
  routing decision context, which still carries the ids.

## Deliberately NOT built (review-rejected)
- Auto-applying prior model output as the answer: anchoring + injection risk. We
  inject as *evidence the agent may extend*, clearly labeled "do not re-solve".
- Token-% budget tracking or NL `context query`: Ward cannot see the context window
  (claude review); fabricated numbers are ceremony-without-act.

## Open questions
- None blocking.
