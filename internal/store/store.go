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
	home := Home()
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, err
	}
	path := filepath.Join(home, "ward.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db, Home: home}
	if err := s.Init(); err != nil {
		return nil, err
	}
	return s, nil
}

// Init creates schema + FTS5 triggers and sets user_version. Idempotent.
func (s *Store) Init() error {
	if _, err := s.DB.Exec(schemaSQL); err != nil {
		return err
	}
	if _, err := s.DB.Exec(ftsTriggers); err != nil {
		return err
	}
	_, err := s.DB.Exec("PRAGMA user_version = 1")
	return err
}

// --- types ---

type Artifact struct {
	ID            string
	Kind          string
	Summary       string
	Content       string
	Tags          []string
	Status        string // proposed | accepted | superseded
	CreatedBy     string
	CreatedAt     string
	UsedCount     int
	LastUsed      string
	SupersededBy  string
	SupersededRsn string
	SupersededAt  string
	PromotedAt    string
	PromotedBy    string
	PromotedRsn   string
	SourceSession string
	SourceAgent   string
	Project       string
	ExpiresAt     string
	VerifyCmd     string
	VerifyKind    string // shell|grep|build|test|hash
	VerifyStatus  string // verified|stale|error|unknown
	VerifyAt      string
	Ceremony      string // light|full
	Local         bool   // trust boundary: store-local artifacts only get verify_cmd executed
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
	Status          string // running|awaiting_approval|completed|rejected
	WaitingApproval string
	CurrentItem     string
	Ceremony       string
	CreatedAt       string
	UpdatedAt       string
}

type RunNode struct {
	RunID        string
	Node         string
	Status       string // pending|ready|running|awaiting_approval|done|failed|skipped
	Touched      []string
	Ceremony     string
	DeclaredObs  string // git-diff observed delta (observation only, D0.1)
	UpdatedAt    string
}

type RoutingDecision struct {
	RunID          string
	Node           string
	Tier           string
	Model          string
	Ceremony       string
	MemoryHit      bool
	VerifyStatus   string
	EscalatedFrom  string
	Reason         string
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
  escalated_from TEXT, reason TEXT,
  contention_inputs TEXT,
  created_at TEXT NOT NULL
);
`

const ftsTriggers = `
DROP TRIGGER IF EXISTS artifacts_fts_ai;
DROP TRIGGER IF EXISTS artifacts_fts_ad;
DROP TRIGGER IF EXISTS artifacts_fts_au;
CREATE TRIGGER artifacts_fts_ai AFTER INSERT ON artifacts BEGIN
  INSERT INTO artifacts_fts(rowid, id, kind, summary, content, tags)
  VALUES (new.rowid, new.id, new.kind, coalesce(new.summary,''), new.content, new.tags);
END;
CREATE TRIGGER artifacts_fts_ad AFTER DELETE ON artifacts BEGIN
  DELETE FROM artifacts_fts WHERE rowid = old.rowid;
END;
CREATE TRIGGER artifacts_fts_au AFTER UPDATE ON artifacts BEGIN
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
