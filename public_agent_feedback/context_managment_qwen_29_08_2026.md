## Context Management for Long-Running Sessions

The current WARD approach handles **state persistence** (SQLite store, sidecar logs) but not **context management** (what the agent actually holds in working memory). This is a critical gap for long-running sessions.

Here's how I'd address it:

---

### Core Problem

AI agents have finite context windows. In long sessions, you accumulate:
- Task history (dozens of completed/failed tasks)
- Verification results (full logs, exit codes, durations)
- Memory artifacts (prior knowledge, skills, context)
- Routing decisions (why task X went to cheap vs. mid)
- Session handoffs (what the previous session did)

If you load everything, you hit token limits and the agent becomes confused. If you truncate, you lose critical state.

---

### Solution: Hierarchical Context Loading + Just-in-Time Retrieval

**Tier 1: Always Loaded (Core Context)**
- Current task (what am I working on right now?)
- Immediate next actions (what should I do next?)
- Critical constraints (AGENTS.md protocol, trust boundaries)
- Active claims (what topics am I working on?)

**Tier 2: Loaded On Demand (Working Context)**
- Related memory artifacts (prior knowledge for current topic)
- Recent verification results (last 3-5 runs, summarized)
- Task history for current topic (what did I try before?)
- Skills/chips relevant to current work

**Tier 3: Archived (Deep Context)**
- Full verification logs (sidecar files)
- Complete task history (all completed/failed tasks)
- Old memory artifacts (superseded knowledge)
- Rejected runs and dossiers

---

### Implementation: Enhanced `ward brief` + Context Query Interface

**1. Context-Aware `ward brief`**

Current `ward brief` loads everything. New version:

```bash
# Default: loads Tier 1 + Tier 2 summaries
ward brief

# Topic-specific: loads context for one topic only
ward brief --topic fix-login-redirect

# Deep dive: loads Tier 3 (full history, old logs)
ward brief --deep

# Compact: just Tier 1 (minimal context for quick decisions)
ward brief --compact
```

**Output structure:**
```
=== CORE CONTEXT (Tier 1) ===
Current task: fix-login-redirect (claimed by agent-1)
Next actions: 
  1. Run verify: go test ./pkg/login/... -v
  2. If pass: ward task done fix-login-redirect
  3. If fail: escalate to mid tier

Active claims:
  - fix-login-redirect (TTL: 45m remaining)
  - helm-chart-update (TTL: 30m remaining)

Protocol constraints:
  - NO PHANTOM RUNS: verify_cmd must exercise real change
  - EXCLUSIVE WORK: claim topic before touching shared files

=== WORKING CONTEXT (Tier 2) ===
Related memory (fix-login-redirect):
  - "Login redirect uses OAuth2 PKCE flow" (verified 2h ago)
  - "Test coverage: 85% for pkg/login/" (verified 1h ago)

Recent verification results:
  - Task fix-login-redirect: go test PASS (exit 0, 1.2s, 2026-08-29T10:00:00Z)
    Full log: .ward/logs/run123_node456_20260829T100000Z.log
  - Task helm-chart-update: helm lint PASS (exit 0, 0.8s, 2026-08-29T09:45:00Z)
    Full log: .ward/logs/run122_node455_20260829T094500Z.log

Task history (fix-login-redirect):
  - Attempt 1: FAILED (exit 1, missing import) → escalated to mid
  - Attempt 2: PASS (exit 0) → verified

=== DEEP CONTEXT (Tier 3, not loaded by default) ===
To load full history: ward brief --deep
To query specific logs: cat .ward/logs/<log-file>
```

**2. Context Query Interface**

Instead of loading everything, the agent queries for what it needs:

```bash
# Natural language query over the store
ward context query "What verified tasks touch login.py?"
ward context query "What did I learn about OAuth2 in the last 24h?"
ward context query "Show me all failed attempts for fix-login-redirect"

# Structured queries
ward context query --topic fix-login-redirect --type verification --last 5
ward context query --tag oauth2 --verified-only
```

**3. Context Summarization**

After completing a task, compress the detailed context into a summary:

```bash
# Summarize a completed task (compresses detailed logs into a memory artifact)
ward memory summarize fix-login-redirect

# Output:
# Created memory artifact: "fix-login-redirect: OAuth2 PKCE flow, tests pass, 
# coverage 85%. Verified 2026-08-29T10:00:00Z. Full logs archived."
```

**4. Context Budget Tracking**

Track how much context is loaded and warn when approaching limits:

```bash
ward context status

# Output:
# Context usage: 12,450 / 32,000 tokens (39%)
# Tier 1 (core): 2,100 tokens
# Tier 2 (working): 10,350 tokens
# Tier 3 (archived): not loaded
# 
# Recommendation: context is healthy, no action needed
```

**5. Topic-Based Context Isolation**

Prevent cross-topic pollution by isolating context per topic:

```bash
# Switch to a different topic's context
ward context switch helm-chart-update

# This loads helm-chart-update's Tier 1/2 context and offloads fix-login-redirect's context
# The agent now has a clean context for the new topic
```

---

### What the CLI Agent Should Build

**Priority 1: Enhance `ward brief` with tiered loading**
- Add `--topic`, `--deep`, `--compact` flags
- Structure output into Tier 1/2/3 sections
- Load only what's needed for the current decision

**Priority 2: Add `ward context query` command**
- Natural language query interface over the store
- Returns targeted slices of context, not everything
- Supports structured queries (topic, type, time range)

**Priority 3: Add `ward memory summarize` command**
- Compresses detailed task history into a memory artifact
- Archives full logs, keeps summary in working context
- Prevents context bloat from accumulated task history

**Priority 4: Add `ward context status` command**
- Tracks token usage (estimate based on content length)
- Warns when approaching context limits
- Suggests summarization or offloading

**Priority 5: Add `ward context switch` command**
- Isolates context per topic
- Offloads previous topic's context
- Prevents cross-topic pollution

---

### What the CLI Agent Should NOT Do

**Don't load everything into context by default.** The current `ward brief` loads all open runs, all claims, all memory artifacts. This is fine for short sessions but breaks down in long sessions. Use tiered loading.

**Don't store full logs in the database.** The sidecar logs are already on disk. Don't duplicate them in SQLite. Load them on demand via `cat` or `ward context query`.

**Don't let context grow unbounded.** Implement summarization and offloading. After completing a task, compress the detailed context into a summary. Archive the details.

**Don't mix topics in the working context.** If the agent is working on `fix-login-redirect`, don't load `helm-chart-update`'s verification results. Use topic-based isolation.

---

### Summary

Context management is about **what the agent holds in working memory**, not just **what the system persists**. The current WARD handles persistence well (SQLite, sidecar logs) but doesn't manage working memory effectively.

The solution is hierarchical loading (Tier 1/2/3), just-in-time retrieval (query interface), and proactive offloading (summarization, topic isolation). This keeps the agent's context focused and prevents token bloat in long-running sessions.
