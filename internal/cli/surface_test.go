package cli

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mha-home-lab/ward/internal/store"
)

// jsonRunner is satisfied by every cobra command; it exists so jsonRun can be
// called with freshly built command constructors.
type jsonRunner interface {
	SetArgs([]string)
	Execute() error
}

// jsonRun runs a command with the global jsonOut switch on and returns its
// captured stdout (--json is a root persistent flag, so tests flip the same
// variable the real parse path sets).
func jsonRun(t *testing.T, c jsonRunner, args []string) string {
	t.Helper()
	oldStdout, oldJSON := os.Stdout, jsonOut
	jsonOut = true
	defer func() { os.Stdout, jsonOut = oldStdout, oldJSON }()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	c.SetArgs(args)
	execErr := c.Execute()
	w.Close()
	os.Stdout = oldStdout
	out, _ := io.ReadAll(r)
	if execErr != nil {
		t.Fatalf("command %v failed: %v", args, execErr)
	}
	return string(out)
}

// jsonNull reports whether any decoded value is a bare null (the v0.8 surface
// contract forbids it: empty collections must serialize as [], never null).
func jsonNull(v any) bool {
	switch x := v.(type) {
	case map[string]any:
		for _, val := range x {
			if val == nil || jsonNull(val) {
				return true
			}
		}
	case []any:
		for _, val := range x {
			if val == nil || jsonNull(val) {
				return true
			}
		}
	}
	return false
}

func parseNoNull(t *testing.T, out string) map[string]any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if jsonNull(v) {
		t.Fatalf("null leaked into JSON output:\n%s", out)
	}
	m, _ := v.(map[string]any)
	return m
}

// newSurfaceStore isolates a test: temp store + temp cwd, store initialized.
func newSurfaceStore(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("WARD_HOME", filepath.Join(dir, ".ward"))
	t.Chdir(dir)
	initC := initCmd()
	initC.SetArgs([]string{"--no-agents-md"})
	if err := initC.Execute(); err != nil {
		t.Fatal(err)
	}
	return dir
}

// putLocalFact seeds one accepted store-local artifact whose verify_cmd greps
// fact.txt for grepPattern, tagged with tags[0] (used to fetch its id).
func putLocalFact(t *testing.T, dir, summary, grepPattern string, tags ...string) string {
	t.Helper()
	os.WriteFile(filepath.Join(dir, "fact.txt"), []byte(summary+"\n"), 0o644)
	put := memoryPutCmd()
	for n, v := range map[string]string{
		"summary": summary, "kind": "solution",
		"verify-cmd": "grep -q " + grepPattern + " fact.txt", "verify-kind": "shell",
		"local": "true", "by": "human", "tags": strings.Join(tags, ","),
	} {
		if err := put.Flags().Set(n, v); err != nil {
			t.Fatal(err)
		}
	}
	put.SetArgs(nil)
	if err := put.Execute(); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	res, _ := s.SearchArtifactsTagged("", "", "", tags[0], 1)
	if len(res) == 0 {
		t.Fatal("seed artifact not found")
	}
	return res[0].ID
}

// claimAndRunOne adds a runnable task, claims it as byAgent, and executes it
// via `ward task run`; returns (taskID, run-result JSON map).
func claimAndRunOne(t *testing.T, byAgent string, title string, addFlags map[string]string) (string, map[string]any) {
	t.Helper()
	execCmd(taskAddCmd(), t, []string{title}, addFlags)
	nx := taskNextCmd()
	execCmd(nx, t, nil, map[string]string{"by": byAgent, "max-tier": "strong"})
	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	claimed, _ := s.ListTasks("claimed", 5)
	s.DB.Close()
	if len(claimed) != 1 {
		t.Fatalf("%s must hold exactly one claim: %+v", byAgent, claimed)
	}
	out := parseNoNull(t, jsonRun(t, taskRunCmd(), []string{claimed[0].ID}))
	if out["task_status"] != "done" {
		t.Fatalf("task must complete for this surface test: %v", out)
	}
	return claimed[0].ID, out
}

// The v0.8 command-surface contract: on an EMPTY store, every swept command
// emits valid JSON with zero null leaks (empty collections serialize as []).
func TestSurfaceEmptyStoreJSONContract(t *testing.T) {
	newSurfaceStore(t)

	cases := []struct {
		name string
		run  func(t *testing.T) string
	}{
		{"timeline", func(t *testing.T) string { return jsonRun(t, timelineCmd(), nil) }},
		{"task list", func(t *testing.T) string { return jsonRun(t, taskListCmd(), nil) }},
		{"memory search", func(t *testing.T) string { return jsonRun(t, memorySearchCmd(), []string{"nothing"}) }},
		{"memory list", func(t *testing.T) string { return jsonRun(t, memoryListCmd(), nil) }},
		{"scorecard", func(t *testing.T) string { return jsonRun(t, scorecardCmd(), nil) }},
		{"wave empty topic", func(t *testing.T) string { return jsonRun(t, waveCmd(), []string{"topic:nope"}) }},
		{"skill-sync empty brain", func(t *testing.T) string {
			return jsonRun(t, syncCmd(), []string{"--dir", filepath.Join(t.TempDir(), "skills")})
		}},
		{"verify all", func(t *testing.T) string { return jsonRun(t, verifyCmd(), []string{"--all"}) }},
		{"memory handoff", func(t *testing.T) string { return jsonRun(t, memoryHandoffCmd(), nil) }},
		{"memory claim list", func(t *testing.T) string { return jsonRun(t, claimListCmd(), nil) }},
		{"harvest", func(t *testing.T) string { return jsonRun(t, harvestCmd(), nil) }},
		{"memory stale", func(t *testing.T) string { return jsonRun(t, memoryStaleCmd(), nil) }},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			out := tc.run(t)
			parseNoNull(t, out)
		})
	}
}

// Populated-store behavior: wave verifies a true claim, catches drift after
// the world changes, and --heal supersedes it (the regression-wave contract).
func TestWaveVerifiesCatchesDriftAndHeals(t *testing.T) {
	dir := newSurfaceStore(t)
	id := putLocalFact(t, dir, "auth uses PKCE", "PKCE", "topic:auth")

	m := parseNoNull(t, jsonRun(t, waveCmd(), []string{"topic:auth"}))
	if m["verified"].(float64) != 1 || m["drifted"].(float64) != 0 {
		t.Fatalf("wave must verify the healthy claim: %v", m)
	}

	// The world drifts: the recorded fact no longer holds.
	os.WriteFile(filepath.Join(dir, "fact.txt"), []byte("auth uses plain passwords\n"), 0o644)

	m = parseNoNull(t, jsonRun(t, waveCmd(), []string{"topic:auth"}))
	if m["drifted"].(float64) != 1 {
		t.Fatalf("drift must be caught: %v", m)
	}
	s, _ := store.Open()
	a, _ := s.GetArtifact(id)
	if a.Status != "accepted" || a.VerifyStatus != "stale" {
		t.Fatalf("without heal the artifact must stay accepted/stale: %+v", a)
	}
	s.DB.Close()

	m = parseNoNull(t, jsonRun(t, waveCmd(), []string{"topic:auth", "--heal"}))
	results := m["results"].([]any)
	healed := false
	for _, r := range results {
		if r.(map[string]any)["healed"] == true {
			healed = true
		}
	}
	if !healed {
		t.Fatalf("--heal must supersede drift: %v", m)
	}
}

// scorecard attributes outcomes (done vs bounced) from pool history alone.
func TestScorecardOutcomeAttribution(t *testing.T) {
	newSurfaceStore(t)

	execCmd(taskAddCmd(), t, []string{"good work"}, map[string]string{"tier": "mid"})
	execCmd(taskAddCmd(), t, []string{"bouncy work"}, map[string]string{"tier": "mid"})

	nxA := taskNextCmd()
	execCmd(nxA, t, nil, map[string]string{"by": "agent-a", "max-tier": "mid"})
	s, _ := store.Open()
	claimedA, _ := s.ListTasks("claimed", 10)
	if len(claimedA) != 1 {
		t.Fatalf("agent-a must hold exactly one claim: %+v", claimedA)
	}
	if err := s.CompleteTask(claimedA[0].ID, "agent-a"); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()

	nxB := taskNextCmd()
	execCmd(nxB, t, nil, map[string]string{"by": "agent-b", "max-tier": "mid"})
	s2, _ := store.Open()
	claimedB, _ := s2.ListTasks("claimed", 10)
	if len(claimedB) != 1 {
		t.Fatalf("agent-b must hold exactly one claim: %+v", claimedB)
	}
	if _, err := s2.FailTask(claimedB[0].ID); err != nil { // bounce: mid -> strong
		t.Fatal(err)
	}
	s2.DB.Close()

	var cards []struct {
		Agent   string  `json:"agent"`
		Done    float64 `json:"done"`
		Bounced float64 `json:"bounced"`
		Holding float64 `json:"currently_holding"`
	}
	out := jsonRun(t, scorecardCmd(), nil)
	if err := json.Unmarshal([]byte(out), &cards); err != nil {
		t.Fatalf("scorecard --json must be an array of engineer cards: %v\n%s", err, out)
	}
	byAgent := map[string]int{}
	for i, c := range cards {
		byAgent[c.Agent] = i
	}
	iA, okA := byAgent["agent-a"]
	iB, okB := byAgent["agent-b"]
	if !okA || !okB {
		t.Fatalf("both engineers must appear on the scorecard: %s", out)
	}
	if cards[iA].Done != 1 || cards[iB].Bounced != 1 {
		t.Fatalf("outcome attribution wrong: %s", out)
	}
}

// skill-sync pushes portable chips into a target dir and reports what it did;
// the synced chip checks FRESH against its oracle store from another cwd.
func TestSkillSyncWritesFreshChips(t *testing.T) {
	dir := newSurfaceStore(t)
	putLocalFact(t, dir, "wake docker before DoD gates", "docker", "portable:ops")

	target := filepath.Join(t.TempDir(), "skills")
	out := jsonRun(t, syncCmd(), []string{"--dir", target})
	m := parseNoNull(t, out)
	if len(m["synced"].([]any)) != 1 {
		t.Fatalf("one portable topic must sync: %s", out)
	}
	chipPath := filepath.Join(target, "ward-ops", "SKILL.md")
	data, err := os.ReadFile(chipPath)
	if err != nil {
		t.Fatalf("synced chip missing: %v", err)
	}
	if !strings.Contains(string(data), "store: ") {
		t.Fatal("chip must carry its oracle-store locator")
	}

	// Audit from a foreign cwd under a foreign WARD_HOME: the locator steers.
	foreign := t.TempDir()
	t.Chdir(foreign)
	t.Setenv("WARD_HOME", filepath.Join(foreign, ".ward"))
	cm := parseNoNull(t, jsonRun(t, skillCheckCmd(), []string{filepath.Dir(chipPath)}))
	if cm["verdict"] != "FRESH" {
		t.Fatalf("freshly synced chip must check FRESH via locator: %v", cm)
	}
}

// timeline unifies routing spans, task transitions, and captures, newest first.
func TestTimelineUnifiesSpansTransitionsCaptures(t *testing.T) {
	dir := newSurfaceStore(t)
	putLocalFact(t, dir, "timeline fact", "timeline", "topic:tl")

	_, runOut := claimAndRunOne(t, "agent-tl", "timed work", map[string]string{
		"tier": "mid", "run": "go version",
		"verify-cmd": "grep -q timeline fact.txt", "tags": "topic:tl",
	})
	_ = runOut

	var spans []map[string]any
	out := jsonRun(t, timelineCmd(), nil)
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &spans); err != nil {
		t.Fatalf("timeline --json must be an array: %v\n%s", err, out)
	}
	kinds := map[string]bool{}
	for _, sp := range spans {
		kinds[sp["Kind"].(string)] = true
	}
	for _, want := range []string{"route", "task", "capture"} {
		if !kinds[want] {
			t.Fatalf("timeline missing %q span; kinds=%v spans=%d", want, kinds, len(spans))
		}
	}
	for i := 1; i < len(spans); i++ {
		if spans[i-1]["At"].(string) < spans[i]["At"].(string) {
			t.Fatalf("timeline must be newest-first: %v before %v", spans[i-1]["At"], spans[i]["At"])
		}
	}
}

// explain and reject emit valid structured evidence in --json mode too, and
// the explain node filter applies in BOTH output modes.
func TestExplainAndRejectJSONShapes(t *testing.T) {
	dir := newSurfaceStore(t)
	putLocalFact(t, dir, "explainable fact", "explainable", "topic:ex")

	taskID, runOut := claimAndRunOne(t, "agent-e", "explained work", map[string]string{
		"tier": "mid", "run": "go version", "verify-cmd": "grep -q explainable fact.txt",
	})
	runID := runOut["run"].(string)

	explain := parseNoNull(t, jsonRun(t, explainCmd(), []string{runID}))
	if decs := explain["decisions"].([]any); len(decs) == 0 {
		t.Fatalf("explain must include decisions: %s", runID)
	}
	if evs := explain["events"].([]any); len(evs) == 0 {
		t.Fatalf("explain must include events: %s", runID)
	}

	nodeID := "work-" + strings.TrimPrefix(taskID, "task-")
	filtered := parseNoNull(t, jsonRun(t, explainCmd(), []string{runID, nodeID}))
	fd := filtered["decisions"].([]any)
	if len(fd) != 1 || fd[0].(map[string]any)["Node"] != nodeID {
		t.Fatalf("node filter must apply in json mode: %v", fd)
	}

	rej := parseNoNull(t, jsonRun(t, rejectCmd(), []string{runID}))
	if rej["run"] == nil {
		t.Fatalf("reject --json must carry the run: %s", runID)
	}
}

// capture reports honestly in JSON: an explicit re-capture names every
// artifact id it wrote, never a phantom count.
func TestCaptureJSONHonestCounts(t *testing.T) {
	newSurfaceStore(t)

	taskID, runOut := claimAndRunOne(t, "agent-c", "captured work", map[string]string{
		"tier": "mid", "run": "go version", "verify-cmd": "test -x /tmp",
	})
	runID := runOut["run"].(string)

	// Capture all done nodes from the completed run (flow.md step 7).
	out := jsonRun(t, captureCmd(), []string{"--run", runID})
	m := parseNoNull(t, out)
	if len(m["captured"].([]any)) < 1 {
		t.Fatalf("explicit capture of done nodes must record ids: %v", m)
	}

	// Single-node capture via workflow path also lands exactly one id.
	wfPath := "workflows/task-" + strings.TrimPrefix(taskID, "task-") + ".yaml"
	if _, err := os.Stat(wfPath); err != nil {
		t.Fatalf("task workflow persisted: %v", err)
	}
	out = jsonRun(t, captureCmd(), []string{
		"--node", "work-" + strings.TrimPrefix(taskID, "task-"), "--workflow", wfPath,
	})
	m = parseNoNull(t, out)
	if len(m["captured"].([]any)) != 1 {
		t.Fatalf("single-node capture must record exactly one id: %v", m)
	}
}
