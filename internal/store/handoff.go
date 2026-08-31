package store

import "database/sql"

// HandoffRow is one entry in handoff_log (one row per `ward memory handoff`).
type HandoffRow struct {
	ID         int64
	At         string
	HeadSHA    string
	CaptureGap bool // true when that handoff observed commits with no captures
	Commits    int  // commits counted during that handoff's gap check
}

// LastHandoff returns the most recent handoff_log row, or (nil, nil) if none
// exists yet.
func (s *Store) LastHandoff() (*HandoffRow, error) {
	row := s.DB.QueryRow(`SELECT id, at, head_sha, capture_gap, commits FROM handoff_log
		ORDER BY id DESC LIMIT 1`)
	var h HandoffRow
	var cg int
	err := row.Scan(&h.ID, &h.At, &h.HeadSHA, &cg, &h.Commits)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	h.CaptureGap = cg != 0
	return &h, nil
}

// LogHandoff appends a handoff_log row recording whether that handoff observed
// a capture gap and how many commits it counted, and returns the new row.
func (s *Store) LogHandoff(at, headSHA string, captureGap bool, commits int) (*HandoffRow, error) {
	cg := 0
	if captureGap {
		cg = 1
	}
	res, err := s.DB.Exec(`INSERT INTO handoff_log (at, head_sha, capture_gap, commits) VALUES (?, ?, ?, ?)`, at, headSHA, cg, commits)
	if err != nil {
		return nil, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, err
	}
	return &HandoffRow{ID: id, At: at, HeadSHA: headSHA, CaptureGap: captureGap, Commits: commits}, nil
}

// CountArtifactsSince returns the number of artifacts created strictly after
// the given RFC3339 timestamp.
func (s *Store) CountArtifactsSince(since string) (int, error) {
	if since == "" {
		return 0, nil
	}
	var n int
	err := s.DB.QueryRow(`SELECT COUNT(*) FROM artifacts WHERE created_at > ?`, since).Scan(&n)
	return n, err
}
