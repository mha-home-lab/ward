package store

import (
	"testing"
)

// TestRecurrenceRecordAndCount: an agent-DECLARED recurrence link records that a
// later capture (fromId) confirms an earlier lesson (ofId); RecurrenceCount
// aggregates how many confirmations point at a given original. Many later
// captures can confirm the same original (many-to-one, deliberately unlike the
// 1:1 superseded_by relationship).
func TestRecurrenceRecordAndCount(t *testing.T) {
	s := openPortableTestStore(t)
	mk := func(summary, content string) string {
		id, err := s.UpsertArtifact(Artifact{Kind: "solution", Summary: summary, Content: content, Tags: []string{"portable:bash"}, Status: "accepted"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	id1 := mk("first", "the mechanism generalizes because x")
	id2 := mk("second", "the mechanism generalizes because y")
	id3 := mk("third", "the mechanism generalizes because z")

	if n, err := s.RecurrenceCount(id1); err != nil || n != 0 {
		t.Fatalf("RecurrenceCount before any link = %d err=%v, want 0", n, err)
	}
	if err := s.RecordRecurrence(id1, id2, ""); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.RecurrenceCount(id1); n != 1 {
		t.Fatalf("RecurrenceCount after one link = %d, want 1", n)
	}
	if err := s.RecordRecurrence(id1, id3, "different wording, same trap"); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.RecurrenceCount(id1); n != 2 {
		t.Fatalf("RecurrenceCount after two links = %d, want 2 (many-to-one)", n)
	}
	// A confirmations link points only at the original; the confirmer has none.
	if n, _ := s.RecurrenceCount(id2); n != 0 {
		t.Fatalf("RecurrenceCount(id2) = %d, want 0", n)
	}
	// A fresh original with no confirmations reports zero.
	id4 := mk("fourth", "the mechanism generalizes because w")
	if n, _ := s.RecurrenceCount(id4); n != 0 {
		t.Fatalf("RecurrenceCount(id4) = %d, want 0", n)
	}
}

// TestRecurrenceRejectsBadLinks: a recurrence confirms an EXISTING lesson, so it
// must refuse a self-link (an artifact cannot confirm itself) and any link whose
// endpoint does not exist. Garbage links must not pollute the count.
func TestRecurrenceRejectsBadLinks(t *testing.T) {
	s := openPortableTestStore(t)
	id, err := s.UpsertArtifact(Artifact{Kind: "solution", Summary: "x", Content: "mechanism", Tags: []string{"portable:bash"}, Status: "accepted"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.RecordRecurrence(id, id, ""); err == nil {
		t.Fatal("self-link must be rejected")
	}
	if err := s.RecordRecurrence(id, "no:such", ""); err == nil {
		t.Fatal("link to a missing from_id must be rejected")
	}
	if err := s.RecordRecurrence("no:such", id, ""); err == nil {
		t.Fatal("link from a missing of_id must be rejected")
	}
}
