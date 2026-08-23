package store

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

// SearchArtifacts finds artifacts matching q, applying FTS5 AND with term-drop
// relaxation (retrieval-001) and status ranking (accepted before proposed).
// Unverified/stale/unknown artifacts are NOT down-ranked here — the router is
// responsible for treating them as a miss; search simply surfaces candidates.
func (s *Store) SearchArtifacts(q, kind, project string, limit int) ([]Artifact, error) {
	return s.SearchArtifactsTagged(q, kind, project, "", limit)
}

// SearchArtifactsTagged adds an exact tag filter on top of the FTS/LIKE query:
// small models write weak free-text queries; a declarative tag selector is the
// reliable surface (rd:c1).
func (s *Store) SearchArtifactsTagged(q, kind, project, tag string, limit int) ([]Artifact, error) {
	tokens := strings.Fields(q)
	if len(tokens) == 0 {
		return nil, nil
	}
	// Build relaxation tiers: full AND, then drop least-informative (shortest) token.
	tiers := [][]string{tokens}
	dropOrder := make([]int, len(tokens))
	for i := range tokens {
		dropOrder[i] = i
	}
	sort.SliceStable(dropOrder, func(i, j int) bool {
		return len(tokens[dropOrder[i]]) < len(tokens[dropOrder[j]])
	})
	for k := 1; k < len(tokens); k++ {
		dropped := map[int]bool{}
		for _, i := range dropOrder[:k] {
			dropped[i] = true
		}
		t := make([]string, 0, len(tokens)-k)
		for i, tok := range tokens {
			if !dropped[i] {
				t = append(t, tok)
			}
		}
		tiers = append(tiers, t)
	}
	if tag != "" && len(tokens) == 0 {
		return s.queryArtifacts("", kind, project, tag, limit, true)
	}
	for _, tier := range tiers {
		if len(tier) == 0 {
			continue
		}
		fts := `"` + strings.Join(tier, `" AND "`) + `"`
		rows, err := s.queryArtifacts(fts, kind, project, tag, limit, true)
		if err != nil {
			return nil, err
		}
		if len(rows) > 0 {
			return rows, nil
		}
	}
	// Final fallback: LIKE scan.
	return s.queryArtifacts("%"+strings.Join(tokens, "%")+"%", kind, project, tag, limit, false)
}

func (s *Store) queryArtifacts(match, kind, project, tag string, limit int, fts bool) ([]Artifact, error) {
	base := `SELECT id, kind, summary, content, tags, status, created_by, created_at,
		used_count, superseded_by, superseded_reason, superseded_at, promoted_at, promoted_by,
		promoted_reason, source_session, source_agent, project, verify_cmd, verify_kind,
		verify_status, verify_at, ceremony_level, expires_at, local FROM artifacts WHERE status != 'superseded'`
	args := []any{}
	if fts && match != "" {
		base += " AND rowid IN (SELECT rowid FROM artifacts_fts WHERE artifacts_fts MATCH ?)"
		args = append(args, match)
	} else if !fts {
		base += " AND (summary LIKE ? OR content LIKE ? OR tags LIKE ?)"
		args = append(args, match, match, match)
	}
	if tag != "" {
		base += ` AND EXISTS (SELECT 1 FROM json_each(artifacts.tags) jt WHERE jt.value = ?)`
		args = append(args, tag)
	}
	if kind != "" {
		base += " AND kind = ?"
		args = append(args, kind)
	}
	if project != "" {
		base += " AND project = ?"
		args = append(args, project)
	}
	base += ` ORDER BY CASE status WHEN 'accepted' THEN 0 WHEN 'proposed' THEN 1 ELSE 2 END,
		used_count DESC, created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.Query(base, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

func scanArtifact(rows *sql.Rows) (Artifact, error) {
	var a Artifact
	var tags, kind, status, ca, cer string
	var used int
	var ssn, sag, proj, vc, vk, vs, va, supBy, supRsn, supAt, promAt, promBy, promRsn, expAt sql.NullString
	err := rows.Scan(&a.ID, &kind, &a.Summary, &a.Content, &tags, &status, &a.CreatedBy, &ca,
		&used, &supBy, &supRsn, &supAt, &promAt, &promBy, &promRsn, &ssn, &sag,
		&proj, &vc, &vk, &vs, &va, &cer, &expAt, &a.Local)
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

// ListArtifacts returns artifacts filtered by status/kind/project.
func (s *Store) ListArtifacts(status, kind, project string, limit int) ([]Artifact, error) {
	base := `SELECT id, kind, summary, content, tags, status, created_by, created_at,
		used_count, superseded_by, superseded_reason, superseded_at, promoted_at, promoted_by,
		promoted_reason, source_session, source_agent, project, verify_cmd, verify_kind,
		verify_status, verify_at, ceremony_level, expires_at, local FROM artifacts WHERE 1=1`
	args := []any{}
	if status != "" {
		base += " AND status = ?"
		args = append(args, status)
	} else {
		base += " AND status != 'superseded'"
	}
	if kind != "" {
		base += " AND kind = ?"
		args = append(args, kind)
	}
	if project != "" {
		base += " AND project = ?"
		args = append(args, project)
	}
	base += ` ORDER BY CASE status WHEN 'accepted' THEN 0 WHEN 'proposed' THEN 1 ELSE 2 END,
		created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.DB.Query(base, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}

// StaleArtifacts surfaces accepted, rarely-reused artifacts (review candidates).
func (s *Store) StaleArtifacts(days, limit int) ([]Artifact, error) {
	q := `SELECT id, kind, summary, content, tags, status, created_by, created_at,
		used_count, superseded_by, superseded_reason, superseded_at, promoted_at, promoted_by,
		promoted_reason, source_session, source_agent, project, verify_cmd, verify_kind,
		verify_status, verify_at, ceremony_level, expires_at, local FROM artifacts
		WHERE status='accepted' AND used_count < 2 AND created_at <= datetime('now','-'||?||' days')
		ORDER BY created_at ASC, used_count ASC LIMIT ?`
	rows, err := s.DB.Query(q, days, limit)
	if err != nil {
		return nil, fmt.Errorf("stale: %w", err)
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, nil
}
