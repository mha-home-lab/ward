# Spec: recurrence links for portable knowledge (agent-declared, not detected)

## Purpose
The field report's real ask — "this exact lesson appears across ≥2
independent runs, which is precisely when it's proven transferable" — is a
correct instinct. But *detecting* that two differently-worded captures
describe the same underlying mechanism (`local -i x=$1 base=$((x%m))` vs
`local n=$1 next=$((n-1))`, both "arithmetic-in-`local`-shadowing") is a
semantic-similarity problem. Every other gate in this pipeline
(transferability lint, drift, capture-gap) stays deterministic on purpose —
a pattern match that's honest about being wrong sometimes, never a content
judgment. Recurrence detection can't be built the same way and still be
honest: lexical overlap would miss exactly the cases that matter (same
mechanism, different variable names, different specific arithmetic) while
occasionally firing on coincidental wording matches that aren't the same
lesson at all. Building it anyway would be the first fake-deterministic gate
in the project — one that *looks* like a pattern match but is actually
laundering a judgment call as a score.

So this doesn't build recurrence *detection*. It builds recurrence
*declaration*: a cheap, structural way for an agent — who already has the
understanding ward doesn't — to record "I recognize this as the same trap as
<id>, just surfaced differently" at the moment it notices, the same way
`Supersede` already lets an agent declare "this replaces that" without ward
judging content. Ward's job is to count and surface the declaration, never
to make it.

## Signals (what good looks like)

1. **New table `recurrences`** (many-to-one — several later captures can all
   confirm the same original lesson, unlike `superseded_by` which is 1:1):
```sql
   CREATE TABLE IF NOT EXISTS recurrences (
       id       INTEGER PRIMARY KEY AUTOINCREMENT,
       of_id    TEXT NOT NULL,   -- the earlier artifact being confirmed
       from_id  TEXT NOT NULL,   -- the new capture that recognized it
       note     TEXT,            -- optional: how the surface form differed
       at       TEXT NOT NULL
   )
```
2. **`ward memory put --local --tags portable:<topic> --recurs <id> ...`** —
   new flag on the existing manual capture path (`internal/cli/memory.go`).
   When set: after `UpsertArtifact` succeeds, record a `recurrences` row
   (`of_id = <id>`, `from_id` = the new artifact's id). No new judgment by
   ward — the agent supplies the link, exactly as it already supplies
   `--verify-cmd`.
3. **`ward capture ... --recurs <id>`** — same flag threaded through
   `captureNode` for the on-pool path, for symmetry (though off-pool is
   where this actually matters most, per the field case).
4. **Deterministic promotion signal, finally real:** `store.RecurrenceCount(id)`
   = `COUNT(*) FROM recurrences WHERE of_id = ?`. Surfaced in:
   - `ward memory get <id> --json` → `"recurrence_count": N`.
   - `ward brief`: a verified portable artifact with `recurrence_count >= 2`
     and no chip yet gets folded into the existing skill-sync nudge, now
     with the honest reason: *"confirmed independently N times — strong
     promotion candidate,"* instead of the current nudge which fires on
     verification status alone. This is the "make the promote-to-portable
     decision data-driven" outcome the field report wanted, built on counts
     an agent declared, not on ward's opinion of the text.
5. **Optional, explicitly assistive nudge — not a gate.** At capture time,
   if a new `portable:<topic>` capture shares enough distinctive tokens
   (reuse `transferability`'s existing tokenizer, same stopword list) with
   an *existing* artifact under the same topic, print a **non-blocking
   hint**: `"this looks similar to <id> — if it's the same lesson in
   different wording, consider --recurs <id> instead of a fresh capture"`.
   This is autocomplete, not detection: it will under-fire (miss real
   recurrences with little lexical overlap, exactly the nameref/eval-order
   case) and may over-fire on coincidence. It never links anything itself —
   only the agent's `--recurs` flag does that. Skippable entirely if you'd
   rather ship steps 1–4 alone and add this later once real usage shows
   whether it's worth the noise.

## Decisions (resolved before code)

- **Ward never declares a recurrence, only records one.** No token-overlap
  score, no threshold, no auto-linking — anywhere in the mandatory path.
  The optional nudge in signal 5 is cosmetically similar to a gate but
  functionally isn't one: it changes no data and blocks nothing.
- **Recognition depends on the agent checking first.** This only works if
  an agent capturing a new portable lesson has *seen* the prior one — which
  it already does, via `brief`/`skill install`, before doing the work. The
  protocol text (`agentdoc.go`, the same "THE ONE EXCEPTION" block from
  control-capture-loop) gets one added sentence: *"If this lesson is the
  same underlying trap as an existing portable artifact in different
  wording, link it with `--recurs <id>` instead of filing an unrelated
  duplicate."* That's the whole enforcement mechanism — same posture as
  every other protocol instruction in this project: ward can't compel
  compliance, only make the correct action cheap and visible.
- **A missed recurrence is a silent miss, not a false gate.** If an agent
  doesn't notice or doesn't link, the lesson just sits as an ordinary
  unconfirmed portable artifact — same as today, no regression. This spec
  makes recurrence cheap to *record* when noticed; it does not guarantee
  every recurrence gets recorded, and shouldn't pretend to.

## What's kept / changed

- **New**: `recurrences` table (migration), `RecordRecurrence`/
  `RecurrenceCount` in `internal/store/artifacts.go`.
- **Changed**: `--recurs` flag on `memoryPutCmd` and `captureCmd`;
  `memory get --json` includes `recurrence_count`; `brief`'s existing
  skill-sync nudge (from the last fix) gains the recurrence-count reason
  when present.
- **Kept**: `Supersede` untouched — recurrence and supersession are
  different relationships (supersede = "this replaces that, one artifact
  wins"; recurrence = "this confirms that, both stand").

## Deliberately NOT built

- No automatic linking, ever, at any confidence threshold — that's the
  semantic-judgment line this spec exists to not cross.
- No cross-project recurrence tracking in v1 — `recurrences` is local to one
  store, same as everything else pre-sync; a lesson that recurs across two
  *different* projects' independent stores isn't visible to either until a
  human notices both reports (same limitation the field report itself hit).
- No retroactive linking of the run #3/#4 case by ward itself — that's a
  human or agent action using the new flag, same as the manual capture
  fix before it.

## Verification gate

```bash
go test ./internal/store/... -run TestRecurrence -v

# E2E
id1=$(ward memory put --local --tags portable:bash \
  --summary "local -i eval-order trap" --content "..." \
  --verify-cmd true --json | jq -r '.id')
id2=$(ward memory put --local --tags portable:bash \
  --summary "local shadowing, different exercise" --content "..." \
  --verify-cmd true --recurs "$id1" --json | jq -r '.id')
ward memory get "$id1" --json | jq -e '.recurrence_count == 1'
# Expected: true — one confirmed recurrence

ward brief | grep -i "confirmed independently"
# Expected: the recurrence-backed reason appears in the skill-sync nudge
```
