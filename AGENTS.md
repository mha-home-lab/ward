# AGENTS.md

<!-- ward:protocol v1 -->
## WARD — verified project memory (managed block; do not edit between markers)

This project is ward-managed. Follow this protocol exactly; it exists so you
never re-solve solved problems and never trust stale claims.

1. SESSION START (always, before planning): run

       ward brief [topic]

   It re-verifies store-local results live, frees expired reservations, and
   prints prior knowledge, open runs, active claims, and suggested next actions.
2. TRUST RULE: only verified artifacts are facts. A memory hit votes for the
   cheap tier ONLY when live-verified against repo state; unverified, stale, or
   imported artifacts count as a MISS -> work at full attention. Treat a
   routing decision's verified context ids as truth, never a recap.
3. EXCLUSIVE WORK: before touching a shared topic (file, migration, release),
   run: ward memory claim add <topic> --ttl 60 --by <your-name>
   A conflict is a hard stop: pick different work, never proceed in parallel.
   Release with: ward memory claim release <topic>
4. RECORDING RESULTS IS AUTOMATIC: successful ward run nodes capture
   store-local artifacts tagged by node id. Do NOT hand-type
   ward memory put; never write a verify_cmd you would not run yourself.
5. BEFORE ENDING: run  ward memory handoff  so the next session inherits
   incomplete work, open runs, and stale candidates.
6. FAILURE POLICY: two escalating failures exhaust the budget and the run stops
   for a human. Never retry past it; report the rejection instead.

Every command accepts --json for machine-readable output. If a command errors,
fix the cause; never bypass the store or the trust boundary.
<!-- /ward:protocol -->
## Repo-specific notes

- `go build ./... && go test ./...` is the verification gate; `gofmt -l .` must
  be empty. Run all three before claiming done.
- Router purity is load-bearing: `internal/routing.Route` must stay pure (no
  I/O, no model calls). Execution lives in `internal/adapter`.
- The trust boundary (`Local` flag) gates verify_cmd execution. Never weaken it.
