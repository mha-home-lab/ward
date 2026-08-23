# flake quarantine — Draft

| | |
|---|---|
| Status | **Draft** — deferred design, deliberately unbuilt |
| Domain | verification / pool |
| Origin | rd:c2 cluster (`bfd02833` pre-flight+quarantine, `a5fee2fa` flake-aware re-verify, `e68a9957` vouching hole) |

## Purpose
A verify_cmd that passes and fails nondeterministically poisons everything
downstream: a flaky green lets an artifact vouch false knowledge into chips
and cheap routes; a flaky red burns escalation budget on work that was actually
fine. Quarantine would make instability itself visible and gate on it.

## Signals
- Same artifact's verify_cmd flipping status across consecutive live runs.
- A node whose attempts fail identically then pass without any change
  (pre-flight already short-circuits identical *failures*; this is the
  inverse).
- Wave results that disagree with the previous wave for the same id.

## Design sketch
1. **Schema first** (the blocker): `verify_history(id, at, status, detail)`
   appended on every live verification. Today only the latest status survives,
   so instability is literally unrecordable. Additive migration, never rewrite.
2. **Flake score**: rolling window (last N=5 verifications) → flips/total.
   `score > 0` marks the artifact unstable.
3. **Quarantine semantics**: unstable artifacts cannot vote cheap and are
   excluded from chip compilation by default; surfaced in `brief` and `memory
   stale --unstable`. NOT auto-superseded — instability is evidence about the
   check, not proof the knowledge is wrong.
4. **Out of scope**: fixing flaky checks automatically; cross-store history.

## What's kept
Router purity untouched (score is an input assembled by the adapter/engine,
not the router). Trust boundary unchanged: history rows written only from live
verifications of store-local artifacts.

## What's changed and why
Nothing yet. This exists as a draft because the R2/R3 explorer cluster
converged on it four times independently — but convergence is not a mandate.

## Open questions
- Does per-artifact history suffice, or does topic-level stability matter?
- Window size vs store noise; does `tick` frequency bias the score?
- Should waves write history too, or only route-time verifications?
