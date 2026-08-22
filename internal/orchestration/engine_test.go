package orchestration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mha-home-lab/ward/internal/store"
)

var tierRank = map[string]int{"cheap": 0, "mid": 1, "strong": 2}

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

// TestEngineNodeTierFloor proves the node `tier:` field is honored end-to-end:
// a channel node (would be cheap/mid by inference) declared `strong` is recorded
// at the strong tier. This is the admission key parallel agents match against.
func TestEngineNodeTierFloor(t *testing.T) {
	repo := t.TempDir()
	eng, closeFn := newTestEngine(t, repo)
	defer closeFn()

	wf := &Workflow{
		Name:  "floor-wf",
		Nodes: []Node{{ID: "n1", Kind: "channel", Tier: "strong"}},
	}
	runID, err := eng.StartWorkflow(wf)
	if err != nil {
		t.Fatal(err)
	}
	decs, _ := eng.Store.RoutingDecisionsForRun(runID)
	for _, d := range decs {
		if d.Node == "n1" {
			if d.Tier != "strong" {
				t.Fatalf("node declared tier strong must route strong, got %s (reason %q)", d.Tier, d.Reason)
			}
			return
		}
	}
	t.Fatal("no decision for n1")
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
	order, _ := wf.TopoOrder()
	// The later of the two siblings in topo order is the one that runs while the
	// other is already done -> that is the one that must contend. The earlier
	// sibling has no done sibling yet, so it must NOT contend.
	bPos, cPos := indexOf(order, "b"), indexOf(order, "c")
	var earlier, later string
	if bPos < cPos {
		earlier, later = "b", "c"
	} else {
		earlier, later = "c", "b"
	}
	decs, _ := eng.Store.RoutingDecisionsForRun(runID)
	seen := map[string]bool{}
	for _, d := range decs {
		if d.Node == "b" || d.Node == "c" {
			seen[d.Node] = d.Contention
		}
	}
	if seen[earlier] {
		t.Fatalf("earlier sibling %s must NOT contend (no done sibling yet), got contention=%v", earlier, seen[earlier])
	}
	if !seen[later] {
		t.Fatalf("later sibling %s must contend with done-sibling (shared.go), got contention=%v", later, seen[later])
	}
}

func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

func TestEngineRunFailureEscalates(t *testing.T) {
	repo := t.TempDir()
	// A verified memory hit for "work" so the first attempt is cheap; on failure
	// it must escalate cheap -> mid -> strong -> reject.
	if err := os.WriteFile(filepath.Join(repo, "spec.txt"), []byte("OIDC login spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng, closeFn := newTestEngine(t, repo)
	defer closeFn()
	eng.Seed("work", "channel", "solution", "OIDC login", "OIDC::spec.txt", "grep")

	// A node with a `run:` command that fails.
	wf := &Workflow{
		Name:  "fail-wf",
		Nodes: []Node{{ID: "start", Kind: "channel"}, {ID: "work", Kind: "channel", Run: "false"}, {ID: "done", Kind: "channel"}},
		Edges: []Edge{{From: "start", To: "work"}, {From: "work", To: "done"}},
	}
	runID, err := eng.StartWorkflow(wf)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := eng.Store.LoadRun(runID)
	if r.Status != "rejected" {
		t.Fatalf("run must be rejected after run: fails, got %s", r.Status)
	}
	decs, _ := eng.Store.RoutingDecisionsForRun(runID)
	// Three routing decisions (cheap, mid, strong) for "work", escalating.
	var workDecs []store.RoutingDecision
	for _, d := range decs {
		if d.Node == "work" {
			workDecs = append(workDecs, d)
		}
	}
	if len(workDecs) < 3 {
		t.Fatalf("expected 3 escalating attempts for work, got %d", len(workDecs))
	}
	tiers := []string{workDecs[0].Tier, workDecs[1].Tier, workDecs[2].Tier}
	for i := 1; i < len(tiers); i++ {
		if tierRank[tiers[i]] <= tierRank[tiers[i-1]] {
			t.Fatalf("tiers must strictly escalate, got %v", tiers)
		}
	}
	// The (re-)attempt context must be the verified fact only — never the
	// failed first attempt's prose.
	seeded, _ := eng.Store.SearchArtifacts("work", "", "", 5)
	seedID := ""
	for _, a := range seeded {
		if a.Status == "accepted" && a.CreatedBy == "seed" {
			seedID = a.ID
		}
	}
	if seedID == "" {
		t.Fatal("seeded verified artifact not found")
	}
	if !strings.Contains(workDecs[0].Context, seedID) {
		t.Fatalf("first attempt context must carry the verified fact %s, got %s", seedID, workDecs[0].Context)
	}
	// Every (re-)attempt resumes from the SAME verified facts; failed-attempt
	// exec output must never appear in Context.
	for i, d := range workDecs {
		if !strings.Contains(d.Context, seedID) {
			t.Fatalf("attempt %d context must carry the verified fact %s, got %s", i, seedID, d.Context)
		}
		if strings.Contains(d.Context, "exec") || strings.Contains(d.Context, "false") {
			t.Fatalf("attempt %d context leaked exec output: %s", i, d.Context)
		}
	}
	nodes, _ := eng.Store.LoadRunNodes(runID)
	for _, n := range nodes {
		if n.Node == "work" && n.Status == "done" {
			t.Fatalf("failed node must NOT be marked done")
		}
	}
}
