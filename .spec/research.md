# research — R&D Loop (Explorers Propose, Architect Evaluates)

| | |
|---|---|
| Status | Implemented; adversarially reviewed 2026-08-23 (self-promotion gate now enforced) |
| Domain | research |
| Version | 1.0.1 |

## Problem

A brain that only records a session's work stagnates. WARD needs fresh
external ideas flowing in as *evaluated* knowledge — and, unlike chef, it also
generates its own operational telemetry (routing decisions, task bounces,
dossiers, drift) that nobody aggregates. Both feeds need one disciplined loop
with the same property as everything else in WARD: no claim becomes knowledge
without a verdict.

Ported from chef `rd-001`, extended with WARD's internal feed.

## Roles

- **Architect** (big-model session / human): picks topics, spawns explorers,
  runs `ward harvest` before deciding, evaluates every proposal, records
  verdicts. Never implements explorer ideas directly.
- **R&D explorer** (small-model subagent): researches ONE topic and proposes
  2–4 artifacts. Tool surface: `ward brief`, `ward memory context/search/get`,
  read-only repo access, web/docs. It NEVER runs `ward memory put --ceremony
  light`, never promotes, never supersedes, never edits code.
- **Engineers** (dispatch pool): never participate in this loop; they execute
  pool tasks, not opinions.

## Protocol

1. **Harvest** — architect runs `ward harvest` (internal telemetry report:
   tier distribution, cheap-hit rate, bounce leaders, dossier themes, drift).
   Internal findings become topics or constraints for briefs.
2. **Brief** — one explorer per topic; topics stay independent (parallel OK).
   The brief names the topic, the WARD implication sought, and the alignment
   rules below.
3. **Boot** — explorer runs `ward memory context <topic>` and
   `ward memory search <topic>` first; duplicating known knowledge is waste.
4. **Research** — primary sources only (docs, papers, repos), current
   material, concrete mechanisms over marketing claims.
5. **Propose** — explorer writes each proposal with:

       ward memory put --ceremony full --by rd-explorer --tags "rd:<topic>" \
         --summary "..." --content "..."

   `--ceremony full` keeps status `proposed` (no auto-accept); provenance
   records the explorer. Kinds: `discovery` (how the world does it),
   `solution` (a recommendation for WARD), `feedback` (a critique of current
   behavior), `context`. Content must be self-contained: what / why it matters
   for WARD / implication / tradeoffs / references.
6. **Evaluate** — architect `get`s each proposal and applies the criteria.
   Verdicts are commands, not thoughts:

       ward memory promote <id> --reason "<verdict>" --by architect
       ward memory supersede <id> --reason "<verdict>"

   Leaving a proposal `proposed` is reserved for calls that genuinely belong
   to the human.

## Evaluation criteria (promote)

1. **Actionable for WARD** — concrete implication (spec/build/behavior change)
   or an explicit rejection rationale worth remembering.
2. **Correct and current** — accurate about the source system; primary-source
   backed.
3. **Non-duplicative** — no covering artifact; partial overlap merges weaker
   into stronger with the merge recorded in the reason.
4. **Well-formed** — self-describing summary, correct kind, complete content.
5. **Aligned** — router purity (routing.md) is untouchable; the D0.3 trust
   boundary is untouchable; no new heavyweight deps without overwhelming need.
   A violating proposal becomes a `solution` documenting the rejection.

## The internal feed (WARD-specific)

`ward harvest` turns the store's own history into R&D input:

- **Bounce leaders** — tasks whose escalation climbed: usually a bad
  acceptance check or a task split too coarse (authoring problem, not model
  problem — see tasks.md lessons from secure-bank/donate-fair).
- **Cheap-hit rate** — share of routes that were cheap+verified: the thesis
  metric. Falling rate means knowledge is rotting or tags/checks are weak.
- **Dossier themes** — rejected work clusters = blocked dependencies or
  systematically mis-floored task kinds.
- **Drift events** — what knowledge rots fastest; candidates for better
  verify kinds (e.g. golden instead of hash).

Internal findings do NOT auto-write artifacts: the architect converts them
into proposals through the same explorer/verdict path (or fixes authoring
guidance in `.arch/tasks.md` directly).

## Anti-patterns

| Anti-pattern | Guard |
|---|---|
| Explorer promotes itself | **Technically enforced (v1.0.1):** `memory put` rejects `--ceremony light` for any `--by rd-explorer*`; proposals cannot self-accept. Previously policy-only — the 2026-08-23 review flagged that a guarantee resting on politeness is not a guarantee. |
| Noise with no WARD implication | Criterion 1; superseded with reason recorded |
| Duplication | Boot-step search; supersede-merge |
| Hype as fact | Criterion 2; primary sources, numbers, references |
| Churn without verdict | Every proposal ends promoted, superseded, or explicitly parked for the human |

## Acceptance criteria

1. An explorer run produces `proposed` artifacts with `rd-explorer` provenance
   and `rd:<topic>` tags; none accepted without an architect verdict.
2. Verdict trail greppable: `ward memory list --status superseded` +
   `ward memory get <id>` show promoted/superseded reasons.
3. `ward harvest` runs on any store and prints the five telemetry sections
   (tiers, hit-rate, bounces, dossiers, drift) in human + `--json`.

## Non-goals

- Automating promotion (architect/human gate only).
- Feeding harvest output into the router (routing.md purity; observer-only).
- An always-on watcher running R&D (tossed wish 12 stands).
