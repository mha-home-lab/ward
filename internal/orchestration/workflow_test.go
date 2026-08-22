package orchestration

import "testing"

func TestValidate(t *testing.T) {
	valid := &Workflow{
		Name:  "ok",
		Nodes: []Node{{ID: "a", Kind: "channel"}, {ID: "b", Kind: "test"}},
		Edges: []Edge{{From: "a", To: "b"}},
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid workflow rejected: %v", err)
	}

	cycle := &Workflow{
		Name:  "cyc",
		Nodes: []Node{{ID: "a", Kind: "channel"}, {ID: "b", Kind: "test"}},
		Edges: []Edge{{From: "a", To: "b"}, {From: "b", To: "a"}},
	}
	if err := cycle.Validate(); err == nil {
		t.Fatal("cycle not detected")
	}

	twoRoots := &Workflow{
		Name:  "two",
		Nodes: []Node{{ID: "a", Kind: "channel"}, {ID: "b", Kind: "channel"}},
		Edges: []Edge{},
	}
	if err := twoRoots.Validate(); err == nil {
		t.Fatal("two roots not detected")
	}

	approvalNoChannel := &Workflow{
		Name:  "ap",
		Nodes: []Node{{ID: "a", Kind: "channel"}, {ID: "b", Kind: "approval"}},
		Edges: []Edge{{From: "a", To: "b"}},
	}
	if err := approvalNoChannel.Validate(); err == nil {
		t.Fatal("approval without channel not detected")
	}

	approvalWithChannel := &Workflow{
		Name:  "ap2",
		Nodes: []Node{{ID: "a", Kind: "channel"}, {ID: "b", Kind: "approval", Channels: []string{"review"}}},
		Edges: []Edge{{From: "a", To: "b"}},
	}
	if err := approvalWithChannel.Validate(); err != nil {
		t.Fatalf("approval with channel rejected: %v", err)
	}
}
