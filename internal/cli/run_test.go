package cli

import (
	"os"
	"path/filepath"
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
