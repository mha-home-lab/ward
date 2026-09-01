package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// --- artifacts ---

// UpsertArtifact inserts (or ignores if id exists) an artifact. Returns the id.
func (s *Store) UpsertArtifact(a Artifact) (string, error) {
	id := idFor(a.Kind, a.Summary, a.Content)
	local := 1
	if !a.Local {
		local = 0
	}
	_, err := s.DB.Exec(`INSERT OR IGNORE INTO artifacts
		(id, kind, summary, content, tags, status, created_by, created_at,
		 source_session, source_agent, project, verify_cmd, verify_kind,
		 verify_status, ceremony_level, local)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?, 'unknown', ?, ?)`,
		id, a.Kind, a.Summary, a.Content, joinTags(a.Tags), a.Status, a.CreatedBy,
		nowISO(), a.SourceSession, a.SourceAgent, a.Project, a.VerifyCmd, a.VerifyKind,
		a.Ceremony, local)
	if err != nil {
		return "", err
	}
	return id, nil
}

// ClaimTopic atomically reserves a topic. The unique index uni_claim_topic
// (claim_topic, project) makes this correct across separate `ward` processes:
// only one active claim per (topic, project) can exist. The acquisition is a
// single plain INSERT — not a check-then-insert — so the database, not the app,
// is the arbiter. On a conflict (an active claim already blocks it) conflict is
// true; the caller treats that as a hard error (exclusive reservation). No
// partial claim is left.
func (s *Store) ClaimTopic(topic, project, by, expires string) (id string, conflict bool, err error) {
	// Normalize project to "" (never NULL) so the unique index groups all
	// "no-project" claims together under one slot.
	if strings.TrimSpace(project) == "" {
		project = ""
	}
	id = "claim:" + sha8(topic+"|"+project+"|"+by+"|"+expires+"|"+nowISO())
	tags, _ := json.Marshal([]string{"claim", topic, project})
	content := fmt.Sprintf("agent=%s expires=%s", by, expires)
	_, e := s.DB.Exec(`INSERT INTO artifacts
		(id, kind, summary, content, tags, status, created_by, created_at,
		 ceremony_level, local, claim_topic, project, expires_at)
		VALUES (?, 'claim', ?, ?, ?, 'accepted', ?, ?, 'light', 1, ?, ?, ?)`,
		id, topic, content, string(tags), by, nowISO(), topic, project, expires)
	if e != nil {
		if isUniqueViolation(e) {
			return "", true, nil
		}
		return "", false, e
	}
	return id, false, nil
}

// ReleaseClaim frees every active claim on (topic, project) so the topic can be
// re-claimed. It clears claim_topic (the unique-index slot) atomically, which is
// what actually reopens the slot — superseding alone would leave the row holding
// the topic and keep blocking re-claim.
func (s *Store) ReleaseClaim(topic, project string) (int64, error) {
	res, err := s.DB.Exec(`UPDATE artifacts
		SET status='superseded', superseded_at=?, superseded_reason='claim released', claim_topic=NULL
		WHERE claim_topic=? AND project=? AND status='accepted'`,
		nowISO(), topic, project)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ActiveClaimIDs returns the ids of active claims matching topic (empty = any)
// and project (empty = any). Because of the unique index there is at most one
// per (topic, project); this supports reporting the conflicting id on a race.
func (s *Store) ActiveClaimIDs(topic, project string) ([]string, error) {
	q := `SELECT id FROM artifacts WHERE claim_topic IS NOT NULL AND status='accepted'`
	args := []any{}
	if topic != "" {
		q += " AND claim_topic=?"
		args = append(args, topic)
	}
	if project != "" {
		q += " AND project=?"
		args = append(args, project)
	}
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func isUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// SweepExpiredClaims frees every active claim whose TTL has elapsed, so the
// topic can be re-claimed. It clears claim_topic (the unique-index slot) and
// marks the row superseded — the same cleanup ReleaseClaim does, but driven by
// expiry instead of an explicit release. Without this, an un-released expired
// claim would keep occupying its slot and block re-claim forever.
func (s *Store) SweepExpiredClaims() (int64, error) {
	res, err := s.DB.Exec(`UPDATE artifacts
		SET status='superseded', superseded_at=?, superseded_reason='expired', claim_topic=NULL
		WHERE claim_topic IS NOT NULL AND expires_at != '' AND expires_at < ?`,
		nowISO(), nowISO())
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// LegacyClaimCount returns the number of accepted claim artifacts that predate
// the v0.4 atomicity migration: they have `claim_topic IS NULL`, so the unique
// index does NOT enforce them. This is a one-time transition gap (only claims
// created before the migration), surfaced by `ward doctor` so it is visible
// rather than a silent hole in the atomicity guarantee.
func (s *Store) LegacyClaimCount() (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT count(*) FROM artifacts WHERE kind='claim' AND status='accepted' AND claim_topic IS NULL`).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// GetArtifact loads one artifact by id.
func (s *Store) GetArtifact(id string) (Artifact, error) {
	var a Artifact
	var tags, kind, status, ca, cer string
	var used int
	var ssn, sag, proj, vc, vk, vs, va, supBy, supRsn, supAt, promAt, promBy, promRsn, expAt, ovr sql.NullString
	err := s.DB.QueryRow(`SELECT id, kind, summary, content, tags, status, created_by,
		created_at, used_count, superseded_by, superseded_reason, superseded_at,
		promoted_at, promoted_by, promoted_reason, source_session, source_agent,
		project, verify_cmd, verify_kind, verify_status, verify_at, ceremony_level, expires_at, local, override_reason
		FROM artifacts WHERE id = ?`, id).Scan(
		&a.ID, &kind, &a.Summary, &a.Content, &tags, &status, &a.CreatedBy, &ca,
		&used, &supBy, &supRsn, &supAt, &promAt, &promBy, &promRsn, &ssn, &sag,
		&proj, &vc, &vk, &vs, &va, &cer, &expAt, &a.Local, &ovr)
	if err == sql.ErrNoRows {
		return a, fmt.Errorf("no artifact %s", id)
	}
	if err != nil {
		return a, err
	}
	a.Kind, a.Status, a.CreatedAt = kind, status, ca
	a.Tags = parseTags(tags)
	a.UsedCount = used
	a.SupersededBy, a.SupersededRsn, a.SupersededAt = nullStr(supBy), nullStr(supRsn), nullStr(supAt)
	a.PromotedAt, a.PromotedBy, a.PromotedRsn = nullStr(promAt), nullStr(promBy), nullStr(promRsn)
	a.SourceSession, a.SourceAgent, a.Project = nullStr(ssn), nullStr(sag), nullStr(proj)
	a.VerifyCmd, a.VerifyKind, a.VerifyStatus, a.VerifyAt = nullStr(vc), nullStr(vk), nullStr(vs), nullStr(va)
	a.Ceremony, a.ExpiresAt = cer, nullStr(expAt)
	a.OverrideReason = nullStr(ovr)
	if a.VerifyStatus == "" {
		a.VerifyStatus = "unknown"
	}
	return a, nil
}

// BumpUsed increments used_count and sets last_used for an artifact.
func (s *Store) BumpUsed(id string) error {
	_, err := s.DB.Exec(`UPDATE artifacts SET used_count = used_count + 1, last_used = ?
		WHERE id = ?`, nowISO(), id)
	return err
}

// Promote sets status=accepted for the given ids (idempotent on accepted).
func (s *Store) Promote(ids []string, reason, who string) ([]string, error) {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		a, err := s.GetArtifact(id)
		if err != nil {
			out = append(out, fmt.Sprintf("%s: no artifact", id))
			continue
		}
		if a.Status == "accepted" {
			out = append(out, fmt.Sprintf("%s: already accepted", id))
			continue
		}
		if a.Status == "superseded" {
			out = append(out, fmt.Sprintf("%s: rejected (superseded)", id))
			continue
		}
		_, err = s.DB.Exec(`UPDATE artifacts SET status='accepted', promoted_at=?, promoted_by=?, promoted_reason=? WHERE id=?`,
			nowISO(), who, reason, id)
		if err != nil {
			return out, err
		}
		out = append(out, fmt.Sprintf("%s: accepted", id))
	}
	return out, nil
}

// Supersede marks an artifact superseded, optionally with a successor id.
func (s *Store) Supersede(id, withID, reason string) error {
	a, err := s.GetArtifact(id)
	if err != nil {
		return err
	}
	if withID != "" {
		if _, err := s.GetArtifact(withID); err != nil {
			return err
		}
	}
	if a.Status == "superseded" {
		return nil
	}
	_, err = s.DB.Exec(`UPDATE artifacts SET status='superseded', superseded_by=?, superseded_reason=?, superseded_at=? WHERE id=?`,
		withID, reason, nowISO(), id)
	return err
}

// SetVerify records a verification outcome.
func (s *Store) SetVerify(id, status string) error {
	_, err := s.DB.Exec(`UPDATE artifacts SET verify_status=?, verify_at=? WHERE id=?`,
		status, nowISO(), id)
	return err
}

// SetVerifyCmd refreshes the persisted gate of an artifact. Skill install uses
// it when re-installing a chip with a (legitimately new) user-supplied gate:
// UpsertArtifact's content-derived id reuses the existing row, so the stored
// verify_cmd must be overwritten to equal the gate that produced the new
// verify_status — otherwise a later live re-verify runs the OLD gate against
// a status the NEW gate earned (gate and verdict diverge).
func (s *Store) SetVerifyCmd(id, cmd, kind string) error {
	_, err := s.DB.Exec(`UPDATE artifacts SET verify_cmd=?, verify_kind=? WHERE id=?`, cmd, kind, id)
	return err
}

// SetLocal marks an artifact as trusted (explicitly trusted import).
func (s *Store) SetLocal(id string) error {
	_, err := s.DB.Exec(`UPDATE artifacts SET local=1 WHERE id=?`, id)
	return err
}

// SetExpires sets the TTL expiry for an artifact (used by advisory claims).
func (s *Store) SetExpires(id, expiresAt string) error {
	_, err := s.DB.Exec(`UPDATE artifacts SET expires_at=? WHERE id=?`, expiresAt, id)
	return err
}

// SetOverrideReason records why a portable transferability-lint gate was
// overridden for an artifact (pack --force --reason). Stored on the artifact
// so the exception is auditable, never silent.
func (s *Store) SetOverrideReason(id, reason string) error {
	_, err := s.DB.Exec(`UPDATE artifacts SET override_reason=? WHERE id=?`, reason, id)
	return err
}

func nullStr(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

// RecordRecurrence records an agent-DECLARED recurrence link: fromID (a newer
// capture) confirms ofID (an earlier lesson) as the same trap surfaced in
// different wording. Ward never detects this — the agent asserts it via
// `--recurs`, mirroring how Supersede lets an agent declare "this replaces
// that" without ward judging content. Many-to-one: several later captures can
// confirm the same original. Both ids must exist and a link may not point an
// artifact at itself.
func (s *Store) RecordRecurrence(ofID, fromID, note string) error {
	if ofID == "" || fromID == "" {
		return fmt.Errorf("recurrence requires both of_id and from_id")
	}
	if ofID == fromID {
		return fmt.Errorf("recurrence cannot link an artifact to itself")
	}
	for _, id := range []string{ofID, fromID} {
		if err := s.ensureArtifactExists(id); err != nil {
			return err
		}
	}
	_, err := s.DB.Exec(`INSERT INTO recurrences (of_id, from_id, note, at) VALUES (?,?,?,?)`,
		ofID, fromID, note, nowISO())
	return err
}

// RecurrenceCount returns how many later captures have declared that they
// confirm the given artifact (COUNT(*) on recurrences.of_id). Zero means
// unconfirmed; >= 2 is the "independently confirmed N times" promotion signal
// the field report asked for — built on counts an agent declared, never on
// ward's opinion of the text.
func (s *Store) RecurrenceCount(id string) (int, error) {
	var n int
	err := s.DB.QueryRow(`SELECT count(*) FROM recurrences WHERE of_id = ?`, id).Scan(&n)
	return n, err
}

func (s *Store) ensureArtifactExists(id string) error {
	var one int
	err := s.DB.QueryRow(`SELECT 1 FROM artifacts WHERE id = ?`, id).Scan(&one)
	if err == sql.ErrNoRows {
		return fmt.Errorf("no artifact %s", id)
	}
	return err
}

// --- runs ---

// CreateRun inserts a new run row.
func (s *Store) CreateRun(r RunState) error {
	_, err := s.DB.Exec(`INSERT INTO runs (id, workflow_name, workflow_path, workflow_hash, status, waiting_approval_id,
		current_item_id, ceremony_level, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.WorkflowName, r.WorkflowPath, r.WorkflowHash, r.Status, r.WaitingApproval, r.CurrentItem, r.Ceremony,
		r.CreatedAt, r.UpdatedAt)
	return err
}

// LoadRun loads a run by id.
func (s *Store) LoadRun(id string) (RunState, error) {
	var r RunState
	var wa, ci, cer, wp, wh sql.NullString
	err := s.DB.QueryRow(`SELECT id, workflow_name, workflow_path, workflow_hash, status, waiting_approval_id,
		current_item_id, ceremony_level, created_at, updated_at FROM runs WHERE id=?`, id).
		Scan(&r.ID, &r.WorkflowName, &wp, &wh, &r.Status, &wa, &ci, &cer, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return r, fmt.Errorf("no run %s", id)
	}
	if err != nil {
		return r, err
	}
	r.WorkflowPath, r.WorkflowHash = nullStr(wp), nullStr(wh)
	r.WaitingApproval, r.CurrentItem, r.Ceremony = nullStr(wa), nullStr(ci), nullStr(cer)
	return r, nil
}

// LatestRun returns the most recently created run, or an error if none exist.
// Used to resolve a workflow path when a command is invoked without --workflow.
func (s *Store) LatestRun() (RunState, error) {
	var r RunState
	var wa, ci, cer, wp, wh sql.NullString
	err := s.DB.QueryRow(`SELECT id, workflow_name, workflow_path, workflow_hash, status, waiting_approval_id,
		current_item_id, ceremony_level, created_at, updated_at FROM runs
		ORDER BY created_at DESC, rowid DESC LIMIT 1`).
		Scan(&r.ID, &r.WorkflowName, &wp, &wh, &r.Status, &wa, &ci, &cer, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return r, fmt.Errorf("no runs yet")
	}
	if err != nil {
		return r, err
	}
	r.WorkflowPath, r.WorkflowHash = nullStr(wp), nullStr(wh)
	r.WaitingApproval, r.CurrentItem, r.Ceremony = nullStr(wa), nullStr(ci), nullStr(cer)
	return r, nil
}

// OpenRuns returns every run that is still in flight (running or awaiting
// approval), oldest first. Used by handoff and brief to surface unfinished work.
func (s *Store) OpenRuns() ([]RunState, error) {
	rows, err := s.DB.Query(`SELECT id, workflow_name, workflow_path, workflow_hash, status, waiting_approval_id,
		current_item_id, ceremony_level, created_at, updated_at FROM runs
		WHERE status IN ('running','awaiting_approval') ORDER BY created_at ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunState
	for rows.Next() {
		var r RunState
		var wa, ci, cer, wp, wh sql.NullString
		if err := rows.Scan(&r.ID, &r.WorkflowName, &wp, &wh, &r.Status, &wa, &ci, &cer, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.WorkflowPath, r.WorkflowHash = nullStr(wp), nullStr(wh)
		r.WaitingApproval, r.CurrentItem, r.Ceremony = nullStr(wa), nullStr(ci), nullStr(cer)
		out = append(out, r)
	}
	return out, nil
}

// ListRunIDs returns every run id (used to derive evidence state from disk).
func (s *Store) ListRunIDs() ([]string, error) {
	rows, err := s.DB.Query(`SELECT id FROM runs`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// CountRunEvidence derives backed vs pre-evidence counts from the sidecar
// logs on disk (the single source of truth). A run is "backed" iff a sidecar
// log exists for its id; runs without one are trusted pre-evidence completions
// that predate the sidecar feature. This never brands historical work as
// unproven — it only reports what is re-verifiable from a log.
func (s *Store) CountRunEvidence() (backed, preEvidence int, err error) {
	ids, err := s.ListRunIDs()
	if err != nil {
		return 0, 0, err
	}
	dir := filepath.Join(Home(), "logs")
	entries, derr := os.ReadDir(dir)
	if derr != nil {
		// No logs dir yet: every run is pre-evidence, which is fine.
		return 0, len(ids), nil
	}
	// Map run id -> has sidecar, by stripping the "<runID>_" prefix once.
	have := make(map[string]bool, len(entries))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".log") {
			continue
		}
		if i := strings.Index(name, "_"); i > 0 {
			have[name[:i]] = true
		}
	}
	for _, id := range ids {
		if have[id] {
			backed++
		} else {
			preEvidence++
		}
	}
	return backed, preEvidence, nil
}

// SaveRun upserts run state.
func (s *Store) SaveRun(r RunState) error {
	// workflow_hash is written on INSERT only: it is the run's identity at
	// birth and is never overwritten by later transitions.
	_, err := s.DB.Exec(`INSERT INTO runs (id, workflow_name, workflow_path, workflow_hash, status, waiting_approval_id,
		current_item_id, ceremony_level, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status,
		workflow_path=excluded.workflow_path,
		waiting_approval_id=excluded.waiting_approval_id, current_item_id=excluded.current_item_id,
		ceremony_level=excluded.ceremony_level, updated_at=excluded.updated_at`,
		r.ID, r.WorkflowName, r.WorkflowPath, r.WorkflowHash, r.Status, r.WaitingApproval, r.CurrentItem, r.Ceremony,
		r.CreatedAt, r.UpdatedAt)
	return err
}

// UpsertRunNode inserts or updates a node's per-run state.
func (s *Store) UpsertRunNode(n RunNode) error {
	_, err := s.DB.Exec(`INSERT INTO run_nodes (run_id, node, status, touched, ceremony_level, declared_obs, escalation, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(run_id, node) DO UPDATE SET status=excluded.status,
		touched=excluded.touched, ceremony_level=excluded.ceremony_level,
		declared_obs=excluded.declared_obs, escalation=excluded.escalation, updated_at=excluded.updated_at`,
		n.RunID, n.Node, n.Status, joinTags(n.Touched), n.Ceremony, n.DeclaredObs, n.Escalation, n.UpdatedAt)
	return err
}

// LoadRunNodes returns all nodes for a run.
func (s *Store) LoadRunNodes(runID string) ([]RunNode, error) {
	rows, err := s.DB.Query(`SELECT run_id, node, status, touched, ceremony_level, declared_obs, escalation, updated_at
		FROM run_nodes WHERE run_id=? ORDER BY node`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunNode
	for rows.Next() {
		var n RunNode
		var touched, cer, obs sql.NullString
		var esc int
		if err := rows.Scan(&n.RunID, &n.Node, &n.Status, &touched, &cer, &obs, &esc, &n.UpdatedAt); err != nil {
			return nil, err
		}
		n.Touched = parseTags(nullStr(touched))
		n.Ceremony, n.DeclaredObs = nullStr(cer), nullStr(obs)
		n.Escalation = esc
		out = append(out, n)
	}
	return out, nil
}

// AddEvent appends a run event (audit).
func (s *Store) AddEvent(runID, action, node, detail string) error {
	_, err := s.DB.Exec(`INSERT INTO run_events (run_id, at, action, node, detail)
		VALUES (?,?,?,?,?)`, runID, nowISO(), action, node, detail)
	return err
}

// AddRoutingDecision records a router decision (incl. contention_inputs for D0.2 audit).
func (s *Store) AddRoutingDecision(d RoutingDecision) error {
	_, err := s.DB.Exec(`INSERT INTO routing_decisions
		(run_id, node, tier, model, ceremony_level, memory_hit, verify_status,
		 contention, escalated_from, reason, context, contention_inputs, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		d.RunID, d.Node, d.Tier, d.Model, d.Ceremony, boolInt(d.MemoryHit), d.VerifyStatus,
		boolInt(d.Contention), d.EscalatedFrom, d.Reason, d.Context, d.ContentionJSON, d.CreatedAt)
	return err
}

// AllRoutingDecisions returns up to limit recent decisions across all runs
// (harvest telemetry; observer-only).
func (s *Store) AllRoutingDecisions(limit int) ([]RoutingDecision, error) {
	rows, err := s.DB.Query(`SELECT run_id, node, tier, model, ceremony_level, memory_hit,
		verify_status, contention, escalated_from, reason, context, contention_inputs, created_at
		FROM routing_decisions ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RoutingDecision
	for rows.Next() {
		var d RoutingDecision
		var mh, con int
		var ctx sql.NullString
		if err := rows.Scan(&d.RunID, &d.Node, &d.Tier, &d.Model, &d.Ceremony, &mh,
			&d.VerifyStatus, &con, &d.EscalatedFrom, &d.Reason, &ctx, &d.ContentionJSON, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.MemoryHit = mh != 0
		d.Contention = con != 0
		d.Context = nullStr(ctx)
		out = append(out, d)
	}
	return out, nil
}

// RoutingDecisionsForRun returns all decisions for a run (for measurement).
func (s *Store) RoutingDecisionsForRun(runID string) ([]RoutingDecision, error) {
	rows, err := s.DB.Query(`SELECT run_id, node, tier, model, ceremony_level, memory_hit,
		verify_status, contention, escalated_from, reason, context, contention_inputs, created_at
		FROM routing_decisions WHERE run_id=? ORDER BY created_at`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RoutingDecision
	for rows.Next() {
		var d RoutingDecision
		var mh, con int
		var ctx sql.NullString
		if err := rows.Scan(&d.RunID, &d.Node, &d.Tier, &d.Model, &d.Ceremony, &mh,
			&d.VerifyStatus, &con, &d.EscalatedFrom, &d.Reason, &ctx, &d.ContentionJSON, &d.CreatedAt); err != nil {
			return nil, err
		}
		d.MemoryHit = mh != 0
		d.Contention = con != 0
		d.Context = nullStr(ctx)
		out = append(out, d)
	}
	return out, nil
}

// SetRoutingSuccess stamps the final routing decision for (runID, node) with its
// execution outcome (1 success / 0 failure). Best-effort telemetry, same shape
// as the verify_status post-stamp in the engine: it refines a record already
// persisted, and a lost stamp changes no admission.
//
// NULL (never stamped) is NOT "unknown failure": it is the honest marker for a
// decision whose node carried no executed check — approval-gate pauses and pure
// passthrough (channel) nodes — plus abandoned/in-flight runs. Cheap-hit
// therefore means "cheap routing carried real checked work that succeeded",
// never "cheap routing, did nothing, counted as a hit".
func (s *Store) SetRoutingSuccess(runID, node string, success bool) error {
	v := 0
	if success {
		v = 1
	}
	_, err := s.DB.Exec(`UPDATE routing_decisions SET execution_success=?
		WHERE id=(SELECT id FROM routing_decisions
		WHERE run_id=? AND node=? ORDER BY created_at DESC, id DESC LIMIT 1)`, v, runID, node)
	return err
}

// KPIReport is the routing-control telemetry for one observation window
// (control-index P2.1): the controlled variables of the "verified memory
// enables cheaper routing" thesis.
type KPIReport struct {
	WindowFrom     string  `json:"window_from"`
	WindowTo       string  `json:"window_to"`
	Total          int     `json:"total"`
	Cheap          int     `json:"cheap"`
	CheapSuccess   int     `json:"cheap_success"`
	Escalated      int     `json:"escalated"`
	MemoryMiss     int     `json:"memory_miss"` // memory_hit=0: no verified memory carried
	VerifiedPass   int     `json:"verified_pass"`
	CheapHitRate   float64 `json:"cheap_hit_rate"`   // % of decisions that were cheap AND succeeded
	EscalationRate float64 `json:"escalation_rate"`  // % of decisions escalated from a lower tier
	VerifyPassRate float64 `json:"verify_pass_rate"` // % with verify_status verified/passed
	MissRate       float64 `json:"miss_rate"`        // % with memory_hit=0
}

// RoutingKPIs aggregates routing_decisions within a window (created_at >= since,
// "" = all history) into the control-plane KPIs.
func (s *Store) RoutingKPIs(since string) (KPIReport, error) {
	var r KPIReport
	// WindowTo is the newest decision edge, so the report is honest about what
	// it actually saw (an empty table has no "now" to claim).
	if err := s.DB.QueryRow(`SELECT COALESCE(MAX(created_at),'') FROM routing_decisions`).Scan(&r.WindowTo); err != nil {
		return r, err
	}
	q := `SELECT count(*),
		COALESCE(SUM(CASE WHEN tier='cheap' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN tier='cheap' AND COALESCE(execution_success,0)=1 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN escalated_from IS NOT NULL AND escalated_from != '' THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN memory_hit=0 THEN 1 ELSE 0 END),0),
		COALESCE(SUM(CASE WHEN verify_status IN ('verified','passed') THEN 1 ELSE 0 END),0)
		FROM routing_decisions`
	args := []any{}
	if since != "" {
		q += ` WHERE created_at >= ?`
		args = append(args, since)
		r.WindowFrom = since
	}
	err := s.DB.QueryRow(q, args...).Scan(&r.Total, &r.Cheap, &r.CheapSuccess, &r.Escalated, &r.MemoryMiss, &r.VerifiedPass)
	if err != nil {
		return r, err
	}
	if r.Total > 0 {
		r.CheapHitRate = float64(r.CheapSuccess) / float64(r.Total) * 100
		r.EscalationRate = float64(r.Escalated) / float64(r.Total) * 100
		r.VerifyPassRate = float64(r.VerifiedPass) / float64(r.Total) * 100
		r.MissRate = float64(r.MemoryMiss) / float64(r.Total) * 100
	}
	return r, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
