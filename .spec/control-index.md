# Control Specs Index — Autonomous Ward Roadmap

All specs derived from: (a) internal control-engineering case study (`.arch/reflection-control-study.md`), (b) a Gemini consultancy (filed in `public_agent_feedback/`, cleaned up in commit `b7f0176`; its contents are folded into the specs below). Each spec is a standalone, verifiable increment.

---

## Execution Order (Dependencies)

### Phase 1: Fix Core Sensors & Bounds (P0 — unblock autonomy)
| Spec | File | Key Deliverable | Depends On |
|------|------|-----------------|------------|
| **P0.1** Anti-windup claim expiry | `control-antiwindup.md` | `SweepExpiredClaims()`, claim age in brief | — |
| **P0.2** Unbiased drift sensor | `control-drift-sensor.md` | `DetectDrift()`, `tick --heal` honest | — |
| **P0.3** Claim age in brief | `control-claim-age.md` | Stale claim age in `brief` output | P0.1 |

> **Gate**: `ward brief` shows `expired-claims-freed>0`, `drift_count=7` (honest), `active-stale-claims` with ages.

---

### Phase 2: Close Knowledge Loop (P1 — thesis enablers)
| Spec | File | Key Deliverable | Depends On |
|------|------|-----------------|------------|
| **P1.1** Skill localization pipeline | `control-skill-localize.md` | `ward skill install <topic> --verify-cmd` | P0.2 (live verify works) |
| **P1.2** Skill sharpening (adaptation) | `control-skill-sharpen.md` | `ward skill sharpen --all` | P1.1 (local skills exist) |

> **Gate**: `ward skill install portable:control-antiwindup --verify-cmd "..."` → router votes cheap on matching tag.

---

### Phase 3: Telemetry & Reference Stability (P2 — control plane)
| Spec | File | Key Deliverable | Depends On |
|------|------|-----------------|------------|
| **P2.1** KPI scorecard | `control-scorecard.md` | `ward scorecard --window 7d` (γ_cheap, ε, etc.) | P0.2 (honest sensors) |
| **P2.2** Experiment watchdog | `control-experiment-watchdog.md` | Report-only stall visibility (age, gate, last attempt) | — |

> **Gate**: `ward scorecard --window 7d` renders γ_cheap, ε, verification pass %; `ward experiment watchdog --check` reports stalled experiment claims read-only — reset/void stays a human decision (see spec for the stated policy question).

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

When all specs are implemented, `ward brief` will show:
```
expired-claims-freed: 2
drift_count: 0
active-stale-claims: 0
portable_local_count: 5
cheap-hit-rate: 35%  (via scorecard)
```

And `ward scorecard --window 7d` will render all KPIs with targets, enabling gain tuning.

---

## Implementation Notes for CLI Agent

- Each spec is a **separate .spec file** in `.spec/control-*.md`.
- Implement **one spec at a time** in order; run its verification gate before moving on.
- After each spec, update CHANGELOG and tag per release procedure.
- Portable lessons are captured via `ward skill install portable:control-*` with the implementation's verify command.