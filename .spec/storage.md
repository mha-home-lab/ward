# storage — Unified SQLite Schema

| | |
|---|---|
| Status | Implemented through v4 (see migration log) |
| Domain | storage |
| Version | 0.5.0 |

## Purpose

Define the single durable store (`ward.db` under `WARD_HOME`) that holds both
chef-derived memory (artifacts) and ciao-derived execution state (runs, channel
items, definitions) plus the router's decision log. One file, additive
migrations, crash-safe.

## What's kept from chef

- SQLite as the backing store; a single file under a configurable home
  (`CHEF_HOME` → `WARD_HOME`, default `.ward`).
- **Content-hash short ids** (`sha256(kind|summary|content)[:8]`) for
  idempotent dedup (`INSERT OR IGNORE`).
- **`PRAGMA user_version` additive migrations** — never `ALTER` destructively;
  add columns, bump version.
- **FTS5** virtual table over artifacts (`summary`, `content`, `tags`) with
  insert/update/delete triggers; LIKE fallback when FTS is unavailable.
- Provenance columns: `source_session`, `source_agent`, `project` lens.
- `verify_cmd` / `verify_status` / `verify_at` columns (chef v1.0.0).
- `used_count` / `last_used` reuse tracking; TTL (`expires_at`).
- Lifecycle columns: `status`, `superseded_by`, `superseded_reason`,
  `superseded_at`, `promoted_at`, `promoted_by`, `promoted_reason`.

## What's kept from ciao

- The *concept* of durable run state, channel items, and workflow/agent/skill
  definitions — but the durable medium changes (see "Changed").
- Run history as an append-only event list (`RunEvent` analogue) for audit and
  crash recovery.

## What's changed and why

- **Unified store.** ciao persists runs as one YAML file per run under
  `.ciao/state/runs` and channel items as YAML under `.ciao/channels`. WARD
  moves *all* of this into `ward.db` tables (`runs`, `run_events`,
  `channels`, `channel_items`, `workflows`, `agents`, `skills`). Reason: chef
  already owns a SQLite store; a second filesystem store duplicates the crash-
  recovery problem and prevents the router from joining memory + runs in one
  query. One transactional file (WAL mode) gives ciao's restart-safety for free.
- **`runs` table** holds run-level identity/status (`id, workflow_name, status,
  waiting_approval_id, current_item_id, ceremony_level, created_at, updated_at`).
  Per-node state (current node, pending/completed sets, `touched` files,
  per-node `ceremony_level`) lives in the separate **`run_nodes`** table — see
  D0.2 in `tasks.md`, which supersedes the earlier "whole-run JSON slice" design.
  This is what makes fan-out width and shared-state overlap queryable for the
  router (routing.md signal #3).
- **`channel_items` table**: `id, channel, run_id, parent_id, type, status,
  payload (JSON), created_at`. Replaces ciao's per-channel YAML files; enables
  dedup-on-resume by `run_id` (ciao already did this by scanning files).
- **`routing_decisions` table** (new, native): `id, run_id, node, tier, model,
  ceremony_level, memory_hit (bool), verify_status, escalated_from, reason,
  created_at`. This is the router's audit/learning log (routing.md). No
  equivalent in either source.
- **`verify` results** live on the artifact row (`verify_status`, `verify_at`)
  plus an optional `verify_log` table for per-run verify outcomes so `resume`
  can show freshness.
- **Contention model** — *resolved by D0.2 (tasks.md)*: per-node state lives in
  `run_nodes` (one row per DAG node per run, carrying `touched` + `ceremony_level`),
  and the router's scoring inputs are persisted in `routing_decisions
  .contention_inputs`. No separate `node_state` table, and no in-memory-only
  computation (the earlier "computed in memory" note is superseded).

## Proposed Schema Sketch

> Source of truth for execution state: **D0.2 in `tasks.md`**. The per-node
> `run_nodes` table (not `runs.pending_nodes`/`completed_nodes` JSON slices)
> carries node state, `touched` files, and `ceremony_level`.

```sql
PRAGMA user_version = 1;

CREATE TABLE artifacts (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  summary TEXT NOT NULL,
  content TEXT,
  tags TEXT NOT NULL DEFAULT '[]',
  status TEXT NOT NULL DEFAULT 'proposed',
  created_by TEXT,
  created_at TEXT NOT NULL,
  used_count INTEGER NOT NULL DEFAULT 0,
  last_used TEXT,
  superseded_by TEXT,
  superseded_reason TEXT,
  superseded_at TEXT,
  promoted_at TEXT, promoted_by TEXT, promoted_reason TEXT,
  source_session TEXT, source_agent TEXT,
  project TEXT,
  expires_at TEXT,
  verify_cmd TEXT,
  verify_status TEXT,          -- verified | stale | error | unknown
  verify_at TEXT,
  ceremony_level TEXT DEFAULT 'light'
);

CREATE VIRTUAL TABLE artifacts_fts USING fts5(
  id UNINDEXED, kind UNINDEXED, summary, content, tags);

CREATE TABLE runs (
  id TEXT PRIMARY KEY,
  workflow_name TEXT NOT NULL,
  status TEXT NOT NULL,
  waiting_approval_id TEXT,
  current_item_id TEXT,
  ceremony_level TEXT DEFAULT 'light',   -- run-level default; per-node override in run_nodes
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE run_nodes (
  id INTEGER PRIMARY KEY,
  run_id TEXT NOT NULL,
  node TEXT NOT NULL,
  status TEXT NOT NULL,           -- pending|ready|running|awaiting_approval|done|failed|skipped
  touched TEXT DEFAULT '[]',      -- JSON: agent-declared files read/written (D0.1)
  ceremony_level TEXT DEFAULT 'light',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE run_events (
  id INTEGER PRIMARY KEY,
  run_id TEXT NOT NULL,
  at TEXT NOT NULL,
  action TEXT,
  node TEXT,
  channel TEXT,
  item_id TEXT,
  detail TEXT
);

CREATE TABLE channel_items (
  id TEXT PRIMARY KEY,
  channel TEXT NOT NULL,
  run_id TEXT NOT NULL,
  parent_id TEXT,
  type TEXT,
  status TEXT,
  payload TEXT,               -- JSON (includes agent-declared 'touched')
  created_at TEXT NOT NULL
);

CREATE TABLE workflows (name TEXT PRIMARY KEY, def TEXT);  -- YAML/JSON def
CREATE TABLE agents   (name TEXT PRIMARY KEY, def TEXT);
CREATE TABLE skills   (name TEXT PRIMARY KEY, content TEXT);

CREATE TABLE routing_decisions (
  id INTEGER PRIMARY KEY,
  run_id TEXT, node TEXT,
  tier TEXT, model TEXT, ceremony_level TEXT,
  memory_hit INTEGER, verify_status TEXT,
  escalated_from TEXT, reason TEXT,
  contention_inputs TEXT,          -- JSON: branching_factor, shared_overlap, touched_files (D0.2)
  created_at TEXT NOT NULL
);
```

## Migrations shipped (additive, `PRAGMA user_version`)

The sketch above is the v1 baseline. Later versions arrived as additive steps
(`ALTER TABLE ... ADD COLUMN` if-missing / `CREATE TABLE IF NOT EXISTS` /
`CREATE INDEX IF NOT EXISTS`), never a rewrite:

- **v1 → v2:** `run_nodes.escalation` (retry budget), `routing_decisions.context`
  (verified artifact ids only — never failed-attempt prose), `runs.workflow_path`
  (so `resume`/`approve` reload the originating YAML in a second session).
- **v2 → v3 (broker.md §1):** `artifacts.claim_topic` + **unique index
  `uni_claim_topic(claim_topic, project)`** — the atomic claim lock. SQLite
  treats NULLs as distinct, so the index constrains only *active* claims; release
  and expiry sweep set `claim_topic = NULL` to free the slot.
- **v3 → v4 (broker.md §4):** the dispatch pool:

```sql
CREATE TABLE tasks (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  kind TEXT DEFAULT 'default',
  tier_floor TEXT DEFAULT 'mid',   -- admission floor: cheap|mid|strong
  tier_rank INTEGER DEFAULT 1,     -- 0..2 mirror for SQL comparison
  status TEXT NOT NULL DEFAULT 'open',  -- open|claimed|done|rejected
  claimed_by TEXT,
  claimed_at TEXT,
  workflow_path TEXT,
  verify_cmd TEXT,
  run TEXT,
  escalation INTEGER DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
```

Also shipped in this window: `Store.OpenRuns()` (open runs for handoff/brief)
and `Store.LoadEvents(runID)` (ordered audit events — the raw material for
`ward explain` and the reject dossier).

## Migration approach

- `init` is idempotent: create tables if absent, then run additive
  `user_version` steps (chef's `migrate()` pattern). Never rewrite existing
  rows destructively.
- Each new column in a later version is an `ALTER TABLE ... ADD COLUMN` +
  `PRAGMA user_version = N`.

## Open questions / risks

- **Blob store dropped for v1.** chef kept >64 KiB content in `blobs/`. WARD
  artifacts are expected small; storing inline is simpler. If large artifacts
  appear, do we add blobs later, or rely on SQLite page limits? *Judgment call
  deferred* — leaning "inline only in v1, revisit if needed."
- **SQLite concurrency under many parallel agents.** WAL mode is assumed; need
  to confirm write contention when several `agent process` calls run
  concurrently. Risk if agents share one `ward.db` path.
- **`node_state` contention cache** — derived from workflow def + git; is git
  state reliable enough, or should agents declare their file touched-set
  explicitly? Hybrid likely.
- **FTS5 availability** in the target SQLite build (Go's `modernc.org/sqlite`
  or `mattn/go-sqlite3`); need to confirm FTS5 is compiled in, else LIKE-only
  fallback (chef already handles this).
