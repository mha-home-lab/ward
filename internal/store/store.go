package store

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Home returns the WARD_HOME directory (.ward by default).
func Home() string {
	if h := os.Getenv("WARD_HOME"); h != "" {
		return h
	}
	return ".ward"
}

// Store wraps the single SQLite database that holds memory, runs, and router log.
type Store struct {
	DB   *sql.DB
	Home string
}

// Open opens (creating if needed) the ward.db and ensures schema.
func Open() (*Store, error) {
	return openDB(filepath.Join(Home(), "ward.db"))
}

// Init creates the base schema + FTS5 triggers, then applies additive
// migrations idempotently up to the current user_version. It never rewrites an
// existing table: new columns arrive via ALTER so databases opened from an
// earlier build keep working.
func (s *Store) Init() error {
	if _, err := s.DB.Exec(schemaSQL); err != nil {
		return err
	}
	if _, err := s.DB.Exec(ftsTriggers); err != nil {
		return err
	}
	return s.migrate()
}

// migrate applies version N -> N+1 steps until current. Each step only adds a
// column if it is missing, so re-running on an already-migrated DB is a no-op.
func (s *Store) migrate() error {
	// v1 -> v2: escalation tracking on run_nodes, verified-only context on
	// routing_decisions, and the originating workflow path on runs so a second
	// session can resume/approve without re-supplying --workflow.
	if err := s.addColumn("run_nodes", "escalation", "INTEGER DEFAULT 0"); err != nil {
		return err
	}
	if err := s.addColumn("routing_decisions", "context", "TEXT"); err != nil {
		return err
	}
	if err := s.addColumn("runs", "workflow_path", "TEXT"); err != nil {
		return err
	}
	// v2 -> v3: claim_topic enables an atomic, cross-process claim reservation.
	// A unique index on (claim_topic, project) enforces "at most one active
	// claim per (topic, project)" in the database itself, so parallel `ward`
	// processes can't both win a check-then-insert race.
	if err := s.addColumn("artifacts", "claim_topic", "TEXT"); err != nil {
		return err
	}
	if _, err := s.DB.Exec("CREATE UNIQUE INDEX IF NOT EXISTS uni_claim_topic ON artifacts(claim_topic, project)"); err != nil {
		return err
	}
	// v3 -> v4: the dispatch pool. Tasks are claimable work items; the pool is
	// what turns tier routing into a fleet lever (admission = agent budget vs
	// task floor, atomicity = conditional UPDATE on status='open').
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS tasks (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		kind TEXT DEFAULT 'default',
		tier_floor TEXT DEFAULT 'mid',
		tier_rank INTEGER DEFAULT 1,
		status TEXT NOT NULL DEFAULT 'open',
		claimed_by TEXT,
		claimed_at TEXT,
		workflow_path TEXT,
		verify_cmd TEXT,
		run TEXT,
		escalation INTEGER DEFAULT 0,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	// v4 -> v5 (broker.md §4, rd:compounding): task topic tags propagate to
	// node tags and then to captured artifacts, so verified knowledge can
	// vouch across tasks sharing a topic instead of dying with its task id.
	if err := s.addColumn("tasks", "tags", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	// v5 -> v6 (external review): persist a semantic hash of the workflow
	// definition at run start, so resume/approve can refuse to continue a run
	// under a DIFFERENT definition than the one that created it (the YAML
	// file living at workflow_path is mutable state; the run's identity is
	// not). Immutable after insert; empty for pre-v6 runs (no guard possible,
	// stated honestly rather than faked).
	if err := s.addColumn("runs", "workflow_hash", "TEXT"); err != nil {
		return err
	}
	// v6 -> v7 (transparency patch): map a task to its most recent run so the
	// audit window (`ward task show`) and the pre-close gate can locate the
	// sidecar evidence file without guessing. Additive; old runs stay untagged.
	if err := s.addColumn("tasks", "last_run_id", "TEXT"); err != nil {
		return err
	}
	// v7: verification "backed" status is DERIVED FROM DISK, not stored in the
	// db. A run is backed iff a sidecar log exists under WARD_HOME/logs for its
	// id; otherwise it is a trusted pre-evidence completion (it predates the
	// sidecar feature and the run's own DB status is its proof). There is no
	// evidence column to migrate, so historical runs are never branded "legacy"
	// or downgraded — the disk is the single source of truth for evidence.
	_, err := s.DB.Exec("PRAGMA user_version = 7")
	if err != nil {
		return err
	}
	// v8 (context-reload / mid-task checkpoint): a checkpoint is authored,
	// mid-session state that does NOT exist on disk — it is the agent's explicit
	// "here's what I've learned, let me shed the raw exploration" note. That is a
	// different thing from run evidence (which is disk-derived); a table is the
	// correct home here, not a file.
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS checkpoints (
		id         INTEGER PRIMARY KEY AUTOINCREMENT,
		task_id    TEXT NOT NULL,
		seq        INTEGER NOT NULL,
		summary    TEXT NOT NULL,
		verify_cmd TEXT,
		exit_code  INTEGER,
		at         TEXT NOT NULL
	)`); err != nil {
		return err
	}
	_, err = s.DB.Exec("PRAGMA user_version = 8")
	// v9 (control-scorecard/kpis): execution_success on routing_decisions
	// enables the cheap-hit KPI (tier='cheap' AND ran successfully) without a
	// run-status join. Additive and nullable: decisions recorded before this
	// migration stay NULL (unknown outcome) rather than guessed.
	if err := s.addColumn("routing_decisions", "execution_success", "INTEGER"); err != nil {
		return err
	}
	_, err = s.DB.Exec("PRAGMA user_version = 9")
	if err != nil {
		return err
	}
	// v10 (control-transferability-lint): a pack --force override reason for a
	// portable artifact is recorded ON the artifact so the lint gate's escape
	// hatch stays auditable (a wall with no trace of why an exception was made
	// is a wall that gets silently patched). Additive and nullable.
	if err := s.addColumn("artifacts", "override_reason", "TEXT"); err != nil {
		return err
	}
	_, err = s.DB.Exec("PRAGMA user_version = 10")
	if err != nil {
		return err
	}
	// v11 (control-capture-loop): handoff_log records one row per `ward memory
	// handoff` so the NEXT call can detect a capture gap — commits happened with
	// no new artifacts captured since the previous handoff. The gap check is
	// deterministic (a count, never a semantic judgment) and warn-only. The
	// capture_gap flag is persisted on the row so a LATER `ward brief` can
	// surface the previous session's gap to the next agent (the loop-closer),
	// not just warn the acting agent at its own handoff.
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS handoff_log (
		id           INTEGER PRIMARY KEY AUTOINCREMENT,
		at           TEXT NOT NULL,
		head_sha     TEXT NOT NULL,
		capture_gap  INTEGER NOT NULL DEFAULT 0,
		commits      INTEGER NOT NULL DEFAULT 0
	)`); err != nil {
		return err
	}
	_, err = s.DB.Exec("PRAGMA user_version = 11")
	if err != nil {
		return err
	}
	// v12 (control-recurrence-links): recurrences records agent-DECLARED
	// links from a later capture (from_id) confirming an earlier lesson
	// (of_id) as "the same trap, surfaced differently". Ward never detects
	// these — only records what an agent asserts via `--recurs`. Many-to-one
	// (several captures can confirm one original), deliberately distinct from
	// superseded_by (1:1, "this replaces that"). RecurrenceCount aggregates the
	// confirmations into a deterministic promotion signal ("independently
	// confirmed N times").
	if _, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS recurrences (
		id       INTEGER PRIMARY KEY AUTOINCREMENT,
		of_id    TEXT NOT NULL,   -- the earlier artifact being confirmed
		from_id  TEXT NOT NULL,   -- the new capture that recognized it
		note     TEXT,            -- optional: how the surface form differed
		at       TEXT NOT NULL
	)`); err != nil {
		return err
	}
	_, err = s.DB.Exec("PRAGMA user_version = 12")
	return err
}

// addColumn adds the column only if it is not already present.
func (s *Store) addColumn(table, col, typ string) error {
	rows, err := s.DB.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	has := false
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if name == col {
			has = true
		}
	}
	if has {
		return nil
	}
	_, err = s.DB.Exec("ALTER TABLE " + table + " ADD COLUMN " + col + " " + typ)
	return err
}

// --- types ---

type Artifact struct {
	ID             string
	Kind           string
	Summary        string
	Content        string
	Tags           []string
	Status         string // proposed | accepted | superseded
	CreatedBy      string
	CreatedAt      string
	UsedCount      int
	LastUsed       string
	SupersededBy   string
	SupersededRsn  string
	SupersededAt   string
	PromotedAt     string
	PromotedBy     string
	PromotedRsn    string
	SourceSession  string
	SourceAgent    string
	Project        string
	ExpiresAt      string
	VerifyCmd      string
	VerifyKind     string // shell|grep|build|test|hash
	VerifyStatus   string // verified|stale|error|unknown
	VerifyAt       string
	Ceremony       string // light|full
	Local          bool   // trust boundary: store-local artifacts only get verify_cmd executed
	OverrideReason string // pack --force --reason: why a portable lint gate was overridden
}

func parseTags(s string) []string {
	var t []string
	if s == "" {
		return []string{}
	}
	_ = json.Unmarshal([]byte(s), &t)
	if t == nil {
		t = []string{}
	}
	return t
}

// RunState mirrors a workflow run; per-node state lives in run_nodes.
type RunState struct {
	ID              string
	WorkflowName    string
	WorkflowPath    string // file the run was started from; used to reload on resume/approve
	WorkflowHash    string // semantic hash of the definition at start; guards resume drift
	Status          string // running|awaiting_approval|completed|rejected
	WaitingApproval string
	CurrentItem     string
	Ceremony        string
	CreatedAt       string
	UpdatedAt       string
}

type RunNode struct {
	RunID       string
	Node        string
	Status      string // pending|ready|running|awaiting_approval|done|failed
	Touched     []string
	Ceremony    string
	DeclaredObs string // git-diff observed delta (observation only, D0.1)
	Escalation  int    // retries already applied to this node
	UpdatedAt   string
}

type RoutingDecision struct {
	RunID          string
	Node           string
	Tier           string
	Model          string
	Ceremony       string
	MemoryHit      bool
	VerifyStatus   string
	Contention     bool
	EscalatedFrom  string
	Reason         string
	Context        string // JSON list of verified artifact ids used as context (never failed-attempt prose)
	ContentionJSON string
	CreatedAt      string
}

// schemaSQL is the v1 schema (D0.2: per-node run_nodes, not whole-run blobs).
const schemaSQL = `
CREATE TABLE IF NOT EXISTS artifacts (
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
  verify_kind TEXT,
  verify_status TEXT,
  verify_at TEXT,
  ceremony_level TEXT DEFAULT 'light',
  local INTEGER DEFAULT 1
);

CREATE VIRTUAL TABLE IF NOT EXISTS artifacts_fts USING fts5(
  id UNINDEXED, kind UNINDEXED, summary, content, tags);

CREATE TABLE IF NOT EXISTS runs (
  id TEXT PRIMARY KEY,
  workflow_name TEXT NOT NULL,
  status TEXT NOT NULL,
  waiting_approval_id TEXT,
  current_item_id TEXT,
  ceremony_level TEXT DEFAULT 'light',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS run_nodes (
  run_id TEXT NOT NULL,
  node TEXT NOT NULL,
  status TEXT NOT NULL,
  touched TEXT DEFAULT '[]',
  ceremony_level TEXT DEFAULT 'light',
  declared_obs TEXT DEFAULT '',
  updated_at TEXT NOT NULL,
  PRIMARY KEY (run_id, node)
);

CREATE TABLE IF NOT EXISTS run_events (
  id INTEGER PRIMARY KEY,
  run_id TEXT NOT NULL,
  at TEXT NOT NULL,
  action TEXT,
  node TEXT,
  channel TEXT,
  item_id TEXT,
  detail TEXT
);

CREATE TABLE IF NOT EXISTS channel_items (
  id TEXT PRIMARY KEY,
  channel TEXT NOT NULL,
  run_id TEXT NOT NULL,
  parent_id TEXT,
  type TEXT,
  status TEXT,
  payload TEXT,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS workflows (name TEXT PRIMARY KEY, def TEXT);
CREATE TABLE IF NOT EXISTS agents (name TEXT PRIMARY KEY, def TEXT);
CREATE TABLE IF NOT EXISTS skills (name TEXT PRIMARY KEY, content TEXT);

CREATE TABLE IF NOT EXISTS routing_decisions (
  id INTEGER PRIMARY KEY,
  run_id TEXT, node TEXT,
  tier TEXT, model TEXT, ceremony_level TEXT,
  memory_hit INTEGER, verify_status TEXT,
  contention INTEGER, escalated_from TEXT, reason TEXT,
  contention_inputs TEXT,
  created_at TEXT NOT NULL
);
`

const ftsTriggers = `
CREATE TRIGGER IF NOT EXISTS artifacts_fts_ai AFTER INSERT ON artifacts BEGIN
  INSERT INTO artifacts_fts(rowid, id, kind, summary, content, tags)
  VALUES (new.rowid, new.id, new.kind, coalesce(new.summary,''), new.content, new.tags);
END;
CREATE TRIGGER IF NOT EXISTS artifacts_fts_ad AFTER DELETE ON artifacts BEGIN
  DELETE FROM artifacts_fts WHERE rowid = old.rowid;
END;
CREATE TRIGGER IF NOT EXISTS artifacts_fts_au AFTER UPDATE ON artifacts BEGIN
  DELETE FROM artifacts_fts WHERE rowid = old.rowid;
  INSERT INTO artifacts_fts(rowid, id, kind, summary, content, tags)
  VALUES (new.rowid, new.id, new.kind, coalesce(new.summary,''), new.content, new.tags);
END;
`

// idFor computes the content-hash short id (dedup key).
func idFor(kind, summary, content string) string {
	return sha8(kind + "|" + summary + "|" + content)
}

func joinTags(t []string) string {
	b, _ := json.Marshal(t)
	return string(b)
}

func stringsContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// splitSep splits "a::b" style verify payloads.
func splitSep(s, sep string) (string, string) {
	parts := strings.SplitN(s, sep, 2)
	if len(parts) == 2 {
		return parts[0], parts[1]
	}
	return s, ""
}
