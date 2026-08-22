package store

import (
	"database/sql"
	"fmt"
)

// Task is one claimable unit of work in the dispatch pool. It is the bridge
// between "a pile of tickets" and the claim lock: a task is claimed atomically,
// admits only agents whose budget covers its tier floor, and re-enters the
// pool at a higher floor when work fails (cross-process escalation).
type Task struct {
	ID           string
	Title        string
	Kind         string // workflow node kind: default|test|channel|approval
	TierFloor    string // cheap|mid|strong — minimum capable tier (admission floor)
	TierRank     int    // 0..2 mirror of TierFloor for SQL comparison
	Status       string // open|claimed|done|rejected
	ClaimedBy    string
	ClaimedAt    string
	WorkflowPath string
	VerifyCmd    string
	Run          string
	Escalation   int
	CreatedAt    string
	UpdatedAt    string
}

// CreateTask inserts a new open task.
func (s *Store) CreateTask(t Task) (string, error) {
	if t.ID == "" {
		t.ID = "task-" + SHA8(t.Title+nowISO())
	}
	now := nowISO()
	rank := tierRank(t.TierFloor)
	_, err := s.DB.Exec(`INSERT INTO tasks
		(id, title, kind, tier_floor, tier_rank, status, verify_cmd, run, escalation, created_at, updated_at)
		VALUES (?,?,?,?,?, 'open', ?, ?, 0, ?, ?)`,
		t.ID, t.Title, t.Kind, t.TierFloor, rank, t.VerifyCmd, t.Run, now, now)
	if err != nil {
		return "", err
	}
	return t.ID, nil
}

// GetTask loads one task by id.
func (s *Store) GetTask(id string) (Task, error) {
	var t Task
	var cb, ca, wp, vc, rn sql.NullString
	err := s.DB.QueryRow(`SELECT id, title, kind, tier_floor, tier_rank, status,
		claimed_by, claimed_at, workflow_path, verify_cmd, run, escalation, created_at, updated_at
		FROM tasks WHERE id=?`, id).
		Scan(&t.ID, &t.Title, &t.Kind, &t.TierFloor, &t.TierRank, &t.Status,
			&cb, &ca, &wp, &vc, &rn, &t.Escalation, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return t, fmt.Errorf("no task %s", id)
	}
	if err != nil {
		return t, err
	}
	t.ClaimedBy, t.ClaimedAt, t.WorkflowPath = nullStr(cb), nullStr(ca), nullStr(wp)
	t.VerifyCmd, t.Run = nullStr(vc), nullStr(rn)
	return t, nil
}

// ListTasks returns tasks ordered oldest-first, filtered by status when set.
func (s *Store) ListTasks(status string, limit int) ([]Task, error) {
	q := `SELECT id, title, kind, tier_floor, tier_rank, status,
		claimed_by, claimed_at, workflow_path, verify_cmd, run, escalation, created_at, updated_at
		FROM tasks`
	var args []any
	if status != "" {
		q += " WHERE status=?"
		args = append(args, status)
	}
	q += " ORDER BY created_at ASC LIMIT ?"
	args = append(args, limit)
	rows, err := s.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		var t Task
		var cb, ca, wp, vc, rn sql.NullString
		if err := rows.Scan(&t.ID, &t.Title, &t.Kind, &t.TierFloor, &t.TierRank, &t.Status,
			&cb, &ca, &wp, &vc, &rn, &t.Escalation, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.ClaimedBy, t.ClaimedAt, t.WorkflowPath = nullStr(cb), nullStr(ca), nullStr(wp)
		t.VerifyCmd, t.Run = nullStr(vc), nullStr(rn)
		out = append(out, t)
	}
	return out, nil
}

// ClaimNextTask atomically pulls the highest-floor open task whose tier floor
// fits within maxTier ("", "cheap", "mid", "strong"). Admission is the agent's
// budget check: a mid-budget agent never receives a strong item. The claim is
// a conditional UPDATE on a row still in 'open' state, so two concurrent
// `ward task next` processes can never win the same task — RowsAffected is the
// arbiter, same spirit as the unique-index claim lock.
func (s *Store) ClaimNextTask(by, maxTier string) (Task, bool, error) {
	max := 2
	switch maxTier {
	case "":
		max = 2
	case "cheap":
		max = 0
	case "mid":
		max = 1
	case "strong":
		max = 2
	default:
		return Task{}, false, fmt.Errorf("invalid --max-tier %q (cheap|mid|strong)", maxTier)
	}
	cands, err := s.DB.Query(`SELECT id FROM tasks WHERE status='open' AND tier_rank <= ?
		ORDER BY tier_rank DESC, created_at ASC`, max)
	if err != nil {
		return Task{}, false, err
	}
	var ids []string
	for cands.Next() {
		var id string
		if err := cands.Scan(&id); err != nil {
			cands.Close()
			return Task{}, false, err
		}
		ids = append(ids, id)
	}
	cands.Close()
	for _, id := range ids {
		res, err := s.DB.Exec(`UPDATE tasks SET status='claimed', claimed_by=?, claimed_at=?, updated_at=?
			WHERE id=? AND status='open'`, by, nowISO(), nowISO(), id)
		if err != nil {
			return Task{}, false, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			t, err := s.GetTask(id)
			return t, true, err
		}
		// Another process claimed it between SELECT and UPDATE; try next.
	}
	return Task{}, false, nil
}

// CompleteTask marks a claimed task done.
func (s *Store) CompleteTask(id, by string) error {
	res, err := s.DB.Exec(`UPDATE tasks SET status='done', updated_at=? WHERE id=? AND status='claimed'`,
		nowISO(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("task %s is not claimed", id)
	}
	return nil
}

// FailTask releases a claimed task back into the pool with its admission floor
// bumped one tier (cross-process escalation). Past strong there is no higher
// tier: the task is rejected for a human, never looped.
func (s *Store) FailTask(id string) (Task, error) {
	t, err := s.GetTask(id)
	if err != nil {
		return t, err
	}
	newRank := t.TierRank + 1
	newStatus := "open"
	floor := tierFromRank(newRank)
	if newRank > 2 {
		newRank = 2
		newStatus = "rejected"
		floor = "strong"
	}
	_, err = s.DB.Exec(`UPDATE tasks SET status=?, tier_floor=?, tier_rank=?, claimed_by=NULL,
		claimed_at=NULL, escalation=escalation+1, updated_at=? WHERE id=?`,
		newStatus, floor, newRank, nowISO(), id)
	if err != nil {
		return t, err
	}
	return s.GetTask(id)
}

// SetTaskWorkflow records which generated workflow file serves this task.
func (s *Store) SetTaskWorkflow(id, path string) error {
	_, err := s.DB.Exec(`UPDATE tasks SET workflow_path=?, updated_at=? WHERE id=?`, path, nowISO(), id)
	return err
}

func tierRank(tier string) int {
	switch tier {
	case "cheap":
		return 0
	case "strong":
		return 2
	default:
		return 1
	}
}

func tierFromRank(r int) string {
	switch r {
	case 0:
		return "cheap"
	case 2:
		return "strong"
	default:
		return "mid"
	}
}

// LoadEvents returns a run's audit events, oldest first. This is the raw
// material for explain and the reject dossier: evidence already collected,
// never re-derived.
func (s *Store) LoadEvents(runID string) ([]RunEvent, error) {
	rows, err := s.DB.Query(`SELECT id, run_id, at, action, node, detail FROM run_events
		WHERE run_id=? ORDER BY id ASC`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunEvent
	for rows.Next() {
		var e RunEvent
		var node sql.NullString
		if err := rows.Scan(&e.Seq, &e.RunID, &e.At, &e.Action, &node, &e.Detail); err != nil {
			return nil, err
		}
		e.Node = nullStr(node)
		out = append(out, e)
	}
	return out, nil
}

// RunEvent is one audit entry in a run's history.
type RunEvent struct {
	Seq    int64
	RunID  string
	At     string
	Action string
	Node   string
	Detail string
}
