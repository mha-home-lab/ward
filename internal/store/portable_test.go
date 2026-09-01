package store

import (
	"os"
	"testing"
)

func openPortableTestStore(t *testing.T) *Store {
	t.Helper()
	home := t.TempDir()
	os.Setenv("WARD_HOME", home)
	t.Cleanup(func() { os.Unsetenv("WARD_HOME") })
	s, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.DB.Close() })
	return s
}

func seedTags(t *testing.T, s *Store, tags ...string) {
	t.Helper()
	for _, tag := range tags {
		if _, err := s.UpsertArtifact(Artifact{
			Kind: "solution", Summary: "lesson " + tag, Content: "the mechanism generalizes: x applies to any y",
			Tags: []string{tag}, Status: "accepted",
		}); err != nil {
			t.Fatal(err)
		}
	}
}

// TestPortableTopicsBothSpellings: PortableTopics must return the DISTINCT topic
// NAME (the part after `portable:`) for both the `portable:<name>` and
// `topic:portable:<name>` conventions, deduped, never the raw tag. This is the
// fix for the silent-skip: a topic:-prefixed portable tag was invisible to the
// strict-prefix version, so its knowledge never reached the global vault.
func TestPortableTopicsBothSpellings(t *testing.T) {
	s := openPortableTestStore(t)
	seedTags(t, s, "portable:bash", "topic:portable:bash", "topic:portable:ops", "portable:trust")
	topics, err := s.PortableTopics()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"bash": true, "ops": true, "trust": true}
	if len(topics) != len(want) {
		t.Fatalf("PortableTopics = %v, want %d distinct topics", topics, len(want))
	}
	for _, tp := range topics {
		if !want[tp] {
			t.Fatalf("PortableTopics = %v, unexpected topic %q", topics, tp)
		}
	}
}

// TestArtifactsForPortableTopic: the sync/pack pipeline resolves source
// artifacts by stripped topic name; the helper must match BOTH tag spellings,
// so knowledge tagged topic:portable:bash is compiled just like portable:bash.
func TestArtifactsForPortableTopic(t *testing.T) {
	s := openPortableTestStore(t)
	seedTags(t, s, "portable:bash", "topic:portable:bash", "portable:ops")
	got, err := s.ArtifactsForPortableTopic("bash")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("ArtifactsForPortableTopic(\"bash\") = %d sources, want 2", len(got))
	}
	if n, err := s.ArtifactsForPortableTopic("ops"); err != nil || len(n) != 1 {
		t.Fatalf("ArtifactsForPortableTopic(\"ops\") = %d sources err=%v, want 1", len(n), err)
	}
	if n, err := s.ArtifactsForPortableTopic("missing"); err != nil || len(n) != 0 {
		t.Fatalf("ArtifactsForPortableTopic(\"missing\") = %d sources err=%v, want 0", len(n), err)
	}
}
