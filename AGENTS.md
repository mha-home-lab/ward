# AGENTS.md

<!-- ward:protocol v5 -->
## WARD — verified project memory (managed block; do not edit between markers)

This project is ward-managed. Follow this protocol exactly; it exists so you
never re-solve solved problems and never trust stale claims.

1. SESSION START (always, before planning): run

       ward brief [topic]

   It re-verifies store-local results live, frees expired reservations, and
   prints prior knowledge, open runs, active claims, the task pool, and
   suggested next actions. Do what it says before planning.
2. SPEC-FIRST: for anything beyond a trivial edit, draft the spec before
   code: .spec/<topic>.md with Purpose / Signals / What's kept / What's
   changed and why / Open questions. Develop incrementally and keep the spec
   honest as you close each increment - a stale spec is a stale claim.
   Load relevant domain knowledge before writing it (see chips below).
3. TRUST RULE: only verified artifacts are facts. A memory hit votes for the
   cheap tier ONLY when live-verified against repo state; unverified, stale, or
   imported artifacts count as a MISS -> work at full attention. Treat a
   routing decision's verified context ids as truth, never a recap.
4. WORK FROM THE POOL (loop, do not ask permission): while brief lists open
   tasks within your budget —

       ward task next --by <your-name> --max-tier <budget>

   implement the pulled task's title in this repo, prove it with

       ward task run <task-id>

    THE RUN IS THE GATE: a task's --run/--verify-cmd is its acceptance check
    and must exercise the real change end-to-end (field lesson: a placeholder
    run like 'true' closes the task while proving nothing - that is a phantom
    success, the exact failure ward exists to prevent). For concurrent Go
    work make the check 'go test ./... -race'; default tests hide data races.
    Then repeat until the pool is empty or every remaining task is beyond your
    ability or blocked. On failure the task re-enters the pool one tier higher —
    do not retry it yourself; pull different work or stop. To resume a task a
    dead session left claimed: ward task take <id> --by <your-name>.
    When nothing is left: ward memory handoff, then stop.
 4b. NO PHANTOM RUNS (hard rule): you may NEVER use `true`, `echo`, `:`, or an
    empty string as a verify_cmd/run. The gate MUST exercise the actual change
    (e.g. `go test ./...`, `helm lint`, `pytest`). `ward task add` rejects these
    at authoring time. If a task lacks a valid gate, reject/repair it before
    claiming — never close work behind a no-op check.
 4c. EXTERNAL TRUTH FOR HIGH-TIER WORK: prefer immutable, observable CI over the
    agent's own shell to break self-attestation bias. For PR-based tasks, make
    the gate poll GitHub Actions so the task only closes when GitHub is green:
        gh pr checks "$PR_URL" --watch --interval 10s && echo 'CI PASSED'
    The verify command must be runnable by the engine, not a promise you made.
5. EXCLUSIVE WORK: before touching a shared topic outside the pool (file,
   migration, release), run: ward memory claim add <topic> --ttl 60
   A conflict is a hard stop: pick different work, never proceed in parallel.
6. RECORDING RESULTS IS AUTOMATIC: successful runs capture store-local
   artifacts tagged by node id. Do NOT hand-type ward memory put; never write
   a verify_cmd you would not run yourself.
7. BEFORE ENDING: run  ward memory handoff  so the next session inherits
   incomplete work, open runs, and stale candidates.
8. FAILURE POLICY: two escalating failures exhaust the budget and the run
   stops for a human with a dossier (ward reject <run>). Never retry past it.

PORTABLE KNOWLEDGE: accepted lessons tagged portable:<topic> are synced to
the global skills directory by 'ward skill-sync' - load any that match your
task before planning.

Every command accepts --json for machine-readable output. If a command errors,
fix the cause; never bypass the store or the trust boundary.
<!-- /ward:protocol -->
## Repo-specific notes

- Next-session charter: read `.arch/CHARTER.md` before planning.

- `go build ./... && go test ./...` is the verification gate; `gofmt -l .` must
  be empty. Run all three before claiming done.
- Router purity is load-bearing: `internal/routing.Route` must stay pure (no
  I/O, no model calls). Execution lives in `internal/adapter`.
- The trust boundary (`Local` flag) gates verify_cmd execution. Never weaken it.
