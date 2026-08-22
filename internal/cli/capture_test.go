package cli

import (
	"os"
	"testing"

	"github.com/mha-home-lab/ward/internal/orchestration"
	"github.com/mha-home-lab/ward/internal/store"
	"github.com/mha-home-lab/ward/internal/verification"
)

func TestCaptureNodeDefaultsTagAndInfersVerify(t *testing.T) {
	home := t.TempDir()
	os.Setenv("WARD_HOME", home)
	t.Cleanup(func() { os.Unsetenv("WARD_HOME") })

	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()

	// test node -> tag=node id, verify inferred as `go test ./...`
	wf := &orchestration.Workflow{Name: "t", Nodes: []orchestration.Node{
		{ID: "impl", Kind: "test", Run: "go test ./..."},
	}}
	id, err := captureNode(s, wf, wf.Nodes[0], "", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.GetArtifact(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Tags) != 1 || a.Tags[0] != "impl" {
		t.Fatalf("default tag must be the node id, got %v", a.Tags)
	}
	if a.VerifyCmd != "go test ./..." || a.VerifyKind != "test" {
		t.Fatalf("test node must infer go test verify, got %q/%q", a.VerifyCmd, a.VerifyKind)
	}
	if a.Status != "accepted" || a.Ceremony != "light" {
		t.Fatalf("capture must be accepted/light, got %s/%s", a.Status, a.Ceremony)
	}
	// The captured claim is findable by the node id on a later lookup (the
	// routing hit path), and tags must not require anything beyond the node id.
	res, err := s.SearchArtifacts("impl", "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) == 0 {
		t.Fatal("captured artifact must be findable by node id for a routing hit")
	}

	// override beats inference (distinct content so it is a distinct artifact)
	id2, err := captureNode(s, wf, wf.Nodes[0], "custom", "grep -rq x README.md", "grep", "different content", "")
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := s.GetArtifact(id2)
	if a2.Tags[0] != "custom" || a2.VerifyCmd != "grep -rq x README.md" {
		t.Fatalf("override not honored: %+v", a2)
	}
}

func TestCaptureNodeHashInference(t *testing.T) {
	home := t.TempDir()
	os.Setenv("WARD_HOME", home)
	t.Cleanup(func() { os.Unsetenv("WARD_HOME") })

	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()

	// write a concrete file the node declares as produced
	path := home + "/out.txt"
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// non-test node with a concrete produces -> hash inference
	wf := &orchestration.Workflow{Name: "t", Nodes: []orchestration.Node{
		{ID: "build", Kind: "context", Produces: []string{path}},
	}}
	id, err := captureNode(s, wf, wf.Nodes[0], "", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.GetArtifact(id)
	if a.VerifyKind != "hash" || a.VerifyCmd != "sha256::"+path {
		t.Fatalf("concrete produces must infer hash verify, got %q/%q", a.VerifyCmd, a.VerifyKind)
	}
	// content first line is the baseline hash, so a live re-verify passes
	res := verification.Run(a, "")
	if res.Status != "verified" {
		t.Fatalf("hash claim should verify, got %s", res.Status)
	}
}
