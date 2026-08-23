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
2. **Two arms minimum**, identical task sets:
   - **independent**: N agents, same tasks, no shared pool (ward present but
     unused — protocol-correct empty-pool behavior);
   - **coordinated**: same N agents through ward's pool only (claims, tiers,
     capture). Takeover prompts are surgical: one job, zero pool commands.
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

## Results log

Append one section per completed experiment: date, arms, falsifier, numbers,
verdict, link to the tasks.md entry. No results yet under this consolidated
protocol; R12/R13 (wikirag solo/fleet) are the historical baseline:
solo 19.9 min, fleet 44.7 min, functional quality parity, coordination tax
named.
