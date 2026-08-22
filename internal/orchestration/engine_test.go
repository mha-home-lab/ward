package orchestration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mha-home-lab/ward/internal/store"
)

func newTestEngine(t *testing.T, repo string) (*Engine, func()) {
	t.Helper()
	home := t.TempDir()
	os.Setenv("WARD_HOME", home)
	t.Cleanup(func() { os.Unsetenv("WARD_HOME") })
	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	return &Engine{Store: s, RepoRoot: repo}, func() { s.DB.Close() }
}

func linearWF() *Workflow {
	return &Workflow{
		Name:  "test-wf",
		Nodes: []Node{{ID: "request", Kind: "channel"}, {ID: "impl", Kind: "test"}, {ID: "complete", Kind: "channel"}},
		Edges: []Edge{{From: "request", To: "impl"}, {From: "impl", To: "complete"}},
	}
}

func TestEngineVerifyOnReadPassing(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "spec.md"), []byte("OIDC login spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng, closeFn := newTestEngine(t, repo)
	defer closeFn()

	// Seed goes through the LIVE verify path.
	eng.Seed("impl", "test", "solution", "OIDC login", "OIDC::spec.md", "grep")

	runID, err := eng.StartWorkflow(linearWF())
	if err != nil {
		t.Fatal(err)
	}
	decs, _ := eng.Store.RoutingDecisionsForRun(runID)
	for _, d := range decs {
		if d.Node == "impl" {
			if d.Tier != "cheap" {
				t.Fatalf("impl should be cheap on passing live grep, got %s (verify=%s)", d.Tier, d.VerifyStatus)
			}
			if d.VerifyStatus != "verified" {
				t.Fatalf("impl should be verified, got %s", d.VerifyStatus)
			}
			return
		}
	}
	t.Fatal("no decision for impl")
}

func TestEngineVerifyOnReadFailing(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "spec.md"), []byte("OIDC login spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng, closeFn := newTestEngine(t, repo)
	defer closeFn()

	// A grep that cannot match must NOT let cheap fire.
	eng.Seed("impl", "test", "solution", "OIDC login", "ZZZNOPE::spec.md", "grep")

	runID, err := eng.StartWorkflow(linearWF())
	if err != nil {
		t.Fatal(err)
	}
	decs, _ := eng.Store.RoutingDecisionsForRun(runID)
	for _, d := range decs {
		if d.Node == "impl" {
			if d.Tier == "cheap" {
				t.Fatalf("impl must NOT be cheap when live grep fails (verify=%s)", d.VerifyStatus)
			}
			if d.VerifyStatus == "verified" {
				t.Fatalf("impl must not be verified")
			}
			return
		}
	}
	t.Fatal("no decision for impl")
}

func TestEngineContentionOnUnorderedSiblings(t *testing.T) {
	repo := t.TempDir()
	eng, closeFn := newTestEngine(t, repo)
	defer closeFn()
	wf := &Workflow{
		Name: "par",
		Nodes: []Node{
			{ID: "a", Kind: "channel"},
			{ID: "b", Kind: "test", Produces: []string{"shared.go"}},
			{ID: "c", Kind: "test", Produces: []string{"shared.go"}},
			{ID: "done", Kind: "channel"},
		},
		Edges: []Edge{
			{From: "a", To: "b"}, {From: "a", To: "c"},
			{From: "b", To: "done"}, {From: "c", To: "done"},
		},
	}
	if err := wf.Validate(); err != nil {
		t.Fatal(err)
	}
	runID, err := eng.StartWorkflow(wf)
	if err != nil {
		t.Fatal(err)
	}
	decs, _ := eng.Store.RoutingDecisionsForRun(runID)
	for _, d := range decs {
		if d.Node == "c" {
			if !d.Contention {
				t.Fatalf("node c should contend with done-sibling b (shared.go), got contention=%v", d.Contention)
			}
			if d.Tier != "strong" {
				t.Fatalf("contentious node c should escalate to strong, got %s", d.Tier)
			}
			return
		}
	}
	t.Fatal("no decision for c")
}
