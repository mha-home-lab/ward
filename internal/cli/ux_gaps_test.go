package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/mha-home-lab/ward/internal/store"
)

// TestDocAssertCatchesStaleProse: a doc claim whose pattern is absent must FAIL
// (stale prose is caught), and the claim must still be recorded so tick/brief
// can surface it. After the prose is fixed, re-assert must pass.
func TestDocAssertCatchesStaleProse(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("README.md", []byte("# secure-bank\nNo auth described here.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()

	assert := docAssertCmd()
	if _, aerr := execCapture(t, assert, "README.md", "auth header"); aerr == nil {
		t.Fatal("doc assert must fail when the pattern is absent (stale prose NOT caught)")
	}
	docs, _ := s.ListArtifacts("", "doc", "", 10)
	if len(docs) != 1 {
		t.Fatalf("doc claim must be recorded, got %d", len(docs))
	}
	if docs[0].VerifyStatus != "error" {
		t.Fatalf("recorded doc claim must be error status, got %s", docs[0].VerifyStatus)
	}

	// Fix the prose; re-assert must pass.
	if err := os.WriteFile("README.md", []byte("# secure-bank\nUses an auth header for the API.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	assert2 := docAssertCmd()
	if _, aerr := execCapture(t, assert2, "README.md", "auth header"); aerr != nil {
		t.Fatalf("doc assert must pass once the prose matches: %v", aerr)
	}
}

// TestTaskRunAutoClaims: `task run <id> --by` claims an unclaimed task instead
// of erroring, then runs it to completion.
func TestTaskRunAutoClaims(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	execCmd(taskAddCmd(), t, []string{"do thing"}, map[string]string{"tier": "mid", "run": "go version"})
	s, _ := store.Open()
	ts, _ := s.ListTasks("open", 1)
	if len(ts) != 1 {
		t.Fatalf("expected one open task, got %d", len(ts))
	}
	id := ts[0].ID
	s.DB.Close()

	run := taskRunCmd()
	out, err := execCapture(t, run, id, "--by", "agent-x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "done") {
		t.Fatalf("auto-claim run must complete: %s", out)
	}
	s2, _ := store.Open()
	defer s2.DB.Close()
	got, _ := s2.GetTask(id)
	if got.Status != "done" {
		t.Fatalf("expected task done, got %s", got.Status)
	}
}

// TestBriefSuggestsPlanWhenPoolEmpty: with no tasks, brief must point at a plan
// file as the work source rather than declaring "nothing to do".
func TestBriefSuggestsPlanWhenPoolEmpty(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.WriteFile("PLAN.md", []byte("# Plan\n- [ ] implement X\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := execCapture(t, briefCmd())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "PLAN.md") {
		t.Fatalf("brief must suggest PLAN.md when pool empty:\n%s", out)
	}
}
