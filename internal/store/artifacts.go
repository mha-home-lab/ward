package store

import (
	"database/sql"
	"fmt"
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

// GetArtifact loads one artifact by id.
func (s *Store) GetArtifact(id string) (Artifact, error) {
	var a Artifact
	var tags, kind, status, ca, cer string
	var used int
	var ssn, sag, proj, vc, vk, vs, va, supBy, supRsn, supAt, promAt, promBy, promRsn, expAt sql.NullString
	err := s.DB.QueryRow(`SELECT id, kind, summary, content, tags, status, created_by,
		created_at, used_count, superseded_by, superseded_reason, superseded_at,
		promoted_at, promoted_by, promoted_reason, source_session, source_agent,
		project, verify_cmd, verify_kind, verify_status, verify_at, ceremony_level, expires_at, local
		FROM artifacts WHERE id = ?`, id).Scan(
		&a.ID, &kind, &a.Summary, &a.Content, &tags, &status, &a.CreatedBy, &ca,
		&used, &supBy, &supRsn, &supAt, &promAt, &promBy, &promRsn, &ssn, &sag,
		&proj, &vc, &vk, &vs, &va, &cer, &expAt, &a.Local)
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

// SetLocal marks an artifact as trusted (explicitly trusted import).
func (s *Store) SetLocal(id string) error {
	_, err := s.DB.Exec(`UPDATE artifacts SET local=1 WHERE id=?`, id)
	return err
}

func nullStr(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

// --- runs ---

// CreateRun inserts a new run row.
func (s *Store) CreateRun(r RunState) error {
	_, err := s.DB.Exec(`INSERT INTO runs (id, workflow_name, workflow_path, status, waiting_approval_id,
		current_item_id, ceremony_level, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)`,
		r.ID, r.WorkflowName, r.WorkflowPath, r.Status, r.WaitingApproval, r.CurrentItem, r.Ceremony,
		r.CreatedAt, r.UpdatedAt)
	return err
}

// LoadRun loads a run by id.
func (s *Store) LoadRun(id string) (RunState, error) {
	var r RunState
	var wa, ci, cer, wp sql.NullString
	err := s.DB.QueryRow(`SELECT id, workflow_name, workflow_path, status, waiting_approval_id,
		current_item_id, ceremony_level, created_at, updated_at FROM runs WHERE id=?`, id).
		Scan(&r.ID, &r.WorkflowName, &wp, &r.Status, &wa, &ci, &cer, &r.CreatedAt, &r.UpdatedAt)
	if err == sql.ErrNoRows {
		return r, fmt.Errorf("no run %s", id)
	}
	if err != nil {
		return r, err
	}
	r.WorkflowPath = nullStr(wp)
	r.WaitingApproval, r.CurrentItem, r.Ceremony = nullStr(wa), nullStr(ci), nullStr(cer)
	return r, nil
}

// SaveRun upserts run state.
func (s *Store) SaveRun(r RunState) error {
	_, err := s.DB.Exec(`INSERT INTO runs (id, workflow_name, workflow_path, status, waiting_approval_id,
		current_item_id, ceremony_level, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET status=excluded.status,
		workflow_path=excluded.workflow_path,
		waiting_approval_id=excluded.waiting_approval_id, current_item_id=excluded.current_item_id,
		ceremony_level=excluded.ceremony_level, updated_at=excluded.updated_at`,
		r.ID, r.WorkflowName, r.WorkflowPath, r.Status, r.WaitingApproval, r.CurrentItem, r.Ceremony,
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

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
