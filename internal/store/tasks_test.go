package store

import (
	"testing"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	t.Setenv("WARD_HOME", t.TempDir())
	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestTaskLifecycle(t *testing.T) {
	s := testStore(t)
	defer s.DB.Close()

	id, err := s.CreateTask(Task{Title: "fix login redirect", Kind: "default", TierFloor: "mid", Run: "go test ./..."})
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.GetTask(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "open" || got.TierRank != 1 || got.Run != "go test ./..." {
		t.Fatalf("unexpected task state: %+v", got)
	}

	// Claim pulls the open task.
	c1, ok, err := s.ClaimNextTask("agent-a", "mid")
	if err != nil || !ok || c1.ID != id {
		t.Fatalf("claim failed: ok=%v err=%v", ok, err)
	}
	if c1.ClaimedBy != "agent-a" || c1.Status != "claimed" {
		t.Fatalf("claim not recorded: %+v", c1)
	}

	// A claimed task cannot be claimed again by another agent.
	if _, ok, _ := s.ClaimNextTask("agent-b", "strong"); ok {
		t.Fatal("claimed task must not be re-claimable")
	}

	// Done requires the claimed state.
	if err := s.CompleteTask(id, "agent-a"); err != nil {
		t.Fatalf("complete claimed task: %v", err)
	}
	if err := s.CompleteTask(id, "agent-b"); err == nil {
		t.Fatal("completing a non-claimed task must error")
	}
}

func TestClaimNextTaskAdmissionByBudget(t *testing.T) {
	s := testStore(t)
	defer s.DB.Close()

	midID, _ := s.CreateTask(Task{Title: "mid work", TierFloor: "mid"})
	cheapID, _ := s.CreateTask(Task{Title: "cheap work", TierFloor: "cheap"})

	// A cheap-budget agent never sees the mid item, even though it sorts first
	// (highest floor wins among admissible tasks).
	c, ok, err := s.ClaimNextTask("cheap-agent", "cheap")
	if err != nil || !ok {
		t.Fatalf("cheap claim failed: %v %v", ok, err)
	}
	if c.ID != cheapID {
		t.Fatalf("cheap agent must get the cheap task, got %+v", c)
	}

	// A mid agent takes the highest admissible floor first.
	c2, ok, _ := s.ClaimNextTask("mid-agent", "mid")
	if !ok || c2.ID != midID {
		t.Fatalf("mid agent must get the mid task, got %+v ok=%v", c2, ok)
	}

	if _, _, err := s.ClaimNextTask("x", "bogus"); err == nil {
		t.Fatal("invalid max-tier must be refused")
	}
}

func TestFailTaskBumpsFloorThenRejects(t *testing.T) {
	s := testStore(t)
	defer s.DB.Close()

	id, _ := s.CreateTask(Task{Title: "hard thing", TierFloor: "cheap"})
	if _, _, err := s.ClaimNextTask("a", "strong"); err != nil {
		t.Fatal(err)
	}

	// cheap -> mid -> strong -> rejected (never loops back to open).
	want := []struct {
		floor  string
		status string
	}{
		{"mid", "open"}, {"strong", "open"}, {"strong", "rejected"},
	}
	for i, w := range want {
		tk, err := s.FailTask(id)
		if err != nil {
			t.Fatal(err)
		}
		if tk.TierFloor != w.floor || tk.Status != w.status {
			t.Fatalf("fail %d: want %s/%s got %s/%s", i, w.floor, w.status, tk.TierFloor, tk.Status)
		}
		if tk.Escalation != i+1 {
			t.Fatalf("escalation must increment, got %d", tk.Escalation)
		}
		// Re-claim between failures (the next agent picks it up).
		if tk.Status == "open" {
			if _, ok, _ := s.ClaimNextTask("next-agent", "strong"); !ok {
				t.Fatalf("bumped task must be re-claimable at step %d", i)
			}
		}
	}
}

func TestLoadEventsOrdered(t *testing.T) {
	s := testStore(t)
	defer s.DB.Close()
	now := NowISO()
	if err := s.CreateRun(RunState{ID: "run-ev", WorkflowName: "wf", Status: "running", CreatedAt: now, UpdatedAt: now}); err != nil {
		t.Fatal(err)
	}
	_ = s.AddEvent("run-ev", "exec", "n1", "first")
	_ = s.AddEvent("run-ev", "done", "n1", "second")
	evs, err := s.LoadEvents("run-ev")
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) != 2 || evs[0].Action != "exec" || evs[1].Action != "done" {
		t.Fatalf("events must load in insertion order: %+v", evs)
	}
}
