package store

import (
	"os"
	"testing"
)

func TestHandoffLog(t *testing.T) {
	home := t.TempDir()
	os.Setenv("WARD_HOME", home)
	t.Cleanup(func() { os.Unsetenv("WARD_HOME") })

	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()

	// No rows yet.
	row, err := s.LastHandoff()
	if err != nil {
		t.Fatal(err)
	}
	if row != nil {
		t.Fatalf("expected nil last handoff, got %+v", row)
	}

	// Log two handoffs; last is the latest. Second one flags a capture gap.
	if _, err := s.LogHandoff("2026-09-01T00:00:00Z", "abc123", false, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.LogHandoff("2026-09-01T01:00:00Z", "def456", true, 3); err != nil {
		t.Fatal(err)
	}
	last, err := s.LastHandoff()
	if err != nil {
		t.Fatal(err)
	}
	if last.At != "2026-09-01T01:00:00Z" || last.HeadSHA != "def456" {
		t.Fatalf("LastHandoff must return the most recent row, got %+v", last)
	}
	if last.ID <= 1 {
		t.Fatalf("expected increasing id, got %d", last.ID)
	}
	if !last.CaptureGap || last.Commits != 3 {
		t.Fatalf("handoff flags and commit count must persist, got %+v", last)
	}
}

func TestCountArtifactsSince(t *testing.T) {
	home := t.TempDir()
	os.Setenv("WARD_HOME", home)
	t.Cleanup(func() { os.Unsetenv("WARD_HOME") })

	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()

	now := NowISO()
	if n, err := s.CountArtifactsSince(now); err != nil || n != 0 {
		t.Fatalf("expected 0 artifacts after now, got %d err=%v", n, err)
	}
	// Empty "since" is a no-op (never counts).
	if n, _ := s.CountArtifactsSince(""); n != 0 {
		t.Fatalf("expected 0 with empty since, got %d", n)
	}

	if _, err := s.UpsertArtifact(Artifact{Kind: "context", Summary: "s", Content: "c", Status: "accepted"}); err != nil {
		t.Fatal(err)
	}
	// Use a clearly-past reference (created_at has 1s resolution, so a "now"
	// captured in the same second as the insert would not be strictly greater).
	if n, _ := s.CountArtifactsSince("2000-01-01T00:00:00Z"); n < 1 {
		t.Fatalf("expected >=1 artifact since year 2000, got %d", n)
	}
}
