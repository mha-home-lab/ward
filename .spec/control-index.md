# Control Specs Index — Autonomous Ward Roadmap

All specs derived from: (a) internal control-engineering case study (`.arch/reflection-control-study.md`), (b) a Gemini consultancy (filed in `public_agent_feedback/`, cleaned up in commit `b7f0176`; its contents are folded into the specs below). Each spec is a standalone, verifiable increment.

---

## Execution Order (Dependencies)

### Phase 1: Fix Core Sensors & Bounds (P0 — unblock autonomy; ALL audited against code)
| Spec | File | Key Deliverable | Depends On |
|------|------|-----------------|------------|
| **P0.1** Anti-windup claim expiry | `control-antiwindup.md` | **Already built** (`SweepExpiredClaims` in tick+brief, tested) — validate, no build | — |
| **P0.2** Unbiased drift reporting | `control-drift-sensor.md` | Fix `drift` count to absolute failing-local-artifacts (heal already unbiased) | — |
| **P0.3** Computed claim age in brief | `control-claim-age.md` | Replace hardcoded `"30+"` (`brief.go:156`) with computed age | — |

> **Gate**: `ward brief` shows `expired-claims-freed` (already live), `drift` counts every non-verified local accepted artifact, stale claims carry a computed age (`mins_aged` is never a literal).

---

### Phase 2: Close Knowledge Loop (P1 — thesis enablers; ALL audited against code)
| Spec | File | Key Deliverable | Depends On |
|------|------|-----------------|------------|
| **P1.1** Skill localization pipeline | `control-skill-localize.md` | `ward skill install <topic> --verify-cmd` (imported → local artifact) | P0.2 |
| **P1.2** Topic-scoped heal | `control-skill-sharpen.md` | `ward tick --heal --topic <tag>` — adapt loop already exists; only add the filter | P1.1 |

> **Gate**: `ward skill install portable:control-antiwindup --verify-cmd "..."` → router votes cheap on matching tag; `ward tick --heal --topic <tag>` heals only that tag.

---

### Phase 3: Telemetry & Reference Stability (P2 — control plane; ALL audited against code)
| Spec | File | Key Deliverable | Depends On |
|------|------|-----------------|------------|
| **P2.1** Routing KPI telemetry | `control-scorecard.md` | `ward kpis [--window]` — aggregation over existing `routing_decisions`; **never** touches the existing outcome-based `ward scorecard` | P0.2 |
| **P2.2** Experiment watchdog | `control-experiment-watchdog.md` | Report-only stall visibility (age, gate, last attempt) — **deferred** | — |

> **Gate**: `ward kpis --window 7d` renders cheap-hit %, escalation %, verify pass %; `ward experiment watchdog --check` (only if the deferred spec is ever built) reports stalled experiment claims read-only — reset/void stays a human decision per `.spec/simulation.md`.

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
ward brief --json (P0.3): stale_claims[].mins_aged is a computed number, never "30+"
ward brief (P0.1)      : expired-claims-freed  (already live today)
ward tick --heal (P0.2): drift counts every non-verified local accepted artifact
ward kpis --window 7d  : cheap-hit %, escalation %, verify pass % from routing_decisions
ward experiment watchdog --check : lists stalled claims, read-only
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
| **1** | `control-claim-age` | ~10 lines | Replace `"30+"` with computed age — single function in `brief.go`. Do first. |
| **1** | `control-drift-sensor` | ~3 lines | Change one condition in `sweepVerify`'s counter. |
| **1** | `control-antiwindup` | none | Run the regression gate and close — mechanism is built + tested. |
| **2** | `control-skill-sharpen` | contained | Thread `--topic` into `sweepVerify`'s existing query; no new package. |
| **2** | `control-scorecard` | contained | `ward kpis` over existing `routing_decisions`. **Decision made**: take the additive-column route for `execution_success` (cheaper to query; consistent with `verify_status` living on the row). |
| **3** | `control-skill-localize` | genuinely new | Ambiguity resolved (one artifact per chip; verify_cmd independent of chip sources). This is the thesis mover — build it right, not fast. |
| **3 (deferred)** | `control-experiment-watchdog` | — | Hold off until a stalled experiment recurs twice more; currently solves one 2026-08-23 incident. Report-only scope preserved if ever built. |