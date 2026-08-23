# standing simulations — Draft

| | |
|---|---|
| Status | **Active discipline** (measurement protocol), experiments individually pre-declared |
| Domain | research / ops |
| Origin | R12/R12 A/B precedent + openai.md P2 ("prove whether three heterogeneous agents coordinated through Ward measurably outperform the same agents working independently") |

## Purpose
Ward's value question is empirical: does pool coordination produce measurably
better outcomes than independent agents — or is it token theater? Every
simulation run under this protocol must be able to answer a yes/no question
with numbers, using the same battery each time so results compare across
sessions.

## The protocol (every experiment)

1. **Pre-declare the falsifier.** Before running, write down the numeric
   condition under which the hypothesis FAILS. Publish the result either way
   (R12 precedent: solo beat fleet 2.3× and was recorded).
2. **Two arms minimum**, identical task sets: a treatment arm exercising the
   ward capability under test, and a control arm without it (e.g. pool-
   coordinated vs pool-absent; continuity-enabled vs fresh-context). Control
   agents are told plainly that no tooling exists — never sabotaged silently.
3. **Metrics recorded per arm** (the standing battery):
   - wall-clock to DoD-green;
   - duplicate-work rate: same file+intent touched by >1 agent;
   - cheap-hit % (verified cheap routes / total decisions) on repeat work;
   - escalation rate and bounce attribution;
   - human interventions count;
   - failures: total, attributed (agent vs environment vs check-authoring),
     silent failures (target: zero).
   - tokens/task where the worker exposes usage; else mark N/A honestly.
4. **Isolation**: sequential arms when any stack shares ports/containers
   (R13 lesson); `docker ps` before DoD gates that need it (daemon sleeps).
5. **Void rule**: contaminated or supervisor-interrupted runs are VOIDED and
   redone, never partially reported.

## Targets

- **mdq** (`~/playground/mdq`) — designated standing target: small real Go
  project, honest test suite, drained pool, already validated as an
  experiment bed (R10 greenfield full-workflow leg). Extensions to its
  backlog are the task source.
- Greenfield targets are created fresh per experiment when the question is
  about cold-start behavior, never reused between arms.

## Current queue (pulled from ward's own store)

- `task-4ddd023a` (strong): heterogeneous fleet experiment — ≥3 DIFFERENT
  agent binaries/models, one repo, pool-only coordination arm vs independent
  arm, full battery above. Falsifier to be written into this spec's results
  section BEFORE launch.
- `task-4da3d810` (mid): Runner-seam promote-or-keep decision AFTER the
  heterogeneous experiment (`.spec/runner-seam.md`).

## Experiment H1 — heterogeneous tiers on mdq (VOIDED — superseded by product decision)

Pre-declared falsifier and rig were built (2026-08-23); launch failed on an
opencode gateway incident (all free models returning server errors from new
CLI sessions while a pre-existing interactive session kept streaming) —
environment failure, cleanly distinguished per protocol. Before any retry,
Mohamed made the strategic call this experiment was meant to inform:
**parallel agent dispatch is retired as a product direction** (operator
experience: supervision mess; R12/R13: coordination tax with quality parity;
mature agent CLIs own orchestration). H1 will not be run. Rig dismantled;
`scripts/fleet-launch` archived to `attic/`; `ward fleet` command removed.
The falsifier framework below survives into H2.

## Experiment H2 — solo continuity: does ward's memory make the NEXT session faster? (PRE-DECLARED, queued)

The remaining usefulness question, now that parallelism is out: **does the
verified-memory loop (brief → work → capture → handoff) measurably reduce
re-discovery cost across sequential sessions on the same repo?**

- Design: mdq clone, 3 sequential small tasks given to fresh sessions (fresh
  context each) of one worker (single opencode model, mid tier). Arm A: ward
  initialized, protocol injected, tasks closed via pool with captures; each
  session starts with `ward brief`. Arm B: no ward, identical task texts,
  fresh context per task.
- Metrics: wall-clock per task and total; pass rate on acceptance checks;
  duplicate-discovery proxy — count of exploratory commands before first
  edit (from session logs), compared across arms.
- **Falsifier**: hypothesis "ward's continuity speeds up sequential work"
  FAILS if total wall-clock A ≥ B, or if A's pass rate is lower. Ties within
  ±10% count as fails (the tool must pay for its ceremony).
- Environment gate before launch: `opencode run` smoke test must pass twice
  consecutively (H1 lesson: gateway incidents masquerade as harness bugs).

## Results log

Append one section per completed experiment: date, arms, falsifier, numbers,
verdict, link to the tasks.md entry. No results yet under this consolidated
protocol; R12/R13 (wikirag solo/fleet) are the historical baseline:
solo 19.9 min, fleet 44.7 min, functional quality parity, coordination tax
named.

### H2 launch log
- 2026-08-23 ~21:00: first attempt GATE-REFUSED. Double smoke test failed;
  3-minute cooldown re-check across mimo/nemotron/hy3 all failed (same
  gateway incident as H1). No rig built, no tokens spent on doomed runs.
  Claim held for next session takeover.
