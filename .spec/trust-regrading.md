# outcome-driven trust re-grading — Draft

| | |
|---|---|
| Status | **Draft** — deferred design, deliberately unbuilt |
| Domain | verification / memory |
| Origin | proposal `2092335d` (donate-fair store) |

## Purpose
Today an artifact's verify status is a two-state gate: last live run passed or
didn't. It cannot *learn*. A procedure that has verified green ten times across
three waves is not the same trust object as one that verified once at capture
time — but the router sees them identically. Re-grading would let accumulated
outcomes evolve trust (e.g. `unknown → verified → hardened`), and repeated
real-world failures downgrade it without waiting for the next verify run.

## Signals
- Verify history density per artifact (needs `.spec/flake-quarantine.md`'s
  schema first — this draft depends on that one).
- Downstream outcomes: work executed on a hit that then failed its own check.
- Reuse rate (`used_count`) correlated with verify outcomes.

## Design sketch
1. **Grade, not badge**: `trust` becomes a small ordinal derived from outcome
   history (count + recency + stability), recomputed lazily, never hand-set.
2. **Router contract stays binary at the boundary**: grades map onto existing
   inputs (hit/verify-status); the pure router does not grow new dimensions.
   A "hardened" artifact behaves exactly like verified today; re-grading only
   changes who is *eligible* to be trusted after drift or import.
3. **Downgrade path**: N downstream failures attributable to a vouching
   artifact demote it to stale-with-reason, visible in `brief`, healed by
   supersede as usual.
4. **Never automatic promotion past `verified`**: promotion beyond verified
   requires the same evidence class as today (live run), just more of it.

## What's kept
No silent status writes: every grade change traces to recorded outcomes. The
Local/trust boundary is untouched — imported artifacts can never earn store-
local trust by correlation.

## What's changed and why
Nothing yet. The dependency on verify-history makes this strictly second in
line behind flake quarantine; building it first would mean inventing the schema
twice.

## Open questions
- Does grading belong in the store (computed column) or the adapter (view)?
- Recency decay: half-life vs windowed counts?
- Interaction with scorecards — do engineer outcomes feed artifact grades?
