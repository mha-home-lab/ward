package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mha-home-lab/ward/internal/orchestration"
	"github.com/mha-home-lab/ward/internal/store"
)

func TestTaskBrokerFlow(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)

	execCmd(taskAddCmd(), t, []string{"fix login redirect"}, map[string]string{"tier": "mid", "run": "true"})
	execCmd(taskAddCmd(), t, []string{"tiny cleanup"}, map[string]string{"tier": "cheap"})

	// A cheap-budget agent is never offered the mid item.
	nextCheap := taskNextCmd()
	if err := nextCheap.Flags().Set("by", "agent-b"); err != nil {
		t.Fatal(err)
	}
	if err := nextCheap.Flags().Set("max-tier", "cheap"); err != nil {
		t.Fatal(err)
	}
	nextCheap.SetArgs(nil)
	if err := nextCheap.Execute(); err != nil {
		t.Fatal(err)
	}

	s, _ := store.Open()
	open, _ := s.ListTasks("open", 10)
	if len(open) != 1 || open[0].TierFloor != "mid" {
		t.Fatalf("cheap agent must leave only the mid task open: %+v", open)
	}
	s.DB.Close()

	// Generate the workflow for the remaining task: file must exist and validate.
	mid := open[0]
	wfCmd := taskWorkflowCmd()
	wfCmd.SetArgs([]string{mid.ID})
	if err := wfCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "workflows", "task-"+strings.TrimPrefix(mid.ID, "task-")+".yaml")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("generated workflow missing: %v", err)
	}
	wf, err := orchestration.LoadWorkflow(path)
	if err != nil {
		t.Fatalf("generated workflow must be runnable: %v", err)
	}
	found := false
	for _, n := range wf.Nodes {
		if n.ID == "work" && n.Run == "true" {
			found = true
		}
	}
	if !found {
		t.Fatalf("work node must carry the task's run command: %+v", wf.Nodes)
	}

	// Fail bumps the floor; done requires claimed.
	failCmd := taskFailCmd()
	failCmd.SetArgs([]string{mid.ID})
	if err := failCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	s2, _ := store.Open()
	defer s2.DB.Close()
	got, _ := s2.GetTask(mid.ID)
	if got.Status != "open" || got.TierFloor != "strong" || got.Escalation != 1 {
		t.Fatalf("failed task must re-enter pool at strong: %+v", got)
	}
}

func TestTickHealSupersedesDriftedArtifacts(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)

	os.WriteFile(filepath.Join(dir, "fact.txt"), []byte("hello\n"), 0o644)
	put := memoryPutCmd()
	for n, v := range map[string]string{
		"summary": "fact about hello", "verify-cmd": "grep -q hello fact.txt",
		"verify-kind": "shell", "local": "true", "by": "human",
	} {
		if err := put.Flags().Set(n, v); err != nil {
			t.Fatal(err)
		}
	}
	put.SetArgs(nil)
	if err := put.Execute(); err != nil {
		t.Fatal(err)
	}

	s, _ := store.Open()
	acc, _ := s.ListArtifacts("accepted", "", "", 10)
	if len(acc) != 1 {
		t.Fatalf("expected exactly one accepted artifact, got %d", len(acc))
	}
	id := acc[0].ID
	if err := s.SetVerify(id, "verified"); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()

	// The fact changes underneath: heal must supersede on the same sweep.
	os.WriteFile(filepath.Join(dir, "fact.txt"), []byte("goodbye\n"), 0o644)
	tick := tickCmd()
	if err := tick.Flags().Set("heal", "true"); err != nil {
		t.Fatal(err)
	}
	tick.SetArgs(nil)
	if err := tick.Execute(); err != nil {
		t.Fatal(err)
	}

	s2, _ := store.Open()
	defer s2.DB.Close()
	a, err := s2.GetArtifact(id)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != "superseded" || a.SupersededRsn != "drift" {
		t.Fatalf("heal must supersede drifted artifact, got status=%s rsn=%s", a.Status, a.SupersededRsn)
	}
}

func TestRejectDossierAndExplain(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	// A workflow whose single work node always fails -> reject + dossier.
	wfPath := filepath.Join(t.TempDir(), "fail.yaml")
	os.WriteFile(wfPath, []byte(`name: doom
nodes:
  - id: start
    kind: channel
  - id: work
    kind: channel
    run: "false"
  - id: done
    kind: channel
edges:
  - {from: start, to: work}
  - {from: work, to: done}
`), 0o644)

	start := runStartCmd()
	if err := start.Flags().Set("workflow", wfPath); err != nil {
		t.Fatal(err)
	}
	if err := start.Flags().Set("auto-approve", "true"); err != nil {
		t.Fatal(err)
	}
	start.SetArgs(nil)
	if err := start.Execute(); err != nil {
		t.Fatal(err)
	}

	s, _ := store.Open()
	r, err := s.LatestRun()
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != "rejected" {
		t.Fatalf("run should be rejected, got %s", r.Status)
	}

	ex := explainCmd()
	ex.SetArgs([]string{r.ID, "work"})
	reject := rejectCmd()
	reject.SetArgs([]string{r.ID})

	dossiers, _ := s.SearchArtifacts("reject:"+r.ID, "", "", 5)
	found := false
	for _, d := range dossiers {
		if tagsContain(d.Tags, "reject:"+r.ID) &&
			strings.Contains(d.Content, "needs a human") &&
			len(d.Content) > 100 {
			found = true
		}
		// The dossier must NOT be tagged with the bare node id: it would count
		// as a memory hit for that node's future runs (thesis violation).
		if tagsContain(d.Tags, "work") {
			t.Fatal("dossier must not carry the bare node tag")
		}
	}
	if !found {
		t.Fatal("expected a substantive dossier for the rejected run")
	}
	s.DB.Close()

	if err := ex.Execute(); err != nil {
		t.Fatal(err)
	}
	if err := reject.Execute(); err != nil {
		t.Fatal(err)
	}
}
