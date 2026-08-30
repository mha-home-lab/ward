package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mha-home-lab/ward/internal/store"
)

// TestWaveHealSparesImportedArtifacts is the trust-boundary regression the
// roadmap missed (filed by a reviewer, v0.9.5): wave --heal superseded every
// non-"verified" artifact, but verification.Run returns "unknown" for imported
// (Local=false) artifacts BECAUSE their verify_cmd must never execute. An
// import is not drift — wave must skip it while still healing genuine local
// drift on the SAME topic tag.
func TestWaveHealSparesImportedArtifacts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WARD_HOME", home)
	dir := t.TempDir()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(home) })

	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}

	// An imported artifact: Local=false (never live-verified here), but real
	// and verified on disk elsewhere, carrying the same topic tag.
	imp := store.Artifact{
		Kind: "solution", Summary: "imported lesson", Tags: []string{"topic:imports"},
		Status: "accepted", CreatedBy: "other", Local: false,
		VerifyCmd: "grep -q IMPORTANT file.txt", VerifyKind: "shell",
	}
	impID, err := s.UpsertArtifact(imp)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Promote([]string{impID}, "import", "other"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVerify(impID, "verified"); err != nil {
		t.Fatal(err)
	}

	// A LOCAL artifact on the same tag whose gate genuinely goes stale.
	if err := os.WriteFile(filepath.Join(dir, "fact.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	loc := store.Artifact{
		Kind: "solution", Summary: "local fact", Tags: []string{"topic:imports"},
		Status: "accepted", CreatedBy: "human", Local: true,
		VerifyCmd: "grep -q hello fact.txt", VerifyKind: "shell",
	}
	locID, err := s.UpsertArtifact(loc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Promote([]string{locID}, "auto-accept", "human"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVerify(locID, "verified"); err != nil {
		t.Fatal(err)
	}
	// The world drifts for the local fact.
	if err := os.WriteFile(filepath.Join(dir, "fact.txt"), []byte("goodbye\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()

	w := waveCmd()
	if err := w.Flags().Set("heal", "true"); err != nil {
		t.Fatal(err)
	}
	w.SetArgs([]string{"topic:imports"})
	if err := w.Execute(); err != nil {
		t.Fatal(err)
	}

	s2, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s2.DB.Close()

	ia, err := s2.GetArtifact(impID)
	if err != nil {
		t.Fatal(err)
	}
	if ia.Status == "superseded" || ia.SupersededRsn != "" || ia.VerifyStatus != "verified" {
		t.Fatalf("imported artifact must survive wave --heal untouched, got status=%s rsn=%q verify=%s",
			ia.Status, ia.SupersededRsn, ia.VerifyStatus)
	}

	la, err := s2.GetArtifact(locID)
	if err != nil {
		t.Fatal(err)
	}
	if la.Status != "superseded" || la.SupersededRsn != "wave drift" {
		t.Fatalf("genuine local drift must still heal, got status=%s rsn=%q",
			la.Status, la.SupersededRsn)
	}
}
