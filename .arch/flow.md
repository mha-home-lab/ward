# WARD — End-to-End Flow

| | |
|---|---|
| Status | Draft (v1 planning) |
| Domain | flow |
| Version | 0.1.0 |

## Representative Task

> `ward run feature-development --title "add OIDC login to secure-bank"`

A feature request flows through the ciao-style DAG
(`request → specification → task-decomposition → approval → implementation →
testing → review → complete`). The point of this walkthrough is to show where
**memory lookup**, the **verify step**, **routing**, and **ceremony scaling**
slot into the ciao execution model, and exactly where the verify gap is closed.

## Sequence

### 1. Intake
- `ward run feature-development` creates a `run` row (status `running`,
  `current_node = request`) and emits the request payload into the `request`
  channel as a pending `work_item`.
- Engine advances: `request` (channel) → `specification`. At each channel node
  WARD pauses for an agent/skill to process the pending item (ciao model).

### 2. Chef-style knowledge lookup (memory)
- Before executing a node, WARD calls the memory layer (`search` / `context`)
  keyed on the node's topic (`"OIDC login"`, `"auth middleware"`).
- Returns prior artifacts: e.g. a `procedure` "wire OIDC with go-oidc", a
  `claim` that auth middleware exists, a `failure` about a past CSRF pitfall.
- **Crucially, at this stage memory only *surfaces* candidates. It does not yet
  vouch for them.** They are tagged `verify_status = unknown` until step 3.

### 3. Verify step (THE VERIFY GAP IS CLOSED HERE)
- For every candidate artifact that the router *might* trust as a signal, WARD
  runs the artifact's `verify_cmd` (or structured check — see verification.md)
  against the **real current repo state**: `grep` for the symbol, `go build`,
  `git diff` hash, etc.
- Each artifact is stamped `verify_status ∈ {verified, stale, error}` and
  `verify_at = now`.
- **Routing must not trust an unverified artifact.** A `stale`/`error`/unknown
  artifact contributes *no* "known pattern" signal to the router; it is treated
  as a miss (or, if it was previously `accepted`, it is queued for
  `supersede --reason "stale per verify"`). This is the concrete closure of the
  qwen-auth gap: memory is re-anchored to the code before any decision depends
  on it.

### 4. Routing decision (model tier)
- The router (`routing.md`) classifies the node's work unit into a tier:
  `cheap` / `mid` / `strong`.
- Signals consumed:
  - **memory hit + verify=verified** → known, solved pattern → `cheap`.
  - **memory miss or verify≠verified** → novel / unverified → escalate (`mid`
    or `strong`).
  - **DAG concurrency / contention** → if this node touches state also touched by
    parallel branches (high branching factor), escalate and engage heavier
    ceremony (see step 6).
  - **task type** → `test` nodes may force `mid` (need correctness); `review`
    may use `strong`.
- Output: `{tier, model, ceremony_level}`. The chosen model is passed to the
  agent/skill executor (replacing ciao's hard-coded gemini call).

### 5. Execution
- The agent/skill runs at the chosen tier, mutates the repo, and emits a result
  `work_item` to its output channel.
- On failure at `cheap`, the router escalates and retries at `mid`/`strong`
  (escalation rule). Repeated cheap failures are logged to the router decision
  table for future tuning.

### 6. DAG / approval handling (ciao-style)
- Engine advances per `graph.go`. At the `approval` node it halts, writes an
  `approval` item, prints `approval_id`, status → `awaiting_approval`.
- Human runs `ward approve <id>` (or `reject`); run resumes.
- **Crash recovery:** if the process dies mid-advance, `ward resume <run>` (or
  automatic resume at next `ward` invocation) reloads the run row from SQLite and
  continues from `current_node` — ciao's restart-safety, now backed by the
  unified SQLite store (no YAML file per run).
- **Ceremony scaling in this step:**
  - *Low concurrency (solo / sequential DAG):* `ceremony = light` — artifacts are
    auto-`accepted` (chef `put` semantics), status updated via `ward tick`,
    approval gate may be auto-passed, router defaults to `cheap` with verify.
  - *High concurrency (parallel branches / shared-state contention):* `ceremony =
    full` — artifacts go through `propose → promote → supersede`, `claim`
    reservations + conflict detection are required before touching shared state,
    approval gates are mandatory, and the router escalates contested nodes to a
    stronger model. The contention score is computed from the DAG (branching
    factor + which nodes read/write the same files).

### 7. Result capture
- Outcome is stored as an artifact. With `light` ceremony: auto-accepted
  `procedure`/`fact`. With `full` ceremony: `propose`d, awaiting promotion.
- If the artifact makes a claim about repo state, a `verify_cmd` is attached so
  future runs can re-verify it (step 3 logic is reusable here).

### 8. Propose-back into memory
- Durable knowledge is written back: a corrected fact, a reusable procedure, a
  discovered pitfall.
- **Incomplete work is captured as structured data, not prose** (qwen-auth gap
  #2): `handoff --incomplete` stores `{summary, files:[...], lines:[...],
  why, attempted_at}` so the next session sees *what was started but not finished
  and why* — surfaced prominently on `resume`.

## Where the Verify Gap Is Closed

Explicitly: **step 3, before step 4.** The router's "known pattern" signal is
computed only over artifacts whose `verify_status = verified` against current
repo state. An artifact that merely *exists* in memory but is `stale`/`error`/
`unknown` is demoted to a miss. Routing therefore never trusts a claim chef
would have surfaced as a blind "trust-me" index. Verification is a *precondition
for memory-as-signal*, not a post-hoc display.

## Where Ceremony Scales

- **Up** (toward full propose/promote/supersede + claims + mandatory approvals)
  when DAG branching factor is high or multiple nodes write overlapping files
  (shared-state contention). Driven by the contention score in step 6.
- **Down** (toward auto-accept + `tick` + optional verify) for solo/sequential
  work, where the qwen-auth feedback shows full ceremony is overhead-negative.
- The same artifact can live under light ceremony in a solo project and full
  ceremony in a concurrent one; the store records `ceremony_level` per artifact
  so `resume` knows how much verification/gate overhead to apply.

## References

- `ward/.spec/memory.md` — lifecycle, `incomplete`, `claim`.
- `ward/.spec/orchestration.md` — DAG, approval, resume.
- `ward/.spec/routing.md` — tiering signals + escalation.
- `ward/.spec/verification.md` — verify_cmd semantics + staleness.
- `ward/.spec/storage.md` — `verify_status`/`verify_at` columns, run rows.
