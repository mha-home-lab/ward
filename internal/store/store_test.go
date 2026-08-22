package store

import (
	"database/sql"
	"os"
	"testing"
)

// TestMigrationFromV1 reproduces the shipped bug: a database created by the
// aadb0dc-era build LACKED the escalation/context/workflow_path columns (they
// were added later via ALTER + user_version), but its create-statement used
// IF NOT EXISTS so re-opening against a newer binary must ADD the missing
// columns idempotently and then accept INSERTs into them.
func TestMigrationFromV1(t *testing.T) {
	home := t.TempDir()
	os.Setenv("WARD_HOME", home)
	t.Cleanup(func() { os.Unsetenv("WARD_HOME") })

	// Build a true aadb0dc-era DB by opening a raw connection (bypassing
	// Store.Init) and creating the three migrated tables WITHOUT the columns the
	// later migration adds (escalation, context, workflow_path).
	dbPath := home + "/ward.db"
	raw, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	v1 := `
	CREATE TABLE runs (
		id TEXT PRIMARY KEY, workflow_name TEXT NOT NULL, status TEXT NOT NULL,
		waiting_approval_id TEXT, current_item_id TEXT, ceremony_level TEXT DEFAULT 'light',
		created_at TEXT NOT NULL, updated_at TEXT NOT NULL
	);
	CREATE TABLE run_nodes (
		run_id TEXT NOT NULL, node TEXT NOT NULL, status TEXT NOT NULL,
		touched TEXT DEFAULT '[]', ceremony_level TEXT DEFAULT 'light',
		declared_obs TEXT DEFAULT '', updated_at TEXT NOT NULL,
		PRIMARY KEY (run_id, node)
	);
	CREATE TABLE routing_decisions (
		id INTEGER PRIMARY KEY, run_id TEXT, node TEXT, tier TEXT, model TEXT,
		ceremony_level TEXT, memory_hit INTEGER, verify_status TEXT,
		contention INTEGER, escalated_from TEXT, reason TEXT,
		contention_inputs TEXT, created_at TEXT NOT NULL
	);`
	if _, err := raw.Exec(v1); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	// Re-open with the current binary: migrate() must ALTER the missing columns
	// in and set user_version=2, without erroring on the pre-existing schema.
	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()

	if err := s.UpsertRunNode(RunNode{RunID: "r", Node: "n", Status: "failed", Escalation: 2, UpdatedAt: "x"}); err != nil {
		t.Fatalf("INSERT with escalation failed (migration bug): %v", err)
	}
	if err := s.AddRoutingDecision(RoutingDecision{RunID: "r", Node: "n", Tier: "strong", Context: `["id"]`, CreatedAt: "x"}); err != nil {
		t.Fatalf("INSERT with context failed (migration bug): %v", err)
	}

	nodes, _ := s.LoadRunNodes("r")
	if len(nodes) == 0 || nodes[0].Escalation != 2 {
		t.Fatalf("escalation column not usable after migration: %+v", nodes)
	}
	decs, _ := s.RoutingDecisionsForRun("r")
	if len(decs) == 0 || decs[0].Context != `["id"]` {
		t.Fatalf("context column not usable after migration: %+v", decs)
	}

	// Cross-session idempotency: open a third time, no error, still works.
	s3, err := Open()
	if err != nil {
		t.Fatalf("third open failed: %v", err)
	}
	defer s3.DB.Close()
	var n int
	if err := s3.DB.QueryRow("SELECT count(*) FROM run_nodes WHERE run_id='r'").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("expected 1 run_node after re-open, got %d", n)
	}
}
