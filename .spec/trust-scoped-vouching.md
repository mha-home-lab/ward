# trust-scoped topic-vouching — Draft

| | |
|---|---|
| Status | **Draft** — deferred design, deliberately unbuilt |
| Domain | memory / routing |
| Origin | proposal `bee2477e` (donate-fair store, rd:c2) |

## Purpose
Topic tags let one task's verified capture vouch for another task on the same
topic (L6 compounding). But topics are unbounded strings: `topic:auth` vouches
equally for a Go middleware change and an OAuth provider migration. Scoped
vouching would bind each artifact's reach to a trust boundary — repo, path
prefix, skill, or agent identity — so a hit cannot cross contexts without an
explicit re-verify or delegation.

## Signals
- A routing decision whose verified context ids come from a different repo/
  path shape than the node being routed.
- Imported artifacts (never store-local) that nonetheless match a topic.
- Fleets spanning repos that share topic conventions.

## Design sketch
1. **Scope field on artifacts**: `trust_scope` = structured claim like
   `repo:ward`, `path:internal/cli/**`, `agent:*`. Default empty = current
   behavior (store-local scope), so nothing regresses.
2. **Hit matching**: the engine's topic-intersection check additionally
   requires scope containment — node context ⊇ artifact scope. A mismatch is a
   MISS with reason "scope", visible in `explain`.
3. **Delegation**: superseding-and-recreating under a wider scope is the only
   way to widen; no in-place promotion. Keeps the audit trail honest.
4. **Chips**: compilation records scopes; consumers of a chip outside its
   scope see an explicit UNVERIFIED-FOR-THIS-REPO marker rather than silence.

## What's kept
The database-as-arbiter pattern: scoping must be enforceable at INSERT time
wherever possible, not by convention. Route purity untouched. Existing stores
migrate additively (empty scope = today's semantics).

## What's changed and why
Nothing yet. The single-store reality makes full scoping premature; fleets
sharing topic conventions are the trigger condition.

## Open questions
- Scope grammar (glob vs explicit list) and where it lives — column vs tags?
- Interaction with project lens (`--project`): same thing or orthogonal?
- Cost: does every route now need path computation? Probably adapter-side.
