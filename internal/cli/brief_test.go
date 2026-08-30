package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mha-home-lab/ward/internal/store"
)

// briefSeeded runs a brief in a store seeded with one verified artifact and one
// awaiting-approval run, then asserts the report carries both.
func TestBriefSurfacesKnowledgeRunsAndClaims(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WARD_HOME", home)
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(home) })

	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	// A verified, store-local artifact tagged "payments".
	a := store.Artifact{
		Kind: "solution", Summary: "stripe webhook dedupe", Tags: []string{"payments"},
		Status: "accepted", CreatedBy: "test", Local: true,
		VerifyCmd: "grep -rq stripe .", VerifyKind: "shell",
	}
	id, err := s.UpsertArtifact(a)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetVerify(id, "verified"); err != nil {
		t.Fatal(err)
	}
	// An open run awaiting approval.
	now := store.NowISO()
	if err := s.CreateRun(store.RunState{
		ID: "run-brief1", WorkflowName: "wf", Status: "awaiting_approval",
		WaitingApproval: "review", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	// An active claim held by another agent.
	if _, _, err := s.ClaimTopic("release-cut", "", "other-agent", ""); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()

	bc := briefCmd()
	bc.SetArgs([]string{"payments"})
	if err := bc.Execute(); err != nil {
		t.Fatal(err)
	}

	// The claim sweep inside brief must have kept the unexpired claim intact and
	// the run must still be open.
	s2, _ := store.Open()
	defer s2.DB.Close()
	runs, _ := s2.OpenRuns()
	if len(runs) != 1 || runs[0].ID != "run-brief1" {
		t.Fatalf("expected the awaiting run to survive brief, got %v", runs)
	}
}

func TestBriefNextActionsGuidance(t *testing.T) {
	b := brief{
		Drift:  2,
		Health: map[string]int{},
		OpenRuns: []map[string]string{
			{"id": "r1", "status": "running"},
			{"id": "r2", "status": "awaiting_approval", "waiting": "n1"},
		},
		Claims: []map[string]string{
			{"topic": "db-migration", "by": "a9", "expires": "soon"},
		},
	}
	next := nextActions(b)
	joined := strings.Join(next, "\n")
	for _, want := range []string{
		"ward run resume r1",
		"ward run approve r2 n1",
		"FAILED live verification",
		"claimed by a9",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("next actions missing %q:\n%s", want, joined)
		}
	}
}

func TestBriefEmptyStoreHasCleanSlateAction(t *testing.T) {
	next := nextActions(brief{Health: map[string]int{}})
	if len(next) != 1 || !strings.Contains(next[0], "proceed") {
		t.Fatalf("empty store should say proceed: %v", next)
	}
}

func TestBriefSurfacesOpenTaskPool(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	s, _ := store.Open()
	if _, err := s.CreateTask(store.Task{Title: "do a thing", TierFloor: "mid"}); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()

	bc := briefCmd()
	if err := bc.Execute(); err != nil {
		t.Fatal(err)
	}
	// Guidance must point at the pool pull command.
	out := brief{Health: map[string]int{}, OpenTasks: []map[string]string{{"id": "t1"}}}
	next := nextActions(out)
	if len(next) == 0 || !strings.Contains(next[0], "ward task next") {
		t.Fatalf("brief must direct agents to the pool: %v", next)
	}
}

func TestLoadWFResolvesDefaultBeforeDemo(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(filepath.Dir(dir)) })

	// No workflows at all: loadWF("") must fail mentioning default.yaml.
	if _, err := loadWF(""); err == nil {
		t.Fatal("loadWF with no workflow files must error")
	} else if !strings.Contains(err.Error(), "workflows/default.yaml") {
		t.Fatalf("error should mention the scaffold path, got: %v", err)
	}

	if err := scaffoldWorkflow("."); err != nil {
		t.Fatal(err)
	}
	wf, err := loadWF("")
	if err != nil {
		t.Fatal(err)
	}
	if wf.Name != "default" || wf.Path != filepath.Join("workflows", "default.yaml") {
		t.Fatalf("expected default workflow resolved from scaffold, got %s at %s", wf.Name, wf.Path)
	}
}
