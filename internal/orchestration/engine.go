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

	// AllowWorkflowDrift overrides the resume guard: a run whose workflow
	// file changed since start is refused by default (external review
	// finding — resuming a DIFFERENT definition than the one that created
	// the run breaks reproducibility). Explicit opt-in re-enables it.
	AllowWorkflowDrift bool

	// lastFail tracks each node's most recent failure detail within one Run
	// pass, powering the identical-failure short-circuit (rd:c1 f0b662e1):
	// a deterministic run: command that failed identically gains nothing from
	// a tier climb, so we stop honestly instead of burning the budget.
	lastFail map[string]string
}

// StartWorkflow creates a run and runs it until an approval pause or completion.
func (e *Engine) StartWorkflow(wf *Workflow) (string, error) {
	runID := "run-" + sha8run(wf.Name+time.Now().String())
	now := store.NowISO()
	defHash, err := wf.DefinitionHash()
	if err != nil {
		return "", fmt.Errorf("hash workflow definition: %w", err)
	}
	if err := e.Store.CreateRun(store.RunState{
		ID: runID, WorkflowName: wf.Name, WorkflowPath: wf.Path, WorkflowHash: defHash,
		Status:   "running",
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
	// Workflow-drift guard: the run row records the definition hash captured
	// at start. Resuming under a mutated YAML would silently execute a
	// different workflow than the one that created the run — refuse unless
	// explicitly overridden. Runs created before v6 have no hash and are
	// honestly unguarded.
	if r, rerr := e.Store.LoadRun(runID); rerr == nil && r.WorkflowHash != "" && !e.AllowWorkflowDrift {
		now, herr := wf.DefinitionHash()
		if herr != nil {
			return fmt.Errorf("hash workflow definition: %w", herr)
		}
		if now != r.WorkflowHash {
			return fmt.Errorf("workflow changed since run %s started (definition hash mismatch): refusing to resume a different definition; pass --allow-drift to override or start a new run", runID)
		}
	}
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
	e.lastFail = map[string]string{}
	for {
		next := ""
		for _, id := range order {
			if !done[id] {
				next = id
				break
			}
		}
		if next == "" {
			// The terminal transition must be checked: a silently-failed
			// SaveRun here is exactly the "executed fine but the store says
			// running forever" divergence (external review finding).
			return e.Store.SaveRun(store.RunState{
				ID: runID, WorkflowName: wf.Name, WorkflowPath: wf.Path, Status: "completed",
				UpdatedAt: store.NowISO(),
			})
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
	if err := e.Store.AddEvent(runID, "approve", nodeID, "human/agent approved"); err != nil {
		return err
	}
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
		// routes to a human — with a dossier, so the human inherits the
		// evidence packet instead of a bare status string.
		//
		// State-machine writes are CHECKED (external review finding: ignored
		// persistence errors here could diverge the durable state machine
		// from reality — e.g. execute succeeded but node=failed never landed,
		// and ward's entire value is trustworthy transitions).
		if err := e.Store.UpsertRunNode(store.RunNode{
			RunID: runID, Node: nodeID, Status: "failed", Escalation: esc, UpdatedAt: store.NowISO(),
		}); err != nil {
			return true, fmt.Errorf("persist failed-node %s: %w", nodeID, err)
		}
		if err := e.Store.SaveRun(store.RunState{
			ID: runID, WorkflowName: wf.Name, WorkflowPath: wf.Path, Status: "rejected", UpdatedAt: store.NowISO(),
		}); err != nil {
			return true, fmt.Errorf("persist rejected run: %w", err)
		}
		if err := e.Store.AddEvent(runID, "reject", nodeID, dec.Reason); err != nil {
			return true, fmt.Errorf("persist reject event: %w", err)
		}
		e.WriteDossier(runID, nodeID)
		return true, nil
	}
	cj, _ := json.Marshal(map[string]any{"overlaps": overlaps, "node_touched": node.Produces})
	if err := e.Store.AddRoutingDecision(store.RoutingDecision{
		RunID: runID, Node: nodeID, Tier: string(dec.Tier), Model: dec.Model,
		Ceremony: dec.Ceremony, MemoryHit: dec.MemoryHit, VerifyStatus: dec.Verify,
		Contention: contention, Reason: dec.Reason, Context: string(ctxJSON),
		ContentionJSON: string(cj), CreatedAt: store.NowISO(),
	}); err != nil {
		return true, fmt.Errorf("persist routing decision for %s: %w", nodeID, err)
	}
	if err := e.Store.UpsertRunNode(store.RunNode{
		RunID: runID, Node: nodeID, Status: "running", Ceremony: dec.Ceremony,
		Escalation: esc, UpdatedAt: store.NowISO(),
	}); err != nil {
		return true, fmt.Errorf("persist running-node %s: %w", nodeID, err)
	}

	if node.Kind == "approval" && !e.AutoApprove {
		if err := e.Store.SaveRun(store.RunState{
			ID: runID, WorkflowName: wf.Name, WorkflowPath: wf.Path, Status: "awaiting_approval",
			WaitingApproval: nodeID, UpdatedAt: store.NowISO(),
		}); err != nil {
			return true, fmt.Errorf("persist awaiting_approval: %w", err)
		}
		for _, ch := range node.Channels {
			if err := e.Store.AddEvent(runID, "channel", nodeID, "post to "+ch); err != nil {
				return true, fmt.Errorf("persist channel event: %w", err)
			}
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
		if err := e.Store.AddEvent(runID, "model", nodeID, detail); err != nil {
			// The attempt transcript is evidence (dossiers/explain read it
			// back); a failed write must not silently hollow out the audit
			// trail while execution proceeds.
			return false, fmt.Errorf("persist model event: %w", err)
		}
		if merr != nil {
			return e.failNode(runID, wf, nodeID, esc, escal, "model failed")
		}
	}
	if node.Run != "" {
		out, exitCode, rerr := e.execLogged(runID, nodeID, node.Run, e.repo())
		detail := "exec ok"
		if rerr != nil {
			detail = fmt.Sprintf("exec failed (exit %d): %s", exitCode, rerr.Error())
		}
		if len(out) > 0 {
			detail += " | " + truncate(string(out), 200)
		}
		if err := e.Store.AddEvent(runID, "exec", nodeID, detail); err != nil {
			return false, fmt.Errorf("persist exec event: %w", err)
		}
		if rerr != nil {
			// Pre-flight gate (rd:c2 bfd02833): these signatures mean the
			// CHECK ITSELF cannot run (missing target file, uninstalled
			// module, nothing collected) — the work is not wrong, the gate
			// is. Reject immediately without burning a single escalation:
			// stronger tiers cannot execute a broken check either.
			if preFlightBrokenCheck(detail) {
				if err := e.Store.AddEvent(runID, "reject", nodeID,
					"preflight: check is non-executable (fix the task's run/verify command): "+detail); err != nil {
					return true, fmt.Errorf("persist preflight reject event: %w", err)
				}
				if err := e.Store.UpsertRunNode(store.RunNode{RunID: runID, Node: nodeID, Status: "failed", Escalation: esc, UpdatedAt: store.NowISO()}); err != nil {
					return true, fmt.Errorf("persist preflight failed-node: %w", err)
				}
				if err := e.Store.SaveRun(store.RunState{ID: runID, WorkflowName: wf.Name, WorkflowPath: wf.Path, Status: "rejected", UpdatedAt: store.NowISO()}); err != nil {
					return true, fmt.Errorf("persist preflight rejected run: %w", err)
				}
				e.WriteDossier(runID, nodeID)
				return true, nil
			}
			// Identical-failure short-circuit: no model in the loop means the
			// retry is byte-identical work; a tier climb cannot change it.
			if node.Prompt == "" && e.lastFail[nodeID] == detail && esc > 0 {
				if err := e.Store.AddEvent(runID, "reject", nodeID,
					"identical failure repeated without progress (no model in loop): "+detail); err != nil {
					return true, fmt.Errorf("persist identical-fail event: %w", err)
				}
				if err := e.Store.UpsertRunNode(store.RunNode{RunID: runID, Node: nodeID, Status: "failed", Escalation: esc, UpdatedAt: store.NowISO()}); err != nil {
					return true, fmt.Errorf("persist identical-fail node: %w", err)
				}
				if err := e.Store.SaveRun(store.RunState{ID: runID, WorkflowName: wf.Name, WorkflowPath: wf.Path, Status: "rejected", UpdatedAt: store.NowISO()}); err != nil {
					return true, fmt.Errorf("persist identical-fail run: %w", err)
				}
				e.WriteDossier(runID, nodeID)
				return true, nil
			}
			e.lastFail[nodeID] = detail
			return e.failNode(runID, wf, nodeID, esc, escal, "run failed")
		}
	}

	// Success (no run, or run succeeded): mark done, persist declared touched,
	// and record the git-diff OBSERVATION (never a routing input — D0.1).
	obs := e.observe(node)
	if err := e.Store.UpsertRunNode(store.RunNode{
		RunID: runID, Node: nodeID, Status: "done", Touched: node.Produces,
		Ceremony: dec.Ceremony, DeclaredObs: obs, Escalation: esc, UpdatedAt: store.NowISO(),
	}); err != nil {
		// Refuse to let the run claim completion while the node's done-state
		// failed to persist — that divergence is the failure mode reviews
		// flagged. The caller surfaces it; nothing is marked completed.
		return false, fmt.Errorf("persist done-node %s: %w", nodeID, err)
	}
	done[nodeID] = true
	if err := e.Store.AddEvent(runID, "done", nodeID, obs); err != nil {
		return false, fmt.Errorf("persist done event: %w", err)
	}
	// Stamp the check outcome onto the routing span (field report bug 8): the
	// decision is recorded pre-execution, but leaving verify=unknown forever
	// starves harvest/scorecard of real pass data when the check ran green.
	// Deliberately best-effort: this is telemetry enrichment on an already-
	// persisted decision; failure changes no admission and loses only a label.
	if node.Run != "" {
		_, _ = e.Store.DB.Exec(`UPDATE routing_decisions SET verify_status='passed'
			WHERE verify_status != 'verified' AND id=(SELECT id FROM routing_decisions
			WHERE run_id=? AND node=? ORDER BY created_at DESC, id DESC LIMIT 1)`, runID, nodeID)
	}
	return false, nil
}

// failNode records a failed work attempt and applies the escalation budget:
// bump the retry count and re-route the SAME node at the higher tier the router
// now selects, or reject/hand-to-human once the budget (max 2) is spent. Never
// marks the node done.
func (e *Engine) failNode(runID string, wf *Workflow, nodeID string, esc int, escal map[string]int, reason string) (bool, error) {
	newEsc := esc + 1
	if newEsc > routing.MaxEscalation {
		if err := e.Store.UpsertRunNode(store.RunNode{
			RunID: runID, Node: nodeID, Status: "failed", Escalation: newEsc, UpdatedAt: store.NowISO(),
		}); err != nil {
			return true, fmt.Errorf("persist exhausted failed-node: %w", err)
		}
		if err := e.Store.SaveRun(store.RunState{
			ID: runID, WorkflowName: wf.Name, WorkflowPath: wf.Path, Status: "rejected", UpdatedAt: store.NowISO(),
		}); err != nil {
			return true, fmt.Errorf("persist exhausted rejected run: %w", err)
		}
		if err := e.Store.AddEvent(runID, "reject", nodeID, "escalation budget exhausted (max 2): "+reason); err != nil {
			return true, fmt.Errorf("persist exhaustion event: %w", err)
		}
		e.WriteDossier(runID, nodeID)
		return true, nil
	}
	escal[nodeID] = newEsc
	if err := e.Store.UpsertRunNode(store.RunNode{
		RunID: runID, Node: nodeID, Status: "failed", Escalation: newEsc, UpdatedAt: store.NowISO(),
	}); err != nil {
		return true, fmt.Errorf("persist failed-node %s: %w", nodeID, err)
	}
	if err := e.Store.AddEvent(runID, "escalate", nodeID, fmt.Sprintf("%s; retry at higher tier (attempt %d/3)", reason, newEsc+1)); err != nil {
		return true, fmt.Errorf("persist escalate event: %w", err)
	}
	return false, nil // not paused -> Run re-picks this node and re-routes
}

// WriteDossier synthesizes the reject dossier from evidence ALREADY COLLECTED:
// the run's event log (each attempt's outcome), the tier path taken from the
// persisted routing decisions, and the verified context that was available. It
// never runs new commands or invents diagnosis — a faithful transcript plus the
// recommendation to involve a human. The dossier is itself a store-local,
// accepted artifact tagged `reject:<runID>`, so the next session finds it via
// memory context like any other fact.
func (e *Engine) WriteDossier(runID, nodeID string) {
	events, err := e.Store.LoadEvents(runID)
	if err != nil {
		return
	}
	decs, err := e.Store.RoutingDecisionsForRun(runID)
	if err != nil {
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, "REJECTED: run %s node %s — escalation budget exhausted; needs a human.\n\n", runID, nodeID)
	b.WriteString("Tier path taken:\n")
	for _, d := range decs {
		if d.Node != nodeID {
			continue
		}
		fmt.Fprintf(&b, "  attempt -> tier=%s model=%s hit=%v verify=%s (%s)\n",
			d.Tier, d.Model, d.MemoryHit, d.VerifyStatus, d.Reason)
		if ids := contextIDsOf(d.Context); len(ids) > 0 {
			fmt.Fprintf(&b, "    verified context available: %s\n", strings.Join(ids, ", "))
		} else {
			b.WriteString("    no verified context was available for this attempt\n")
		}
	}
	b.WriteString("\nAttempt transcript:\n")
	for _, ev := range events {
		if ev.Node != nodeID {
			continue
		}
		fmt.Fprintf(&b, "  [%s] %s: %s\n", ev.At, ev.Action, ev.Detail)
	}
	// Failure tail: the evidence the human actually needs — WHY it failed, not
	// just "failed 2x". Pull the last lines of the sidecar log for this run.
	if content, ok := store.ReadSidecar(runID); ok {
		b.WriteString("\nLast execution evidence (from sidecar log):\n")
		for _, l := range store.Tail(content, 20) {
			b.WriteString("  " + l + "\n")
		}
	}
	a := store.Artifact{
		Kind:      "error",
		Summary:   fmt.Sprintf("REJECT run %s node %s: escalation budget spent, human needed", runID, nodeID),
		Content:   b.String(),
		Tags:      []string{"dossier", "reject:" + runID},
		Status:    "accepted",
		CreatedBy: "ward",
		Local:     true,
		Ceremony:  "light",
	}
	// Best-effort by design: the dossier is a derived synthesis for humans;
	// if it cannot be written the rejection itself is still durably recorded
	// (checked above), and WriteDossier already no-ops on read errors.
	_, _ = e.Store.UpsertArtifact(a)
}

// contextIDsOf parses the persisted context JSON of a decision.
func contextIDsOf(ctxJSON string) []string {
	ctxJSON = strings.TrimSpace(ctxJSON)
	if ctxJSON == "" || ctxJSON == "null" || ctxJSON == "[]" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(ctxJSON), &ids); err != nil {
		return nil
	}
	return ids
}

// memoryHitForNode returns whether a VERIFIED prior solution exists for this
// node. The thesis: an artifact may not vote for cheap until it matches current
// repo state. So before trusting, we run verification.Run LIVE against the repo
// and persist the result. Only status=="verified" counts as a real hit.
//
// Retrieval is TAG-FIRST (external review finding: the previous FTS-only
// candidate pull made topic compounding accidentally dependent on summary
// wording — an artifact whose text never mentioned the next node's id/kind
// could never be retrieved by a later same-topic task, silently killing the
// L6 compounding loop). Exact node-id tag and every topic tag are queried
// directly; FTS over id/kind remains only as a legacy candidate source. The
// eligibility filter is unchanged either way: exact node-id tag or shared
// topic tag, accepted status, LIVE-verified before any vote. Route purity is
// untouched: this is engine-side signal gathering.
func (e *Engine) memoryHitForNode(node Node) (bool, string, []string) {
	cands := map[string]store.Artifact{}
	for _, tag := range append([]string{node.ID}, node.Tags...) {
		if tag == "" {
			continue
		}
		res, err := e.Store.SearchArtifactsTagged("", "", "", tag, 10)
		if err == nil {
			for _, a := range res {
				cands[a.ID] = a
			}
		}
	}
	for _, q := range []string{node.ID, node.Kind} {
		res, err := e.Store.SearchArtifacts(q, "", "", 10)
		if err == nil {
			for _, a := range res {
				cands[a.ID] = a
			}
		}
	}
	// Eligibility: exact node-id tag, or any shared topic tag.
	eligible := func(a store.Artifact) bool {
		if hasTag(a.Tags, node.ID) {
			return true
		}
		for _, t := range node.Tags {
			if t != "" && hasTag(a.Tags, t) {
				return true
			}
		}
		return false
	}
	bestStatus := ""
	var verifiedIDs []string
	for _, a := range cands {
		if a.Status != "accepted" {
			continue
		}
		if !eligible(a) {
			continue
		}
		// LIVE gate: verify against the repo right now, then persist.
		res := verification.Run(a, e.repo())
		// Deliberately ignored: THIS route already gates on res.Status in
		// memory; SetVerify only refreshes the cache column for later
		// sessions. A failed write costs a re-verify next time, never a
		// wrong admission now (state-machine writes elsewhere are checked).
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
	// Ignored by design: Seed is a demo helper; the seeded artifact's status
	// is re-established live at every route regardless of the cached column.
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

// execLogged runs a node's `run` command in the repo root (the real adapter)
// and ALSO writes a sidecar evidence log under WARD_HOME/logs so the run is
// auditable outside the binary db. It returns the combined output, the exit
// code, and the (os/exec) error. A best-effort sidecar write failure is logged
// but never masks the execution outcome — the check's pass/fail is what gates.
func (e *Engine) execLogged(runID, nodeID, cmd, repo string) ([]byte, int, error) {
	start := time.Now()
	c := exec.Command("sh", "-c", cmd)
	if repo != "" {
		c.Dir = repo
	}
	out, err := c.CombinedOutput()
	elapsed := time.Since(start)
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else {
			exitCode = -1
		}
	}
	// Transparency: persist what ran, regardless of success, so humans can read
	// the evidence even for green runs (and especially for red ones). The
	// sidecar IS the proof of verification: if it cannot be written, the run
	// cannot claim success. We fail the node rather than leave a "completed"
	// task with missing evidence (a zombie state that contradicts the gate).
	if _, werr := store.WriteSidecar(runID, nodeID, cmd, exitCode, elapsed, out); werr != nil {
		return out, exitCode, fmt.Errorf("verification evidence could not be written to sidecar log: %w", werr)
	}
	return out, exitCode, err
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

// preFlightBrokenCheck reports whether a failed check's output means the
// check could not EXECUTE (as opposed to executing and failing): missing test
// files, uninstalled modules, nothing collected. These waste escalation
// budget on every tier because the failure is independent of the work.
func preFlightBrokenCheck(detail string) bool {
	signatures := []string{
		"file or directory not found",
		"directory not found",
		"no such file or directory",
		"ModuleNotFoundError",
		"ImportError while importing",
		"no tests to run",
		"[setup failed]",
		"go.mod file not found",
		"command not found",
	}
	for _, sig := range signatures {
		if strings.Contains(detail, sig) {
			return true
		}
	}
	return false
}
