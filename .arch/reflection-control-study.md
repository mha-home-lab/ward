# Ward Autonomy: A Control Engineering Case Study

**Date**: 2026-08-29 | **Version**: 0.9.4 | **Context**: Post v0.9.4 cross-project routing release

---

## 1. System Identification: What Is Ward?

Ward is a **discrete-time, multi-loop feedback control system** for agent coordination. Its plant is the coding agent; its controller is the routing/verification logic; its sensors are live verification runs; its actuators are tier assignment and task pool admission.

### Core Control Loops

| Loop | Reference (Setpoint) | Sensor | Controller | Actuator | Disturbance Rejection |
|------|---------------------|--------|------------|----------|----------------------|
| **Routing** | "cheap if verified hit else escalate" | `memoryHitForNode` (tag-first retrieval + live verify) | `routing.Route` (pure function) | Tier assignment → `adapter.Run` | Live verify gates stale memory |
| **Verification** | `verify_cmd` exit 0 | Sidecar log exit code | `verification.Run` | `verify_status` column (verified/error/unknown) | Re-verify on every route |
| **Claim/Escalation** | Task done in ≤2 escalations | Run status (completed/rejected) | `FailTask` (tier+1) / `CompleteTask` | Task tier floor, claim status | Max 2 escalations → reject |
| **Memory/Compounding** | Prior solution → cheap routing | `ContextForTask` (tag-scoped) | `memoryHitForNode` | Verified artifact carried as evidence | Tag-first retrieval + live gate |
| **Context Reload** | Agent starts with scoped knowledge | `ward task next/run` output | `printScopedContext` + `printLatestCheckpoint` | Prior knowledge block in prompt | Mechanical, not discipline |
| **Cross-Project** | Ward requests in ward store | `--project` flag + registry | `OpenForName` | Correct `.ward` DB | Misplacement guard warning |

---

## 2. Current Operating Point (Measured State)

### State Vector (v0.9.4)
```
Tasks:         13 total  | 4 open  | 6 stale-claimed  | 3 rejected
Artifacts:     8 accepted | 1 verified | 7 error-drift  | 0 proposed
Portable:      8 lessons  | all imported (non-local) | 0 local
Claims:        6 stale    | 0 expired-freed  | 0 active
Runs:          0 history  | 0 active
Gates:         2 phantom-smoke gate (H2) | never met
Experiments:   1 claimed (H2, 6 days stale) | 0 running
```

### Key Deviations from Design Intent

| Design Intent | Measured | Deviation | Control Implication |
|---------------|----------|-----------|---------------------|
| Claims auto-expire | 6 stale, 0 freed | `expired-claims-freed=0` always | Integrator windup — claims integrate indefinitely |
| Live verify gates memory | 7/8 local artifacts in `error` | Drift detected but `drift=0` in brief | Sensor bias — drift reported as 0 |
| Cheap routing via memory | 0 cheap hits recorded | No telemetry on cheap-hit % | No feedback signal for primary objective |
| H2 experiment gate | "2 clean opencode smokes" | Never met in 6 days | Reference unreachable — dead loop |
| Portable lessons → local skills | 8 imported, 0 local | `Local=false` → never vote cheap | Feedforward path open but not closed |
| Skill sharpening | 16 global skills | Never re-verified, never sharpened | No adaptation loop |

---

## 3. Control-Theoretic Diagnosis

### 3.1 Integrator Windup (Stale Claims)
**Symptom**: 6 stale claims persist indefinitely. `ward brief` reports `expired-claims-freed=0` every sweep.

**Root Cause**: The claim TTL mechanism exists (`ExpiresAt` column) but:
- No automatic expiry enforcement in `brief`/`tick` (code checks `LegacyClaimCount` but doesn't free by TTL)
- No anti-windup: claims integrate error (stalled work) without saturation handling
- No observability: brief shows "STALE CLAIM" but no metric, no alert, no auto-recovery

**Control Fix Needed**: 
- Add anti-windup: auto-release claims past TTL in `tick` (not just legacy cleanup)
- Add claim-age metric to `brief` output
- Saturate claim integral: max N concurrent claims per agent

### 3.2 Sensor Bias (Drift Reporting)
**Symptom**: 7 local artifacts in `verify=error`, but `ward brief` reports `drift=0`. `tick --heal` says "drift=0; healed=5" — contradictory.

**Root Cause**: `tick` counts drift as "status changed from verified→error" but these were `unknown→error`. The drift counter only tracks verified→error transitions. Artifacts created as `accepted` with `verify=unknown` then degraded to `error` are invisible to drift counter.

**Control Fix Needed**:
- Drift = any local artifact whose live verify status ≠ stored verify_status
- Brief should report: `drift=7` (not 0), `healed=5` (after tick --heal)
- Add drift rate metric (drift/sweep) for trend monitoring

### 3.3 Missing Feedback Signal (Cheap-Hit Rate)
**Symptom**: The primary thesis metric — "cheap tier hit rate" — is never measured, logged, or fed back.

**Root Cause**: Routing decisions are logged (`routing_decisions` table) but:
- No periodic aggregation (cheap-hit %, escalation rate, verification pass rate)
- No experiment harness to measure thesis: "verified memory → cheaper routing → same quality"
- H2 experiment designed to measure this but gate unreachable

**Control Fix Needed**:
- Add `ward scorecard` (exists but unimplemented) as periodic KPI report
- Make cheap-hit % a control variable with target (e.g., >30%)
- Close H2 experiment loop: automate the gate or lower the barrier

### 3.4 Open Feedforward Loop (Portable Lessons)
**Symptom**: 8 portable lessons exist (`portable:*` tags), all `Local=false` (imported), so they **never vote for cheap**. They are feedforward knowledge that never enters the feedback loop.

**Root Cause**: 
- `Local=false` → `verify.Run` returns `unknown` → never verified → never votes cheap
- `ward skill-sync` pushes to global skills but no mechanism to `pull` and `localize` (mark Local=true, add verify_cmd)
- No "lesson → skill → local artifact" pipeline

**Control Fix Needed**:
- `ward skill install <topic>`: pull from global, add verify_cmd, mark Local=true
- Periodic skill sharpening: re-run verify_cmd, update, promote
- Skills must earn `Local` by passing live verify — closes the feedforward loop

### 3.5 Unreachable Reference (H2 Experiment)
**Symptom**: H2 experiment claimed 6 days ago, gate "2 clean opencode smokes" never met.

**Root Cause**: 
- Gate requires external CI (opencode) which is flaky ("gateway incidents masquerade as harness bugs")
- No fallback, no automated retry, no human escalation path
- The reference (experiment launch) is unreachable → integral error accumulates

**Control Fix Needed**:
- Gate should be `ward`-internal (e.g., `go test ./... -race` passes 2x) not external
- Add experiment watchdog: if gate unmet >24h, auto-escalate to human or lower gate
- Make experiment launch a `task run` with its own verification, not a pre-condition

### 3.6 No Adaptation Loop (Skill Sharpening)
**Symptom**: 16 global skills, 0 re-verified since creation. Lessons degrade silently.

**Root Cause**: 
- Skills are static files in `~/.config/opencode/skills/`
- No `ward skill sharpen` command
- No scheduled re-verification of skill content
- No metric: "skill freshness" or "skill verification pass rate"

**Control Fix Needed**:
- `ward skill sharpen <topic>`: pull global, re-verify, update local
- Scheduled sharpening (cron-like) for all portable skills
- Skill health metric in `brief`

---

## 4. Autonomy Improvement Proposals (Prioritized)

### P0: Close the Integrator Windup (Stale Claims)
**Change**: `tick` must enforce claim TTL and free expired claims.
```go
// In tick.go SweepExpiredClaims()
func (s *Store) SweepExpiredClaims() (int64, error) {
    // Existing: frees legacy claims
    // ADD: free claims where ExpiresAt < now AND status == "claimed"
}
```
**Metric**: `brief` shows `expired-claims-freed=N` (should be >0 when stale exist).

### P1: Fix Drift Sensor
**Change**: Drift = any local artifact where `verify_status != live_verify_result`.
```go
// In brief.go / tick.go
driftCount := count of local artifacts where 
    VerifyStatus != "" && VerifyStatus != liveVerify(artifact)
```
**Metric**: `brief` shows `drift=7` (honest), `tick --heal` reports `healed=7`.

### P2: Cheap-Hit Telemetry (Primary KPI)
**Change**: Add `ward scorecard` command that aggregates routing decisions.
```bash
ward scorecard --window 7d
# Output:
# cheap-hit rate: 12% (target >30%)
# escalation rate: 40%
# verification pass rate: 65%
# mean escalations/task: 1.2
```
**Control Use**: If cheap-hit < target → increase verification aggressiveness or improve retrieval.

### P3: Skill Localization Pipeline
**Change**: `ward skill install <topic>` → pulls global, adds verify_cmd, marks Local=true.
```go
// skillCmd.install()
func installSkill(name string) error {
    global := loadGlobalSkill(name)
    verifyCmd := synthesizeVerifyCmd(global.Content) // or prompt
    artifact := Artifact{..., Local: true, VerifyCmd: verifyCmd, ...}
    upsert, then verify.Run() // earns Local by passing
}
```
**Metric**: `brief` shows `portable:local=N` (should grow).

### P4: Experiment Watchdog (H2 Gate)
**Change**: Lower H2 gate to ward-internal; add watchdog.
```yaml
# .spec/simulation.md gate change:
gate: "go test ./... -race"  # internal, deterministic
watchdog: 24h  # if unmet, auto-escalate to human
```

### P5: Autonomous Skill Sharpening (Adaptation Loop)
**Change**: `ward skill sharpen --all` scheduled daily.
```go
func SharpenAll() {
    for skill in ListGlobalSkills() {
        local := FindLocalArtifact(skill.Topic)
        if local == nil { continue }
        res := verification.Run(local, repo)
        if res.Status != "verified" {
            // update or demote
        }
    }
}
```
**Metric**: Skill verification pass rate over time.

### P6: Claim Age Metric & Auto-Release
**Change**: `brief` shows claim age; `tick` auto-releases >4h stale.
```
STALE CLAIM: task-798321b1 (age=144h) → AUTO-RELEASED
```

---

## 5. Structural Observability Gaps

| Signal | Currently | Needed |
|--------|-----------|--------|
| Cheap-hit rate | Never measured | Continuous, windowed |
| Verification pass rate | Manual query | Dashboard in `brief` |
| Escalation distribution | None | Histogram in `scorecard` |
| Claim turnaround time | None | p50/p95 in `scorecard` |
| Drift rate | Hidden (reports 0) | Honest count + rate |
| Skill freshness | Unknown | Last sharpened timestamp |
| Experiment gate status | Binary (met/unmet) | Progress %, time-to-deadline |

---

## 6. Recommended Architecture: Ward as a Proper Control System

```
┌─────────────────────────────────────────────────────────────────┐
│                    WARD CONTROL PLANE                           │
├─────────────────────────────────────────────────────────────────┤
│  REFERENCE GENERATOR                                            │
│    • Target cheap-hit rate (e.g., 30%)                          │
│    • Target verification pass rate (e.g., 90%)                  │
│    • Max claim age (e.g., 4h)                                   │
│    • Max escalations (2)                                        │
├─────────────────────────────────────────────────────────────────┤
│  CONTROLLER (routing.Route + tick + brief + scorecard)          │
│    • Proportional: tier escalation on failure                   │
│    • Integral: claim age → auto-release                         │
│    • Derivative: drift rate → verification aggressiveness       │
│    • Feedforward: skill install → local artifact → cheap vote   │
├─────────────────────────────────────────────────────────────────┤
│  PLANT (agent + repo)                                           │
│    • adapter.Run (opencode/Codex/Claude)                        │
│    • repo state (git, files)                                    │
├─────────────────────────────────────────────────────────────────┤
│  SENSORS (live verification)                                    │
│    • verify_cmd exit code (per artifact, per run)               │
│    • routing decision log (tier, memory_hit, verify_status)     │
│    • claim age, escalation count                                │
│    • drift detector (periodic re-verify)                        │
├─────────────────────────────────────────────────────────────────┤
│  OBSERVABILITY (brief + scorecard + timeline)                   │
│    • Real-time KPIs (cheap-hit, drift, claim-age, skill fresh)  │
│    • Trend plots (7d, 30d windows)                              │
│    • Alert on: drift>0, claim-age>4h, cheap-hit<target, gate-stalled>24h
└─────────────────────────────────────────────────────────────────┘
```

---

## 7. Immediate Action Plan (This Session Budget)

| Action | File | Effort | Verifies |
|--------|------|--------|----------|
| 1. Fix drift sensor in `tick`/`brief` | `tick.go`, `brief.go` | 30m | `brief` shows `drift=7` |
| 2. Auto-free expired claims in `tick` | `tick.go` | 20m | `brief` shows `expired-claims-freed>0` |
| 3. Add claim age to `brief` output | `brief.go` | 15m | Stale claims show age |
| 4. Lower H2 gate to internal | `.spec/simulation.md` | 10m | H2 can launch |
| 5. Add `ward skill install` skeleton | `skill.go` | 45m | Portable → local works |
| 6. Add `ward scorecard` skeleton | `scorecard.go` | 60m | Cheap-hit % visible |

---

## 8. Reflection: What This Session Reveals

The battlefield feedback (openai.md, claude, qwen) correctly identified **symptoms** (context reload, evidence injection, cross-project routing) but the **control system** view reveals the **structural causes**:

1. **No integral action on claims** → stale claims accumulate
2. **Biased drift sensor** → system thinks it's healthy when 87% of local knowledge is degraded
3. **Missing primary KPI** → can't steer toward thesis (cheap routing via memory)
4. **Open feedforward loop** → portable lessons exist but never close into feedback
5. **Unreachable references** → experiments stall, no watchdog
6. **No adaptation** → skills don't sharpen, lessons don't compound

Ward has excellent **component-level engineering** (pure router, atomic claims, live verify gate, tag-first retrieval) but weak **system-level control**. The autonomy improvements above close the loops at the system level.

**The fundamental thesis** — "verified memory enables cheaper routing" — is a **control objective**, not a feature. It requires:
- A measurable controlled variable (cheap-hit rate)
- A sensor (routing decision log aggregation)
- A controller (verification aggressiveness, retrieval tuning)
- An adaptation loop (skill sharpening, lesson localization)

Without these, Ward is a well-engineered component library, not an autonomous control plane.

---

## 9. Portable Lessons from This Reflection

These should be captured as `portable:control-*` artifacts:

1. `portable:control-antiwindup-claims` — claim TTL enforcement prevents integrator windup
2. `portable:control-drift-sensor-honesty` — drift = any deviation, not just verified→error
3. `portable:control-primary-kpi` — every thesis needs a measured controlled variable
4. `portable:control-feedforward-closure` — imported lessons must earn Local to enter feedback
5. `portable:control-watchdog-gates` — unreachable references need fallback or escalation
6. `portable:control-adaptation-loop` — static knowledge degrades; schedule sharpening

---

*End of reflection. Next session should pick up P0-P2 from the action plan.*