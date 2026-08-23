package orchestration

import (
	"strings"
	"testing"
)

func TestTaskWorkflowGateAlwaysTravels(t *testing.T) {
	cases := []struct {
		name       string
		run        string
		verify     string
		wantSubstr string
	}{
		{"both: work AND-gated by verify", "go build ./...", "go test ./...", "go build ./... ) && ( go test ./..."},
		{"verify-only gates interactive work", "", "test -s .spec/x.md", "test -s .spec/x.md"},
	}
	for _, tc := range cases {
		wf := TaskWorkflow("task-abc", "t", "default", tc.run, tc.verify, nil)
		var work Node
		for _, n := range wf.Nodes {
			if n.ID == "work-abc" {
				work = n
			}
		}
		if !strings.Contains(work.Run, tc.wantSubstr) {
			t.Fatalf("%s: want %q in %q", tc.name, tc.wantSubstr, work.Run)
		}
	}
}
