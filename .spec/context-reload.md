# Spec: mechanical context reload + mid-task checkpoint

## Purpose
Ward's memory thesis pays off only if prior knowledge is reloaded *mechanically*,
not by the agent remembering to run `ward brief` every time. Two code-confirmed
gaps (per consulting review):

1. `task run` builds the workflow and executes but never injects scoped prior
   knowledge — reload is 100% protocol-trust. Three tasks deep, an agent silently
   re-derives from scratch.
2. There is no mid-task checkpoint. `capture` only fires at task *close*, so a
   long task that is hard *within itself* has no sanctioned point to say "this is
   what I've learned; let me shed the raw exploration."

## Signals (what good looks like)
- `ward task next <id>` and `ward task run <id>` print, non-optionally, a
  "Prior knowledge (scoped to tags)" block sourced from `memory context` for the
  task's tags, and the "Latest checkpoint" block. Reload becomes structural.
- `ward task checkpoint <id> "<summary>" [--verify CMD]` records a partial
  capture WITHOUT closing the task; it is shown in `task show` and fed back into
  the next reload so the agent can discard raw work.
- The checkpoint's optional `--verify CMD` is executed and its exit code stored,
  but a non-zero exit does NOT block the checkpoint (it is a progress note, not a
  gate).

## What's kept / changed
- New `checkpoints` table (id, task_id, seq, summary, verify_cmd, exit_code, at).
  This is authored, mid-session state that does NOT exist on disk — so a table is
  correct (it is NOT the disk-derivable evidence anti-pattern from the
  transparency patch; don't conflate the two).
- `Store.ContextForTask(tags, limit)` returns artifacts matching ANY task tag via
  the existing `SearchArtifactsTagged` tag selector (no new free-text engine).
- `task next` / `task run` text output appends the scoped-context + checkpoint
  blocks. JSON (`task run`) gains `prior_knowledge` + `latest_checkpoint` fields.
- `task show` gains a Checkpoints section (text + JSON).
- `ward brief [topic]` already scopes prior knowledge by topic — no change needed
  there beyond what exists.

## What is deliberately NOT built (re rejected by the reviews)
- `ward context status` token-%: Ward cannot see the LLM context window; a
  fabricated percentage is exactly the "ceremony without act" failure mode.
- Natural-language `ward context query`: unverifiable retrieval; the existing
  FTS + tag selector already covers targeted lookup.
- `ward context switch` topic isolation: out of scope for a solo loop.

## Open questions
- None blocking. Checkpoint `--verify` semantics: record-and-continue (chosen),
  not gate (keeps it a lightweight offload primitive).
