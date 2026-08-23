package orchestration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mha-home-lab/ward/internal/store"
)

func testStoreOrch(t *testing.T) *store.Store {
	t.Helper()
	t.Setenv("WARD_HOME", t.TempDir())
	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// rd:c2 bfd02833: a non-executable check must be rejected WITHOUT any
// escalation attempt — stronger tiers cannot execute a broken gate either.
func TestPreFlightBrokenCheckRejectsImmediately(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.24\n"), 0o644)
	eng := &Engine{Store: testStoreOrch(t), RepoRoot: dir, AutoApprove: true}
	wf := &Workflow{
		Name:  "broken-gate",
		Nodes: []Node{{ID: "start", Kind: "channel"}, {ID: "work", Kind: "test", Run: "go test ./does-not-exist_test.go"}, {ID: "done", Kind: "channel"}},
		Edges: []Edge{{From: "start", To: "work"}, {From: "work", To: "done"}},
	}
	runID, err := eng.StartWorkflow(wf)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := eng.Store.LoadRun(runID)
	if r.Status != "rejected" {
		t.Fatalf("broken-check run must reject, got %s", r.Status)
	}
	decs, _ := eng.Store.RoutingDecisionsForRun(runID)
	workAttempts := 0
	for _, d := range decs {
		if d.Node == "work" {
			workAttempts++
		}
	}
	if workAttempts != 1 {
		t.Fatalf("pre-flight must stop after ONE attempt, got %d", workAttempts)
	}
	events, _ := eng.Store.LoadEvents(runID)
	found := false
	for _, e := range events {
		if strings.Contains(e.Detail, "preflight:") {
			found = true
		}
	}
	if !found {
		t.Fatal("expected preflight reject event")
	}
}
