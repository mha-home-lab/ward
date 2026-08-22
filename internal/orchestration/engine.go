package orchestration

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/mha-home-lab/ward/internal/adapter"
	"github.com/mha-home-lab/ward/internal/observe"
	"github.com/mha-home-lab/ward/internal/routing"
	"github.com/mha-home-lab/ward/internal/store"
	"github.com/mha-home-lab/ward/internal/verification"
)

// Engine runs workflows, persists run state, and applies the pure router.
type Engine struct {
	Store       *store.Store
	RepoRoot    string
	AutoApprove bool
}

// StartWorkflow creates a run and runs it until an approval pause or completion.
func (e *Engine) StartWorkflow(wf *Workflow) (string, error) {
	runID := "run-" + sha8run(wf.Name+time.Now().String())
	now := store.NowISO()
	if err := e.Store.CreateRun(store.RunState{
		ID: runID, WorkflowName: wf.Name, WorkflowPath: wf.Path, Status: "running",
		Ceremony: "light", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		return "", err
	}
	for _, n := range wf.Nodes {
		if err := e.Store.UpsertRunNode(store.RunNode{
			RunID: runID, Node: n.ID, Status: "pending", UpdatedAt: now,
		}); err != nil {
			return "", err
		}
	}
	return runID, e.Run(runID, wf)
}

// Run advances the run from its current persisted state to the next pause.
func (e *Engine) Run(runID string, wf *Workflow) error {
	order, err := wf.TopoOrder()
	if err != nil {
		return err
	}
	done, err := e.doneMap(runID)
	if err != nil {
		return err
	}
	escal, err := e.escalMap(runID)
	if err != nil {
		return err
	}
	for {
		next := ""
		for _, id := range order {
			if !done[id] {
				next = id
				break
			}
		}
		if next == "" {
			_ = e.Store.SaveRun(store.RunState{
				ID: runID, WorkflowName: wf.Name, WorkflowPath: wf.Path, Status: "completed",
				UpdatedAt: store.NowISO(),
			})
			return nil
		}
		paused, err := e.stepNode(runID, wf, next, done, escal)
		if err != nil {
			return err
		}
		if paused {
			return nil
		}
	}
}

// Approve resolves an awaiting_approval node and resumes the run.
func (e *Engine) Approve(runID, nodeID string, wf *Workflow) error {
	r, err := e.Store.LoadRun(runID)
	if err != nil {
		return err
	}
	if r.WaitingApproval != nodeID {
		return fmt.Errorf("run not awaiting approval at %s", nodeID)
	}
	if err := e.Store.UpsertRunNode(store.RunNode{
		RunID: runID, Node: nodeID, Status: "done", UpdatedAt: store.NowISO(),
	}); err != nil {
		return err
	}
	_ = e.Store.AddEvent(runID, "approve", nodeID, "human/agent approved")
	return e.Run(runID, wf)
}

func (e *Engine) stepNode(runID string, wf *Workflow, nodeID string, done map[string]bool, escal map[string]int) (bool, error) {
	nm := wf.nodeMap()
	node := nm[nodeID]

	esc := escal[nodeID]
	hit, verify, verifiedIDs := e.memoryHitForNode(node)
	contention, overlaps := e.contentionForNode(wf, node, done)
	dec := routing.Route(routing.Inputs{
		NodeKind: node.Kind, MemoryHit: hit, Verify: verify, Contention: contention,
		Escalation: esc, DeclaredTier: node.Tier,
	})
	ctxJSON, _ := json.Marshal(verifiedIDs)
	if dec.Reject {
		// Escalation budget exhausted: do not mark done. The run is rejected /
		// routes to a human.
		_ = e.Store.UpsertRunNode(store.RunNode{
			RunID: runID, Node: nodeID, Status: "failed", Escalation: esc, UpdatedAt: store.NowISO(),
		})
		_ = e.Store.SaveRun(store.RunState{
			ID: runID, WorkflowName: wf.Name, WorkflowPath: wf.Path, Status: "rejected", UpdatedAt: store.NowISO(),
		})
		_ = e.Store.AddEvent(runID, "reject", nodeID, dec.Reason)
		return true, nil
	}
	cj, _ := json.Marshal(map[string]any{"overlaps": overlaps, "node_touched": node.Produces})
	_ = e.Store.AddRoutingDecision(store.RoutingDecision{
		RunID: runID, Node: nodeID, Tier: string(dec.Tier), Model: dec.Model,
		Ceremony: dec.Ceremony, MemoryHit: dec.MemoryHit, VerifyStatus: dec.Verify,
		Contention: contention, Reason: dec.Reason, Context: string(ctxJSON),
		ContentionJSON: string(cj), CreatedAt: store.NowISO(),
	})
	_ = e.Store.UpsertRunNode(store.RunNode{
		RunID: runID, Node: nodeID, Status: "running", Ceremony: dec.Ceremony,
		Escalation: esc, UpdatedAt: store.NowISO(),
	})

	if node.Kind == "approval" && !e.AutoApprove {
		_ = e.Store.SaveRun(store.RunState{
			ID: runID, WorkflowName: wf.Name, WorkflowPath: wf.Path, Status: "awaiting_approval",
			WaitingApproval: nodeID, UpdatedAt: store.NowISO(),
		})
		for _, ch := range node.Channels {
			_ = e.Store.AddEvent(runID, "channel", nodeID, "post to "+ch)
		}
		return true, nil
	}

	// The REAL adapter: a node's work happens here. If it carries a `prompt`,
	// drive a model (at the routed tier) via opencode. If it carries a `run`
	// command, execute it against the repo. Either can fail -> escalate, never
	// silently succeed. The routing/verify logic above is untouched.
	if node.Prompt != "" {
		out, merr := adapter.Run(e.repo(), adapter.ModelForTier(string(dec.Tier)), node.Prompt)
		detail := "model ok"
		if merr != nil {
			detail = "model failed: " + merr.Error()
		}
		if len(out) > 0 {
			detail += " | " + truncate(out, 200)
		}
		_ = e.Store.AddEvent(runID, "model", nodeID, detail)
		if merr != nil {
			return e.failNode(runID, wf, nodeID, esc, escal, "model failed")
		}
	}
	if node.Run != "" {
		out, rerr := execShell(node.Run, e.repo())
		detail := "exec ok"
		if rerr != nil {
			detail = "exec failed: " + rerr.Error()
		}
		if len(out) > 0 {
			detail += " | " + truncate(string(out), 200)
		}
		_ = e.Store.AddEvent(runID, "exec", nodeID, detail)
		if rerr != nil {
			return e.failNode(runID, wf, nodeID, esc, escal, "run failed")
		}
	}

	// Success (no run, or run succeeded): mark done, persist declared touched,
	// and record the git-diff OBSERVATION (never a routing input — D0.1).
	obs := e.observe(node)
	_ = e.Store.UpsertRunNode(store.RunNode{
		RunID: runID, Node: nodeID, Status: "done", Touched: node.Produces,
		Ceremony: dec.Ceremony, DeclaredObs: obs, Escalation: esc, UpdatedAt: store.NowISO(),
	})
	done[nodeID] = true
	_ = e.Store.AddEvent(runID, "done", nodeID, obs)
	return false, nil
}

// failNode records a failed work attempt and applies the escalation budget:
// bump the retry count and re-route the SAME node at the higher tier the router
// now selects, or reject/hand-to-human once the budget (max 2) is spent. Never
// marks the node done.
func (e *Engine) failNode(runID string, wf *Workflow, nodeID string, esc int, escal map[string]int, reason string) (bool, error) {
	newEsc := esc + 1
	if newEsc > routing.MaxEscalation {
		_ = e.Store.UpsertRunNode(store.RunNode{
			RunID: runID, Node: nodeID, Status: "failed", Escalation: newEsc, UpdatedAt: store.NowISO(),
		})
		_ = e.Store.SaveRun(store.RunState{
			ID: runID, WorkflowName: wf.Name, WorkflowPath: wf.Path, Status: "rejected", UpdatedAt: store.NowISO(),
		})
		_ = e.Store.AddEvent(runID, "reject", nodeID, "escalation budget exhausted (max 2): "+reason)
		return true, nil
	}
	escal[nodeID] = newEsc
	_ = e.Store.UpsertRunNode(store.RunNode{
		RunID: runID, Node: nodeID, Status: "failed", Escalation: newEsc, UpdatedAt: store.NowISO(),
	})
	_ = e.Store.AddEvent(runID, "escalate", nodeID, fmt.Sprintf("%s; retry at higher tier (attempt %d/3)", reason, newEsc+1))
	return false, nil // not paused -> Run re-picks this node and re-routes
}

// memoryHitForNode returns whether a VERIFIED prior solution exists for this
// node. The thesis: an artifact may not vote for cheap until it matches current
// repo state. So before trusting, we run verification.Run LIVE against the repo
// and persist the result. Only status=="verified" counts as a real hit.
func (e *Engine) memoryHitForNode(node Node) (bool, string, []string) {
	cands := map[string]store.Artifact{}
	for _, q := range []string{node.ID, node.Kind} {
		res, err := e.Store.SearchArtifacts(q, "", "", 10)
		if err == nil {
			for _, a := range res {
				cands[a.ID] = a
			}
		}
	}
	// Only artifacts explicitly tagged with this exact node id count. The loose
	// "any accepted artifact of the same kind" fallback caused false hits.
	bestStatus := ""
	var verifiedIDs []string
	for _, a := range cands {
		if a.Status != "accepted" {
			continue
		}
		if !hasTag(a.Tags, node.ID) {
			continue
		}
		// LIVE gate: verify against the repo right now, then persist.
		res := verification.Run(a, e.repo())
		_ = e.Store.SetVerify(a.ID, res.Status)
		if res.Status == "verified" {
			verifiedIDs = append(verifiedIDs, a.ID)
			continue
		}
		bestStatus = res.Status
	}
	if len(verifiedIDs) > 0 {
		// A verified prior solution exists: it is the ONLY context carried into
		// the (re-)attempt. Failed-attempt prose is never persisted as context.
		return true, "verified", verifiedIDs
	}
	if bestStatus != "" {
		// accepted but not currently verified: a hit, but it cannot vote cheap.
		return true, bestStatus, nil
	}
	return false, "unknown", nil
}

// repo returns the configured repo root, defaulting to the process cwd.
func (e *Engine) repo() string {
	if e.RepoRoot != "" {
		return e.RepoRoot
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// Seed creates an accepted artifact and runs its verify_cmd LIVE (never stamps
// a status). The resulting verify_status is whatever the repo actually says.
func (e *Engine) Seed(nodeID, kind, tagKind, summary, verifyCmd, verifyKind string) {
	a := store.Artifact{
		Kind: tagKind, Summary: summary, Content: "seeded for " + nodeID,
		Tags:   []string{nodeID, kind},
		Status: "accepted", CreatedBy: "seed", Local: true,
		VerifyKind: verifyKind, VerifyCmd: verifyCmd, Ceremony: "light",
	}
	id, err := e.Store.UpsertArtifact(a)
	if err != nil {
		return
	}
	res := verification.Run(a, e.repo())
	_ = e.Store.SetVerify(id, res.Status)
}

func hasTag(tags []string, v string) bool {
	for _, t := range tags {
		if t == v {
			return true
		}
	}
	return false
}

// contentionForNode detects overlap between this node's declared touched set
// and any already-completed node's declared touched set.
func (e *Engine) contentionForNode(wf *Workflow, node Node, done map[string]bool) (bool, []string) {
	nm := wf.nodeMap()
	var overlaps []string
	for id := range done {
		dn := nm[id]
		if !overlap(dn.Produces, node.Produces) {
			continue
		}
		// Only UNORDERED nodes (neither is an ancestor of the other) can run
		// concurrently, so only they genuinely contend. Sequential overlap is
		// expected and must not force escalation (the D0.1 false-positive trap).
		if wf.Reachable(id)[node.ID] || wf.Reachable(node.ID)[id] {
			continue
		}
		overlaps = append(overlaps, id)
	}
	return len(overlaps) > 0, overlaps
}

func overlap(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y || strings.HasPrefix(x, y) || strings.HasPrefix(y, x) {
				return true
			}
		}
	}
	return false
}

// observe records declared-vs-git-diff as an OBSERVATION ONLY (D0.1 evidence).
// It logs BOTH sets (not just counts) so under-declaration is visible. It never
// feeds back into routing.
func (e *Engine) observe(node Node) string {
	changed := []string{}
	if r := e.repo(); r != "" {
		if f, err := observe.GitChangedFiles(r); err == nil {
			changed = append(changed, f...)
		}
		if f, err := observe.GitUntracked(r); err == nil {
			changed = append(changed, f...)
		}
	}
	b, _ := json.Marshal(map[string]any{"declared": node.Produces, "observed": changed})
	return string(b)
}

// execShell runs a node's `run` command in the repo root (the real adapter).
func execShell(cmd, repo string) ([]byte, error) {
	c := exec.Command("sh", "-c", cmd)
	if repo != "" {
		c.Dir = repo
	}
	return c.CombinedOutput()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func (e *Engine) doneMap(runID string) (map[string]bool, error) {
	nodes, err := e.Store.LoadRunNodes(runID)
	if err != nil {
		return nil, err
	}
	m := map[string]bool{}
	for _, n := range nodes {
		if n.Status == "done" {
			m[n.Node] = true
		}
	}
	return m, nil
}

// escalMap returns the current retry count per node (for the escalation budget).
func (e *Engine) escalMap(runID string) (map[string]int, error) {
	nodes, err := e.Store.LoadRunNodes(runID)
	if err != nil {
		return nil, err
	}
	m := map[string]int{}
	for _, n := range nodes {
		m[n.Node] = n.Escalation
	}
	return m, nil
}

func sha8run(s string) string {
	return store.SHA8(s)
}
