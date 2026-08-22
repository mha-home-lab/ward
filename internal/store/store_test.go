package store

import (
	"os"
	"testing"
)

// TestMigrationFromV1 reproduces the shipped bug: a database created by an
// earlier build already contains the escalation/context columns (they were in
// the create-statement then) but its user_version is 1. Re-opening must not
// error on INSERT, and the migrate path must skip the already-present columns.
func TestMigrationFromV1(t *testing.T) {
	home := t.TempDir()
	os.Setenv("WARD_HOME", home)
	t.Cleanup(func() { os.Unsetenv("WARD_HOME") })

	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	// Simulate an aadb0dc-era DB: columns present, but user_version left at 1.
	if _, err := s.DB.Exec("PRAGMA user_version=1"); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()

	s2, err := Open() // Init() -> migrate(): columns exist -> skip, set uv=2
	if err != nil {
		t.Fatal(err)
	}
	defer s2.DB.Close()

	if err := s2.UpsertRunNode(RunNode{RunID: "r", Node: "n", Status: "failed", Escalation: 2, UpdatedAt: "x"}); err != nil {
		t.Fatalf("INSERT with escalation failed (migration bug): %v", err)
	}
	if err := s2.AddRoutingDecision(RoutingDecision{RunID: "r", Node: "n", Tier: "strong", Context: `["id"]`, CreatedAt: "x"}); err != nil {
		t.Fatalf("INSERT with context failed (migration bug): %v", err)
	}

	nodes, _ := s2.LoadRunNodes("r")
	if len(nodes) == 0 || nodes[0].Escalation != 2 {
		t.Fatalf("escalation column not usable after migration: %+v", nodes)
	}
	decs, _ := s2.RoutingDecisionsForRun("r")
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
