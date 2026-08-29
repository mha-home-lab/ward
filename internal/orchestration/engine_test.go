package orchestration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mha-home-lab/ward/internal/adapter"
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
	// Identical-failure short-circuit: a deterministic run: command that fails
	// the same way twice is rejected after TWO attempts (cheap->mid), not
	// three — climbing tiers cannot change byte-identical work (rd:c1
	// f0b662e1). Tiers must still strictly escalate across the two attempts.
	var workDecs []store.RoutingDecision
	for _, d := range decs {
		if d.Node == "work" {
			workDecs = append(workDecs, d)
		}
	}
	if len(workDecs) != 2 {
		t.Fatalf("identical-failure short-circuit expects exactly 2 attempts, got %d", len(workDecs))
	}
	if tierRank[workDecs[1].Tier] <= tierRank[workDecs[0].Tier] {
		t.Fatalf("attempt 2 must outrank attempt 1, got %v", workDecs)
	}
	events, _ := eng.Store.LoadEvents(runID)
	sawShortCircuit := false
	for _, e := range events {
		if e.Action == "reject" && strings.Contains(e.Detail, "identical failure repeated") {
			sawShortCircuit = true
		}
	}
	if !sawShortCircuit {
		t.Fatal("expected an identical-failure short-circuit reject event")
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

// TestBuildEvidenceBlockSkipsNonLocal proves the trust boundary: only store-local
// verified artifacts have their content injected as worker evidence. Imported
// (non-local) artifacts are routing signals only and must never reach the worker.
func TestBuildEvidenceBlockSkipsNonLocal(t *testing.T) {
	got := buildEvidenceBlock([]store.Artifact{
		{ID: "local1", Summary: "local solution", Content: "do X then Y", Local: true},
		{ID: "imp1", Summary: "imported solution", Content: "secret injected", Local: false},
	})
	if !strings.Contains(got, "local1") || !strings.Contains(got, "do X then Y") {
		t.Fatalf("local artifact must be injected, got %q", got)
	}
	if strings.Contains(got, "imp1") || strings.Contains(got, "secret injected") {
		t.Fatalf("non-local artifact must NOT be injected, got %q", got)
	}
	if !strings.Contains(got, "VERIFIED PRIOR CONTEXT") {
		t.Fatalf("block must be labeled, got %q", got)
	}
	if buildEvidenceBlock(nil) != "" {
		t.Fatal("empty input must yield empty block")
	}
}

// TestEngineEvidenceInjectedOnMemoryHit proves the routing≠knowing gap is closed
// end-to-end: a prompt node with a live-verified memory hit receives the verified
// artifact as appended evidence in the prompt handed to the adapter (the worker),
// not just a routing signal. The adapter binary is swapped for a probe that records
// its final argument (the prompt).
func TestEngineEvidenceInjectedOnMemoryHit(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "spec.md"), []byte("OIDC login spec\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	eng, closeFn := newTestEngine(t, repo)
	defer closeFn()

	// A verified local prior solution for node "impl".
	eng.Seed("impl", "test", "solution", "OIDC login flow", "OIDC::spec.md", "grep")

	// Probe adapter: record the final arg (the prompt) to a file.
	capture := filepath.Join(t.TempDir(), "prompt.txt")
	probe := filepath.Join(t.TempDir(), "probe.sh")
	script := "#!/bin/sh\nlast=\"\"; for a in \"$@\"; do last=\"$a\"; done; printf '%s' \"$last\" > " + capture + "\n"
	if err := os.WriteFile(probe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	oldBin := adapter.Binary
	adapter.Binary = probe
	defer func() { adapter.Binary = oldBin }()

	wf := &Workflow{
		Name:  "ev-wf",
		Nodes: []Node{{ID: "request", Kind: "channel"}, {ID: "impl", Kind: "test", Prompt: "Implement the login."}, {ID: "complete", Kind: "channel"}},
		Edges: []Edge{{From: "request", To: "impl"}, {From: "impl", To: "complete"}},
	}
	if _, err := eng.StartWorkflow(wf); err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("adapter was not invoked with a prompt: %v", err)
	}
	if !strings.Contains(string(prompt), "Implement the login.") {
		t.Fatalf("original prompt must survive, got %q", prompt)
	}
	if !strings.Contains(string(prompt), "VERIFIED PRIOR CONTEXT") || !strings.Contains(string(prompt), "OIDC login flow") {
		t.Fatalf("verified evidence must be injected into the worker prompt, got %q", prompt)
	}
}
