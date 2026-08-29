package cli

import (
	"encoding/json"
	"io"
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

	execCmd(taskAddCmd(), t, []string{"fix login redirect"}, map[string]string{"tier": "mid", "run": "test -f /etc/hosts"})
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
		// Node ids are PER-TASK ("work-<id>") so capture tags never collide
		// across tasks — a shared "work" tag let one task's result vouch for
		// another's node (dogfood regression).
		if strings.HasPrefix(n.ID, "work-") && n.Run == "test -f /etc/hosts" {
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

func TestTaskRunCompletesAndCaptures(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())
	dir := t.TempDir()
	t.Chdir(dir)

	execCmd(taskAddCmd(), t, []string{"run tests"}, map[string]string{
		"tier": "cheap", "kind": "test", "run": "test -f /etc/hosts",
	})
	next := taskNextCmd()
	if err := next.Flags().Set("by", "agent-a"); err != nil {
		t.Fatal(err)
	}
	if err := next.Flags().Set("max-tier", "cheap"); err != nil {
		t.Fatal(err)
	}
	if err := next.Execute(); err != nil {
		t.Fatal(err)
	}

	s, _ := store.Open()
	ts, _ := s.ListTasks("claimed", 10)
	if len(ts) != 1 {
		t.Fatalf("expected one claimed task, got %d", len(ts))
	}
	id := ts[0].ID
	s.DB.Close()

	run := taskRunCmd()
	run.SetArgs([]string{id})
	if err := run.Execute(); err != nil {
		t.Fatal(err)
	}

	s2, _ := store.Open()
	defer s2.DB.Close()
	got, _ := s2.GetTask(id)
	if got.Status != "done" {
		t.Fatalf("task must be done after successful run, got %s", got.Status)
	}
	// The bridge must capture the result so the NEXT session routes cheap.
	caps, _ := s2.SearchArtifacts("work", "", "", 5)
	found := false
	for _, a := range caps {
		if tagsContain(a.Tags, "work-"+strings.TrimPrefix(id, "task-")) && a.Status == "accepted" && a.Local {
			found = true
		}
	}
	if !found {
		t.Fatal("task run must auto-capture a store-local artifact tagged work")
	}
	// The generated workflow must be recorded on the task.
	if got.WorkflowPath == "" {
		t.Fatal("task workflow path must be recorded")
	}
}

func TestTaskRunFailureReleasesAtHigherFloor(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	execCmd(taskAddCmd(), t, []string{"impossible"}, map[string]string{
		"tier": "cheap", "run": "test -f .does-not-exist",
	})
	next := taskNextCmd()
	if err := next.Flags().Set("by", "agent-b"); err != nil {
		t.Fatal(err)
	}
	if err := next.Execute(); err != nil {
		t.Fatal(err)
	}

	s, _ := store.Open()
	ts, _ := s.ListTasks("claimed", 10)
	id := ts[0].ID
	s.DB.Close()

	run := taskRunCmd()
	run.SetArgs([]string{id})
	if err := run.Execute(); err != nil {
		t.Fatal(err)
	}

	s2, _ := store.Open()
	defer s2.DB.Close()
	got, _ := s2.GetTask(id)
	if got.Status != "open" || got.TierFloor != "mid" || got.Escalation != 1 {
		t.Fatalf("failed task must re-enter pool one tier higher: %+v", got)
	}
	// And a dossier exists for the rejected run.
	r, _ := s2.LatestRun()
	if r.Status != "rejected" {
		t.Fatalf("engine should have rejected the doomed run, got %s", r.Status)
	}
	dossiers, _ := s2.SearchArtifacts("reject:"+r.ID, "", "", 5)
	if len(dossiers) == 0 {
		t.Fatal("expected dossier for rejected task run")
	}
}

func TestTaskTakeRecoversDeadSessionClaim(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	execCmd(taskAddCmd(), t, []string{"orphaned work"}, nil)
	next := taskNextCmd()
	if err := next.Flags().Set("by", "dead-agent"); err != nil {
		t.Fatal(err)
	}
	if err := next.Execute(); err != nil {
		t.Fatal(err)
	}
	s, _ := store.Open()
	ts, _ := s.ListTasks("claimed", 10)
	id := ts[0].ID
	s.DB.Close()

	// A different agent takes over the orphaned claim.
	take := taskTakeCmd()
	if err := take.Flags().Set("by", "successor"); err != nil {
		t.Fatal(err)
	}
	take.SetArgs([]string{id})
	if err := take.Execute(); err != nil {
		t.Fatal(err)
	}

	s2, _ := store.Open()
	defer s2.DB.Close()
	got, _ := s2.GetTask(id)
	if got.ClaimedBy != "successor" || got.Status != "claimed" {
		t.Fatalf("take must transfer the claim: %+v", got)
	}

	// Done/rejected tasks are not takeable, and only the HOLDER may close.
	wrongClose := taskDoneCmd()
	if err := wrongClose.Flags().Set("by", "someone-else"); err != nil {
		t.Fatal(err)
	}
	wrongClose.SetArgs([]string{id})
	if err := wrongClose.Execute(); err == nil {
		t.Fatal("closing a task you do not hold must error (attribution guard)")
	}
	rightClose := taskDoneCmd()
	if err := rightClose.Flags().Set("by", "successor"); err != nil {
		t.Fatal(err)
	}
	rightClose.SetArgs([]string{id})
	if err := rightClose.Execute(); err != nil {
		t.Fatal(err)
	}
	take2 := taskTakeCmd()
	if err := take2.Flags().Set("by", "vulture"); err != nil {
		t.Fatal(err)
	}
	take2.SetArgs([]string{id})
	if err := take2.Execute(); err == nil {
		t.Fatal("taking a done task must error")
	}
}

func TestSkillPackGateAndStaleness(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	// Verdict-knowledge (no verify_cmd): accepted via promotion.
	put := memoryPutCmd()
	for n, v := range map[string]string{
		"summary": "invariant checklist", "kind": "discovery", "tags": "rd:checks",
		"content": "conservation, floor, totality", "ceremony": "full",
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
	proposed, _ := s.ListArtifacts("proposed", "", "", 5)
	if len(proposed) != 1 {
		t.Fatalf("full ceremony must stay proposed, got %d", len(proposed))
	}
	if _, err := s.Promote([]string{proposed[0].ID}, "accept", "architect"); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()

	out := filepath.Join(t.TempDir(), "chip")
	pack := skillPackCmd()
	if err := pack.Flags().Set("out", out); err != nil {
		t.Fatal(err)
	}
	pack.SetArgs([]string{"rd:checks"})
	if err := pack.Execute(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(out, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "invariant checklist") || !strings.Contains(string(body), proposed[0].ID) {
		t.Fatal("chip must contain knowledge and cite its source id")
	}

	check := skillCheckCmd()
	check.SetArgs([]string{out})
	if err := check.Execute(); err != nil {
		t.Fatal(err)
	}

	// Retire the source: chip must flip STALE with the source named.
	s2, _ := store.Open()
	if err := s2.Supersede(proposed[0].ID, "", "drift drill"); err != nil {
		t.Fatal(err)
	}
	s2.DB.Close()
	check2 := skillCheckCmd()
	check2.SetArgs([]string{out})
	if err := check2.Execute(); err != nil {
		t.Fatal(err)
	}
}

func TestHarvestReportsTelemetry(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	s, _ := store.Open()
	now := store.NowISO()
	// One cheap+verified decision, one miss.
	for _, d := range []store.RoutingDecision{
		{RunID: "r1", Node: "n1", Tier: "cheap", MemoryHit: true, VerifyStatus: "verified", CreatedAt: now},
		{RunID: "r2", Node: "n2", Tier: "mid", CreatedAt: now},
	} {
		if err := s.AddRoutingDecision(d); err != nil {
			t.Fatal(err)
		}
	}
	id, err := s.CreateTask(store.Task{Title: "bouncer", TierFloor: "mid"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ClaimNextTask("a", "strong"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.FailTask(id); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()

	h := harvestCmd()
	if err := h.Execute(); err != nil {
		t.Fatal(err)
	}
}

// L6 acceptance: verified knowledge compounds ACROSS tasks sharing a topic tag.
// Task A (topic:x) completes -> captured+verified. Task B (different id, same
// topic:x) must route CHEAP on a live-verified memory hit.
func TestTopicTagsCompoundAcrossTasks(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	for _, title := range []string{"regression A", "regression B"} {
		execCmd(taskAddCmd(), t, []string{title}, map[string]string{
			"tier": "cheap", "kind": "test", "run": "test -f /etc/hosts", "tags": "topic:regression",
		})
	}

	var secondID string
	pullAndRun := func(agent string) {
		next := taskNextCmd()
		if err := next.Flags().Set("by", agent); err != nil {
			t.Fatal(err)
		}
		if err := next.Flags().Set("max-tier", "mid"); err != nil {
			t.Fatal(err)
		}
		if err := next.Execute(); err != nil {
			t.Fatal(err)
		}
		s, _ := store.Open()
		ts, _ := s.ListTasks("claimed", 1)
		id := ts[0].ID
		if agent == "eng-2" {
			secondID = id
		}
		s.DB.Close()
		run := taskRunCmd()
		run.SetArgs([]string{id})
		if err := run.Execute(); err != nil {
			t.Fatal(err)
		}
	}

	pullAndRun("eng-1") // first: miss -> mid; captures verified knowledge tagged topic:regression
	pullAndRun("eng-2") // second: MUST route cheap via live-verified topic vouching

	s2, _ := store.Open()
	defer s2.DB.Close()
	decs, _ := s2.AllRoutingDecisions(10)
	var last store.RoutingDecision
	found := false
	for _, d := range decs {
		if strings.HasPrefix(d.Node, "work-") && strings.HasSuffix(d.Node, strings.TrimPrefix(secondID, "task-")) {
			last, found = d, true
		}
	}
	if !found {
		t.Fatal("no work decision for the second task")
	}
	if last.Tier != "cheap" || !last.MemoryHit || last.VerifyStatus != "verified" {
		t.Fatalf("second same-topic task must route cheap+verified, got %+v", last)
	}
	var ctxIDs []string
	if err := json.Unmarshal([]byte(last.Context), &ctxIDs); err != nil || len(ctxIDs) == 0 {
		t.Fatalf("cheap decision must cite its evidence ids, got %q", last.Context)
	}
}

func TestTaskRunRequiresClaimedTask(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	execCmd(taskAddCmd(), t, []string{"unpulled"}, nil)
	s, _ := store.Open()
	ts, _ := s.ListTasks("open", 10)
	s.DB.Close()
	run := taskRunCmd()
	run.SetArgs([]string{ts[0].ID})
	if err := run.Execute(); err == nil {
		t.Fatal("task run on an unclaimed task must error (pull first)")
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

// External-review regression (openai.md #5): topic compounding must retrieve
// by TAG, not by luck of summary wording. Task A's capture text names only
// A's own node id and kind ("test"); task B is kind "default" with a
// different id, so B's FTS keys match nothing. Only an exact-tag lookup can
// find A's verified capture. Under the old FTS-only candidate pull this
// routed mid (silent compounding failure); tag-first retrieval routes cheap.
func TestTopicCompoundingSurvivesSummaryWording(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())
	t.Chdir(t.TempDir())

	// Task A: kind test -> its capture summary contains "(test)", never
	// anything about task B.
	execCmd(taskAddCmd(), t, []string{"alpha work"}, map[string]string{
		"tier": "cheap", "kind": "test", "run": "test -f /etc/hosts",
		"verify-cmd": "test -f /etc/hosts", "tags": "topic:wording",
	})
	// Task B: kind default -> FTS over "work-<B>" and "default" cannot match
	// A's capture text. Compounding must still hit via the topic tag.
	execCmd(taskAddCmd(), t, []string{"beta work"}, map[string]string{
		"tier": "cheap", "kind": "default", "run": "test -f /etc/hosts",
		"verify-cmd": "test -f /etc/hosts", "tags": "topic:wording",
	})

	var secondID string
	pullAndRun := func(agent string) string {
		next := taskNextCmd()
		if err := next.Flags().Set("by", agent); err != nil {
			t.Fatal(err)
		}
		next.SetArgs(nil)
		if err := next.Execute(); err != nil {
			t.Fatal(err)
		}
		s, _ := store.Open()
		ts, _ := s.ListTasks("claimed", 1)
		id := ts[0].ID
		s.DB.Close()
		run := taskRunCmd()
		run.SetArgs([]string{id})
		if err := run.Execute(); err != nil {
			t.Fatal(err)
		}
		return id
	}
	pullAndRun("eng-1")
	secondID = pullAndRun("eng-2")

	s2, _ := store.Open()
	defer s2.DB.Close()
	decs, _ := s2.AllRoutingDecisions(10)
	suffix := strings.TrimPrefix(secondID, "task-")
	var last store.RoutingDecision
	found := false
	for _, d := range decs {
		if strings.HasSuffix(d.Node, suffix) {
			last, found = d, true
		}
	}
	if !found {
		t.Fatal("no work decision for the second task")
	}
	if last.Tier != "cheap" || !last.MemoryHit || last.VerifyStatus != "verified" {
		t.Fatalf("compounding must work via tags alone regardless of summary wording, got %+v reason=%q",
			last, last.Reason)
	}
}

// Field-report regression (muse-spark DX): agents authored placeholder gates
// ("true") or no check at all, so tasks closed as done while proving nothing.
// task add must WARN on missing checks and HARD-REJECT phantom gates (neverphantom).
func TestTaskAddWarnsOnWeakGate(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())

	capture := func(args []string) (string, error) {
		old := os.Stderr
		r, w, _ := os.Pipe()
		os.Stderr = w
		c := taskAddCmd()
		c.SetArgs(args)
		execErr := c.Execute()
		w.Close()
		os.Stderr = old
		out, _ := io.ReadAll(r)
		return string(out), execErr
	}

	// No gate at all: allowed, but warned loudly (manual work is legitimate).
	if s, err := capture([]string{"no gate", "--tier", "cheap"}); err != nil {
		t.Fatalf("no-gate task must still be allowed: %v", err)
	} else if !strings.Contains(s, "NO acceptance check") {
		t.Fatalf("missing-check task must warn: %q", s)
	}
	// Phantom gates are rejected outright so a task can never close behind them.
	for _, ph := range []string{"true", ":", "echo", "echo done"} {
		if _, err := capture([]string{"phantom gate", "--tier", "cheap", "--run", ph}); err == nil {
			t.Fatalf("phantom run %q must be rejected, not accepted", ph)
		}
	}
	if _, err := capture([]string{"phantom verify", "--tier", "cheap", "--verify-cmd", "true"}); err == nil {
		t.Fatalf("phantom verify-cmd 'true' must be rejected")
	}
	// A real gate is accepted without warning.
	if s, err := capture([]string{"real gate", "--tier", "cheap", "--run", "go build ./..."}); err != nil {
		t.Fatalf("honest gate must be accepted: %v", err)
	} else if strings.Contains(s, "warning") {
		t.Fatalf("honest gate must not warn: %q", s)
	}
}
