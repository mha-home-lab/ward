# verification — Claims Checked Against Real Repo State

| | |
|---|---|
| Status | Implemented (v0.5: golden kind, tick --heal) |
| Domain | verification |
| Version | 0.5.0 |

## Purpose

Close the qwen-auth **verify gap**: a chef artifact is a *claim* that can drift
silently from the actual code ("next: add validation" while validation was
already written but unwired). WARD makes an artifact's claim **checkable against
real repo state** (grep / build / test / hash / git-diff) and, critically, makes
a passing check a **precondition for the router trusting the artifact**
(flow.md step 3; routing.md signal #2).

## What's kept from chef

- `verify_cmd` / `verify_status` / `verify_at` columns on artifacts (chef
  v1.0.0, session-protocol-003).
- `ward verify [--project X]` runs each artifact's `verify_cmd` and reports
  ✓ (`verified`) / ✗ (`stale`) / ⚠ (`error`), surfacing which artifacts are
  stale.
- `--verify "<cmd>"` attaches a check at write time (`put`/`propose`).
- Resume shows cached `verify_status` (✓/✗/⚠) on context artifacts (chef
  render_resume).

## What's changed and why (the concrete gap closure)

chef treats verify as a **post-hoc display**: `verify` reports status, but
routing/search still trust the artifact regardless. WARD changes the trust
model:

1. **Verify is a routing precondition.** Before the router consumes a memory
   artifact as a "known pattern" signal, WARD requires `verify_status =
   verified` against *current* repo state. `stale` / `error` / `unknown` ⇒ the
   artifact is demoted to a miss; if it was `accepted`, it is queued for
   `supersede --reason "stale per verify"`. Memory is re-anchored to the code
   before any decision depends on it.
2. **Structured check types** beyond a free shell command (qwen-auth idea: attach
   a git diff / file hash / function-existence check). v1 supports:
   - `shell` — run `verify_cmd`, zero exit ⇒ verified.
   - `grep` — symbol/pattern must exist in a path (`grep -q <pat> <file>`).
   - `build` — `go build ./...` (or project-specific) must pass.
   - `test` — a named test must pass.
   - `hash` — a recorded file hash must match (catches silent edits).
   Stored as `verify_cmd` + a `verify_kind` discriminator; shell remains the
   generic fallback.
3. **Staleness is detectable, not asserted.** The verify step compares the
   artifact's recorded evidence (symbol present / hash / build green) to live
   repo state. A claim whose evidence no longer holds is `stale` — this is
   exactly the "validation was written but not wired in" case caught by a
   `grep`/`build` check. A previously-verified artifact that now fails is
   stamped `stale` (not merely `error`) so drift is distinguishable from a
   never-verified claim; `ward tick`/`brief` report it as **drift**.
4. **Freshness TTL.** `verify_at` ages; a `verify_status` older than a
   configurable window (e.g. per-artifact `verify_ttl`) is treated as `unknown`
   and re-verified before routing trusts it. Prevents trusting a check from a
   stale point in time.
5. **Scope boundary (v1) — do not conflate the two triggers.** *Verify-triggered*
   supersede-queueing is **in** scope: a stale `accepted` artifact is queued for
   `supersede --reason "stale per verify"`. This is distinct from *TTL-based
   auto-supersede on `expires_at` expiry*, which is **out** of v1 scope: v1
   records and surfaces TTL/expiry metadata (e.g. in `activity`) but does **not**
   silently retire artifacts on a timer. flow.md's "queue for supersede" refers
   only to the verify trigger; blueprint.md's non-goal of no timer-based
   auto-supersede refers only to the TTL trigger.
6. **`golden` verify kind (v0.5).** Format `<expected-file>::<command>`: run the
   command, diff its stdout against the checked-in expected file (trailing
   newlines normalized). "Done" can mean *the output is right*, not merely exit
   0 or an input hash — verification that matches semantic completion. Same D0.3
   trust boundary as every other kind (store-local artifacts only). A golden
   mismatch on a previously-verified artifact is `stale`.
7. **Self-healing ticks (v0.5) — resolves the auto-supersede open question.**
   The question "flag-only vs auto-supersede" is resolved as **explicit operator
   action**: plain `ward tick` only reports drift (diagnosis); `ward tick --heal`
   supersedes any store-local accepted artifact whose live re-verification is
   `stale`/`error`, with reason `drift` (treatment). Healing inspects post-sweep
   statuses, so an artifact that failed on a previous tick is still healed — no
   zombies persist across sweeps. It acts only on store-local artifacts (the
   sweep already excludes imports), and supersede happens only after the real
   re-run failed: never a status stamp without evidence.

## Flow integration

- `ward resume` / `ward route` run verify-on-read for any artifact the router
  will consume, so the verify gap cannot reopen in light (solo) mode either.
- `ward verify` is the explicit bulk re-check (CI / pre-route gate).

## Open questions / risks

- **Auto-supersede vs. flag-only — RESOLVED (v0.5, item 7 above).** Flag in
  `tick`/`brief` output by default; auto-supersede only under explicit
  `--heal`. Non-lossy either way (reason `drift` recorded, handoff surfaces it).
- **Who runs the shell command / sandboxing.** `verify_cmd` executes arbitrary
  shell in the repo. Must run in a sandbox or trusted dir; a malicious artifact
  could run arbitrary code on `verify`. Proposal: verify runs only on artifacts
  the local user/agent explicitly wrote (not on imported/remote ones) in v1;
  document the trust boundary. Risk: supply-chain via shared memory store.
- **"Fresh enough" window — DECIDED (v0.5.x): verify-on-session-start replaces
  TTL.** The freshness-TTL mechanism (item 4) is deliberately left unbuilt:
  `ward brief` re-runs every local artifact's verify_cmd LIVE at session start,
  and routing live-verifies again before trusting. A stored `verify_status` is
  therefore never older than the current session's sweep when it matters, so an
  age-based demotion would add a knob without changing any decision.
  `verify_at` remains recorded for audit. Revisit-when: stores grow to where a
  full sweep per brief costs more than staleness risk (hundreds of artifacts
  with expensive checks).
- **Verify cost vs. benefit.** Running `build`/`test` per artifact on every
  brief/route could be slow at scale; if that bites, cache by `verify_at` +
  content hash of touched files rather than reintroducing a TTL knob.
  Revisit with the TTL decision above.
- **Git as backend.** ciao's per-step `git snapshot` is dropped; git here is only
  an *optional* verify backend (diff/hash), not a run-log. Confirm no other
  consumer needs the snapshot. (Same risk noted in orchestration.md.)
