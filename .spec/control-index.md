# Control Specs Index — Autonomous Ward Roadmap

All specs derived from: (a) internal control-engineering case study (`.arch/reflection-control-study.md`), (b) a Gemini consultancy (filed in `public_agent_feedback/`, cleaned up in commit `b7f0176`; its contents are folded into the specs below). Each spec is a standalone, verifiable increment.

---

## Execution Order (Dependencies)

### Phase 1: Fix Core Sensors & Bounds (P0 — unblock autonomy; ALL audited against code)
| Spec | File | Key Deliverable | Depends On |
|------|------|-----------------|------------|
| **P0.1** Anti-windup claim expiry | `control-antiwindup.md` | **Built + closed v0.9.5** (`SweepExpiredClaims` in tick+brief, tested) — validated, regression gate only | — |
| **P0.2** Unbiased drift reporting | `control-drift-sensor.md` | **Built v0.9.5** — `drift` counts every failing local accepted artifact (absolute; heal was already unbiased) | — |
| **P0.3** Computed claim age in brief | `control-claim-age.md` | **Built v0.9.5** — `brief.go` `claimAge()` computes `mins_aged`/`age_hours` (never a literal) | — |

> **Gate (v0.9.5)**: `ward brief` shows `expired-claims-freed` (already live), `drift` counts every non-verified local accepted artifact, stale claims carry a computed age (`mins_aged` is never a literal).

---

### Phase 2: Close Knowledge Loop (P1 — thesis enablers; ALL audited against code)
| Spec | File | Key Deliverable | Depends On |
|------|------|-----------------|------------|
| **P1.1** Skill localization pipeline | `control-skill-localize.md` | **Built v0.9.5** — `ward skill install <topic> --verify-cmd` (+ `list-global`) | P0.2 |
| **P1.2** Topic-scoped heal | `control-skill-sharpen.md` | **Closed v0.9.5** — already exists as `ward wave <topic> [--heal]`; spec updated, no code | P1.1 |

> **Gate (v0.9.5)**: `ward skill install cli-contract --verify-cmd "..."` → `ward route test --kind test --memory-hit --verify-status verified` → `tier=cheap`; `ward wave <topic> --heal` heals only that tag (live-verified, surface_test).

---

### Phase 3: Telemetry & Reference Stability (P2 — control plane; ALL audited against code)
| Spec | File | Key Deliverable | Depends On |
|------|------|-----------------|------------|
| **P2.1** Routing KPI telemetry | `control-scorecard.md` | **Built v0.9.5** — `ward kpis [--window]` over `routing_decisions` + additive `execution_success` column stamped by the engine; existing `ward scorecard` untouched | P0.2 |
| **P2.2** Experiment watchdog | `control-experiment-watchdog.md` | Report-only stall visibility — **deferred** (until a stalled experiment recurs twice more) | — |

> **Gate (v0.9.5)**: `ward kpis --window 7d` renders cheap-hit %, escalation %, verify pass %, memory-miss % from `routing_decisions`; `ward experiment watchdog --check` (only if the deferred spec is ever built) reports stalled experiment claims read-only — reset/void stays a human decision per `.spec/simulation.md`.

---

## Cross-Cutting (Already Implemented)

| Feature | Spec | Status |
|---------|------|--------|
| Cross-project routing | `control-cross-project.md` (not separate — in v0.9.4) | ✅ Done (v0.9.4) |
| Evidence injection | `evidence-injection.md` | ✅ Done (v0.9.3) |
| Context reload + checkpoint | `context-reload.md` | ✅ Done (v0.9.1) |
| State-machine write safety | `state-machine-writes.md` (portable) | ✅ Done (v0.8.x) |

---

## Portable Lessons to Capture (After Each Phase)

| Phase | Lesson Tag | Captured By |
|-------|------------|-------------|
| P0 | `portable:control-antiwindup-claims` | `ward skill install` |
| P0 | `portable:control-drift-sensor-honesty` | `ward skill install` |
| P1 | `portable:control-feedforward-closure` | `ward skill install` |
| P2 | `portable:control-primary-kpi` | `ward skill install` |
| P2 | `portable:control-watchdog-gates` | `ward skill install` |

---

## Success Criteria (System-Level)

Every figure below is **computed from real state at run time** — the specs add
no constants:

```
ward brief --json (P0.3): stale_claims[].mins_aged is a computed number, never "30+"  ✅
ward brief (P0.1)      : expired-claims-freed  ✅ (already live)
ward tick --heal (P0.2): drift counts every non-verified local accepted artifact  ✅
ward wave <topic> --heal: topic-scoped adapt loop   ✅ (P1.2, existing)
ward kpis --window 7d  : cheap-hit %, escalation %, verify pass % from routing_decisions  ✅
ward skill install <topic> --verify-cmd → verified local artifact → tier=cheap  ✅ (P1.1)
ward experiment watchdog --check : lists stalled claims, read-only (deferred)
```

And `ward kpis --window 7d` renders the control variables with targets,
enabling gain tuning. The existing outcome-based `ward scorecard` remains
unchanged.

---

## Implementation Notes for CLI Agent

- Each spec is a **separate .spec file** in `.spec/control-*.md`.
- Implement **one spec at a time** in order; run its verification gate before moving on.
- After each spec, update CHANGELOG and tag per release procedure.
- Portable lessons are captured via `ward skill install portable:control-*` with the implementation's verify command.

## Build Order (by real effort, after the code audit)

| Tier | Work | Effort | Notes |
|------|------|--------|-------|
| **1** | `control-claim-age` | ~10 lines | ✅ Built (v0.9.5). Computed `mins_aged`/`age_hours` in `brief.go` via `claimAge()`; never a literal. |
| **1** | `control-drift-sensor` | ~3 lines | ✅ Built (v0.9.5). `sweepVerify`'s counter counts every `stale`/`error` local result (absolute), not only transitions. |
| **1** | `control-antiwindup` | none | ✅ Closed (v0.9.5). Regression gate `TestSweepExpiredClaims` green; mechanism was already built + tested. |
| **2** | `control-skill-sharpen` | none | ✅ Closed (v0.9.5): the topic-scoped adapt loop ALREADY existed as **`ward wave <topic> [--heal]`** — no `--topic` needed on tick. Spec updated, no production delta. |
| **2** | `control-scorecard` | contained | ✅ Built (v0.9.5). `ward kpis [--window]` over `routing_decisions` + new nullable `execution_success` column (additive migration v9), stamped by the engine on success and all failure paths. Existing `ward scorecard` untouched. Series-specific note below. |
| **3** | `control-skill-localize` | genuinely new | ✅ Built (v0.9.5). `ward skill install <topic> --verify-cmd` + `ward skill list-global` in `internal/cli/skill.go`. One artifact per chip; user-supplied gate independent of chip sources. Proven live (2026-08-30): verified local artifact → `ward route ... --memory-hit` votes `tier=cheap`. |
| **3 (deferred)** | `control-experiment-watchdog` | — | Hold off until a stalled experiment recurs twice more; currently solves one 2026-08-23 incident. Report-only scope preserved if ever built. |

## Phase gate (all verified live 2026-08-30, `git describe` = v0.9.5)

- `go build ./... && go test ./... -count=1` green; `gofmt -l .` empty.
- `ward kpis --window 7d --json` → KPI report from `routing_decisions` (empty until workflows run).
- `ward skill list-global` → real chips; `ward skill install <topic> --verify-cmd "<repo gate>"` → `Local=true` + `VerifyStatus=verified` when the gate passes; `ward route test --kind test --memory-hit --verify-status verified` → `tier=cheap`.

### Series-specific measurement note (P2.1)
`execution_success` refines each routing decision row: the engine stamps the
last decision for (run, node) with 1 on done and 0 on every failed/rejected
attempt (escalate, exhausted, preflight, identical-failure). Decisions never
reached by an outcome stay NULL — unknown, never guessed. `ward kpis`
aggregates cheap-hit = tier cheap AND success; these are per-attempt
observations, so a node that escalates then succeeds contributes one failure
and one cheap-tier-success to the rates, not a single node verdict.