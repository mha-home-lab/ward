package orchestration

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mha-home-lab/ward/internal/observe"
	"github.com/mha-home-lab/ward/internal/routing"
	"github.com/mha-home-lab/ward/internal/store"
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
		ID: runID, WorkflowName: wf.Name, Status: "running",
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
				ID: runID, WorkflowName: wf.Name, Status: "completed",
				UpdatedAt: store.NowISO(),
			})
			return nil
		}
		paused, err := e.stepNode(runID, wf, next, done)
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

func (e *Engine) stepNode(runID string, wf *Workflow, nodeID string, done map[string]bool) (bool, error) {
	nm := wf.nodeMap()
	node := nm[nodeID]

	hit, verify := e.memoryHitForNode(node)
	contention, overlaps := e.contentionForNode(wf, node, done)
	dec := routing.Route(routing.Inputs{
		NodeKind: node.Kind, MemoryHit: hit, Verify: verify, Contention: contention,
	})
	if dec.Reject {
		_ = e.Store.SaveRun(store.RunState{
			ID: runID, WorkflowName: wf.Name, Status: "rejected", UpdatedAt: store.NowISO(),
		})
		_ = e.Store.AddEvent(runID, "reject", nodeID, dec.Reason)
		return true, nil
	}
	cj, _ := json.Marshal(map[string]any{"overlaps": overlaps, "node_touched": node.Produces})
	_ = e.Store.AddRoutingDecision(store.RoutingDecision{
		RunID: runID, Node: nodeID, Tier: string(dec.Tier), Model: dec.Model,
		Ceremony: dec.Ceremony, MemoryHit: dec.MemoryHit, VerifyStatus: dec.Verify,
		Reason: dec.Reason, ContentionJSON: string(cj), CreatedAt: store.NowISO(),
	})
	_ = e.Store.UpsertRunNode(store.RunNode{
		RunID: runID, Node: nodeID, Status: "running", Ceremony: dec.Ceremony, UpdatedAt: store.NowISO(),
	})

	if node.Kind == "approval" && !e.AutoApprove {
		_ = e.Store.SaveRun(store.RunState{
			ID: runID, WorkflowName: wf.Name, Status: "awaiting_approval",
			WaitingApproval: nodeID, UpdatedAt: store.NowISO(),
		})
		for _, ch := range node.Channels {
			_ = e.Store.AddEvent(runID, "channel", nodeID, "post to "+ch)
		}
		return true, nil
	}

	// Non-approval (or auto-approved) node: mark done, persist declared touched,
	// and record the git-diff OBSERVATION (never a routing input — D0.1).
	obs := e.observe(node)
	_ = e.Store.UpsertRunNode(store.RunNode{
		RunID: runID, Node: nodeID, Status: "done", Touched: node.Produces,
		Ceremony: dec.Ceremony, DeclaredObs: obs, UpdatedAt: store.NowISO(),
	})
	done[nodeID] = true
	_ = e.Store.AddEvent(runID, "done", nodeID, obs)
	return false, nil
}

// memoryHitForNode returns whether a verified (or at least accepted) prior
// solution exists in the store for this node.
func (e *Engine) memoryHitForNode(node Node) (bool, string) {
	cands := map[string]store.Artifact{}
	for _, q := range []string{node.ID, node.Kind} {
		res, err := e.Store.SearchArtifacts(q, "", "", 10)
		if err == nil {
			for _, a := range res {
				cands[a.ID] = a
			}
		}
	}
	for _, a := range cands {
		if a.Status != "accepted" {
			continue
		}
		if strings.Contains(a.Summary, node.ID) || hasTag(a.Tags, node.ID) {
			return true, a.VerifyStatus
		}
	}
	// fall back to any accepted artifact of the same kind
	for _, a := range cands {
		if a.Status == "accepted" && a.Kind == node.Kind {
			return true, a.VerifyStatus
		}
	}
	return false, "unknown"
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
// It never feeds back into routing.
func (e *Engine) observe(node Node) string {
	changed := []string{}
	if e.RepoRoot != "" {
		if f, err := observe.GitChangedFiles(e.RepoRoot); err == nil {
			changed = append(changed, f...)
		}
		if f, err := observe.GitUntracked(e.RepoRoot); err == nil {
			changed = append(changed, f...)
		}
	}
	return fmt.Sprintf("declared=%d observed_git=%d", len(node.Produces), len(changed))
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

func sha8run(s string) string {
	return store.SHA8(s)
}
