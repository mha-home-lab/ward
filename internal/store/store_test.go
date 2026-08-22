package store

import (
	"database/sql"
	"os"
	"sync"
	"sync/atomic"
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

// TestClaimCasRace proves the claim reservation is safe across separate
// `ward` processes: eight independent Open() handles race on the same topic and
// exactly ONE wins, the rest get a conflict. This is the bug the check-then-
// insert path could not guarantee (the unique index on (claim_topic, project)
// is the arbiter, not app logic).
func TestClaimTopicAtomicRace(t *testing.T) {
	home := t.TempDir()
	os.Setenv("WARD_HOME", home)
	defer os.Unsetenv("WARD_HOME")

	// Warm up the schema/migration once. Each `ward` process opens the DB once
	// in real life; we mirror that by opening the 8 independent stores
	// SEQUENTIALLY (so Init/migrate don't race each other), then race only the
	// claim INSERTs — which is exactly the cross-process scenario we're testing.
	stores := make([]*Store, 8)
	for i := range stores {
		s, err := Open()
		if err != nil {
			t.Fatal(err)
		}
		stores[i] = s
	}
	defer func() {
		for _, s := range stores {
			s.DB.Close()
		}
	}()

	var wins, conflicts int32
	var wg sync.WaitGroup
	for _, s := range stores {
		wg.Add(1)
		go func(s *Store) {
			defer wg.Done()
			_, conflict, err := s.ClaimTopic("topicX", "", "racer", "")
			if err != nil {
				t.Error(err)
				return
			}
			if conflict {
				atomic.AddInt32(&conflicts, 1)
			} else {
				atomic.AddInt32(&wins, 1)
			}
		}(s)
	}
	wg.Wait()

	if got := atomic.LoadInt32(&wins); got != 1 {
		t.Fatalf("exactly one claim must win, got %d", got)
	}
	if got := atomic.LoadInt32(&conflicts); got != 7 {
		t.Fatalf("rest must conflict, got %d", got)
	}
	// And only one active claim exists on disk.
	s, _ := Open()
	defer s.DB.Close()
	ids, _ := s.ActiveClaimIDs("topicX", "")
	if len(ids) != 1 {
		t.Fatalf("expected 1 active claim on disk, got %v", ids)
	}
}

// TestClaimReleaseAndReclaim checks release frees the unique slot so the topic
// can be re-claimed, and that a second distinct claim on the same topic is
// blocked until then.
func TestClaimReleaseAndReclaim(t *testing.T) {
	home := t.TempDir()
	os.Setenv("WARD_HOME", home)
	defer os.Unsetenv("WARD_HOME")
	s, _ := Open()
	defer s.DB.Close()

	if _, conflict, err := s.ClaimTopic("t1", "", "a", ""); err != nil || conflict {
		t.Fatalf("first claim should win (conflict=%v err=%v)", conflict, err)
	}
	if _, conflict, _ := s.ClaimTopic("t1", "", "b", ""); !conflict {
		t.Fatal("second claim on same topic must conflict")
	}
	if n, err := s.ReleaseClaim("t1", ""); err != nil || n != 1 {
		t.Fatalf("release must free exactly one, got n=%d err=%v", n, err)
	}
	if ids, _ := s.ActiveClaimIDs("t1", ""); len(ids) != 0 {
		t.Fatalf("no active claim after release, got %v", ids)
	}
	// A different agent can now reclaim the freed topic.
	if _, conflict, _ := s.ClaimTopic("t1", "", "c", ""); conflict {
		t.Fatal("reclaim after release must succeed")
	}
}

// TestLegacyClaimCount confirms the one-time transition gap is observable: a
// claim written WITHOUT claim_topic (as pre-v0.4 claims are) is counted as
// legacy and stays invisible to the atomic path.
func TestLegacyClaimCount(t *testing.T) {
	home := t.TempDir()
	os.Setenv("WARD_HOME", home)
	defer os.Unsetenv("WARD_HOME")
	s, _ := Open()
	defer s.DB.Close()

	// A pre-v0.4-style claim: claim_topic left NULL (mimics the old UpsertArtifact path).
	if _, err := s.DB.Exec(`INSERT INTO artifacts (id, kind, summary, content, tags, status, created_by, created_at, local, claim_topic)
		VALUES ('claim:old', 'claim', 'old', 'x', '["claim","old",""]', 'accepted', 'a', 't', 1, NULL)`); err != nil {
		t.Fatal(err)
	}
	// A v0.4-style claim: has claim_topic, must NOT count as legacy.
	if _, conflict, err := s.ClaimTopic("new", "", "a", ""); err != nil || conflict {
		t.Fatalf("new claim should win (conflict=%v err=%v)", conflict, err)
	}

	n, err := s.LegacyClaimCount()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("exactly one legacy (NULL claim_topic) claim expected, got %d", n)
	}
}
