package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/mha-home-lab/ward/internal/store"
)

// artifactIDFromJSON pulls the "id" field out of a `ward memory put --json`
// response.
func artifactIDFromJSON(t *testing.T, out string) string {
	t.Helper()
	var v map[string]any
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		t.Fatalf("invalid put JSON: %v\n%s", err, out)
	}
	id, _ := v["id"].(string)
	if id == "" {
		t.Fatalf("no id in put JSON:\n%s", out)
	}
	return id
}

// TestRecurrencePutGetBrief exercises the whole control-recurrence-links path
// end to end: `--recurs` on `memory put` records an agent-declared link;
// `memory get --json` reports the recurrence_count; and `brief` folds a topic
// confirmed >= 2 times into the skill-sync nudge as a strong promotion
// candidate.
func TestRecurrencePutGetBrief(t *testing.T) {
	newSurfaceStore(t)
	t.Setenv("HOME", t.TempDir()) // empty global vault -> topic stays unsynced

	// Original lesson (the earlier artifact being confirmed).
	put1 := jsonRun(t, memoryPutCmd(), []string{
		"--summary", "local -i eval-order trap",
		"--content", "the mechanism: local -i shadows across the arithmetic because eval order replaces the outer value",
		"--tags", "portable:bash", "--local", "--by", "human"})
	id1 := artifactIDFromJSON(t, put1)

	// First independent confirmation (different wording, same trap).
	jsonRun(t, memoryPutCmd(), []string{
		"--summary", "local shadowing different exercise",
		"--content", "the mechanism: local -i shadows across the arithmetic because eval order replaces the outer value",
		"--tags", "portable:bash", "--local", "--recurs", id1})

	// Second independent confirmation.
	jsonRun(t, memoryPutCmd(), []string{
		"--summary", "local arithmetic third form",
		"--content", "the mechanism: local -i shadows across the arithmetic because eval order replaces the outer value",
		"--tags", "portable:bash", "--local", "--recurs", id1})

	// memory get --json surfaces the count.
	got := jsonRun(t, memoryGetCmd(), []string{id1})
	m := parseNoNull(t, got)
	if rc, ok := m["recurrence_count"].(float64); !ok || int(rc) != 2 {
		t.Fatalf("recurrence_count = %v, want 2", m["recurrence_count"])
	}

	// brief surfaces it as a strong promotion candidate.
	out := jsonRun(t, briefCmd(), nil)
	bm := parseNoNull(t, out)
	found := false
	for _, n := range bm["next"].([]any) {
		s := n.(string)
		if strings.Contains(s, "strong promotion candidate") && strings.Contains(s, "confirmed 2 times") && strings.Contains(s, "bash") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("brief must flag a twice-confirmed portable topic as a strong promotion candidate:\n%s", out)
	}
}

// TestHintIfRecurrence: signal 5 assistive autocomplete — a new portable:*
// capture whose content shares enough distinctive tokens with an existing
// artifact under the SAME topic returns a non-blocking hint suggesting
// --recurs. It fires on overlap, never links by itself, and stays silent for
// dissimilar content and for non-portable tags.
func TestHintIfRecurrence(t *testing.T) {
	newSurfaceStore(t)
	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	id1, err := s.UpsertArtifact(store.Artifact{
		Kind: "solution", Summary: "first", Tags: []string{"portable:bash"},
		Content: "the mechanism local arithmetic shadows because eval order", Status: "accepted",
	})
	if err != nil {
		t.Fatal(err)
	}

	// Similar content under the same portable topic -> hint referencing id1.
	h := hintIfRecurrence(s, []string{"portable:bash"}, "the mechanism local arithmetic shadows because eval order differently", "new:id")
	if !strings.Contains(h, "--recurs") || !strings.Contains(h, id1) {
		t.Fatalf("expected hint referencing %s, got %q", id1, h)
	}
	// Dissimilar content -> no hint (overlap below the assistive threshold).
	if h2 := hintIfRecurrence(s, []string{"portable:bash"}, "completely unrelated mechanism about filesystems and mounts", "new:id2"); h2 != "" {
		t.Fatalf("expected no hint for dissimilar content, got %q", h2)
	}
	// Non-portable tag -> no hint even with matching words.
	if h3 := hintIfRecurrence(s, []string{"topic:auth"}, "the mechanism local arithmetic shadows because eval order", "new:id3"); h3 != "" {
		t.Fatalf("expected no hint for non-portable tag, got %q", h3)
	}
}
