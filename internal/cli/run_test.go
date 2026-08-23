package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mha-home-lab/ward/internal/orchestration"
	"github.com/mha-home-lab/ward/internal/store"
)

// Regression for the resume/approve corruption bug: without --workflow, a
// second session must reload the run's persisted workflow_path, NOT silently
// fall back to oidc-login.yaml (which would overwrite the run's nodes).
func TestResolveRunWFUsesPersistedPath(t *testing.T) {
	home := t.TempDir()
	os.Setenv("WARD_HOME", home)
	t.Cleanup(func() { os.Unsetenv("WARD_HOME") })

	wfPath := filepath.Join(home, "wf.yaml")
	yaml := "name: fail-demo\nnodes:\n  - id: start\n    kind: channel\n" +
		"  - id: work\n    kind: test\n    run: \"false\"\n  - id: done\n    kind: channel\n" +
		"edges:\n  - {from: start, to: work}\n  - {from: work, to: done}\n"
	if err := os.WriteFile(wfPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()

	wf, err := orchestration.LoadWorkflow(wfPath)
	if err != nil {
		t.Fatal(err)
	}
	eng := &orchestration.Engine{Store: s, AutoApprove: true}
	runID, err := eng.StartWorkflow(wf)
	if err != nil {
		t.Fatal(err)
	}

	got, err := resolveRunWF(s, "", runID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "fail-demo" {
		t.Fatalf("resume must use the persisted workflow path, got %q (would have corrupted the run)", got.Name)
	}
}

// External-review regression (openai.md #3): a workflow file mutated between
// run start and resume must NOT be silently executed as if it were the
// original definition. The persisted definition hash must make resume refuse;
// --allow-drift is the explicit override.
func TestResumeRefusesWorkflowDrift(t *testing.T) {
	home := t.TempDir()
	os.Setenv("WARD_HOME", home)
	t.Cleanup(func() { os.Unsetenv("WARD_HOME") })
	dir := t.TempDir()
	t.Chdir(dir)

	wfPath := filepath.Join(dir, "wf.yaml")
	write := func(run string) {
		y := "name: drift-demo\nnodes:\n  - id: start\n    kind: channel\n" +
			"  - id: work\n    kind: test\n    run: \"" + run + "\"\n  - id: done\n    kind: channel\n" +
			"edges:\n  - {from: start, to: work}\n  - {from: work, to: done}\n"
		if err := os.WriteFile(wfPath, []byte(y), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A paused run: approval node waits, so the file can be mutated mid-flight.
	pausedYAML := "name: drift-demo\nnodes:\n  - id: start\n    kind: channel\n" +
		"  - id: gate\n    kind: approval\n    channels: [notify]\n" +
		"  - id: work\n    kind: test\n    run: \"true\"\n" +
		"  - id: done\n    kind: channel\n" +
		"edges:\n  - {from: start, to: gate}\n  - {from: gate, to: work}\n  - {from: work, to: done}\n"
	if err := os.WriteFile(wfPath, []byte(pausedYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	wf, err := orchestration.LoadWorkflow(wfPath)
	if err != nil {
		t.Fatal(err)
	}
	eng := &orchestration.Engine{Store: s}
	runID, err := eng.StartWorkflow(wf) // no AutoApprove -> pauses at gate
	if err != nil {
		t.Fatal(err)
	}

	// Mutate the definition after start.
	write("false")

	driftEng := &orchestration.Engine{Store: s, AutoApprove: true}
	wf2, err := orchestration.LoadWorkflow(wfPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := driftEng.Run(runID, wf2); err == nil || !strings.Contains(err.Error(), "hash mismatch") {
		t.Fatalf("resume under a changed definition must be refused: %v", err)
	}

	// Explicit override proceeds; the mutated check then fails honestly and
	// the run terminates rejected (a rejection is a durable state, not a
	// Go-level error).
	okEng := &orchestration.Engine{Store: s, AutoApprove: true, AllowWorkflowDrift: true}
	if err := okEng.Run(runID, wf2); err != nil {
		t.Fatalf("drift override should proceed: %v", err)
	}
	r, _ := s.LoadRun(runID)
	if r.Status != "rejected" {
		t.Fatalf("overridden run must fail honestly on its mutated check, got %s", r.Status)
	}
}
