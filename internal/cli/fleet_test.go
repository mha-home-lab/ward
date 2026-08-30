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

// TestWaveHealSparesProposedArtifacts is the second reviewed wave bug (v0.9.7):
// the wave sweep selected by tag which excluded only 'superseded', so a PROPOSED
// (review-pending) local artifact whose gate went red was healed by wave --heal,
// pulling real candidates out of review. Wave must act only on the accepted
// knowledge surface, same as tick's sweepVerify.
func TestWaveHealSparesProposedArtifacts(t *testing.T) {
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

	// A PROPOSED local candidate under review: promoted? No — it stays
	// 'proposed' until review accepts it. Its gate is currently red, which in
	// the bug was enough for wave --heal to supersede it.
	if err := os.WriteFile(filepath.Join(dir, "fact.txt"), []byte("draft\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prop := store.Artifact{
		Kind: "solution", Summary: "candidate under review", Tags: []string{"topic:proposed"},
		Status: "proposed", CreatedBy: "agent", Local: true,
		VerifyCmd: "grep -q FINAL fact.txt", VerifyKind: "shell",
	}
	propID, err := s.UpsertArtifact(prop)
	if err != nil {
		t.Fatal(err)
	}
	// A prior review pass had verified it; the candidate is re-checked later.
	if err := s.SetVerify(propID, "verified"); err != nil {
		t.Fatal(err)
	}
	// An ACCEPTED local artifact on the same tag with a red gate must still heal.
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("gone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	acc := store.Artifact{
		Kind: "solution", Summary: "accepted fact", Tags: []string{"topic:proposed"},
		Status: "accepted", CreatedBy: "human", Local: true,
		VerifyCmd: "grep -q PRESENT other.txt", VerifyKind: "shell",
	}
	accID, err := s.UpsertArtifact(acc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Promote([]string{accID}, "auto-accept", "human"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetVerify(accID, "verified"); err != nil {
		t.Fatal(err)
	}
	s.DB.Close()

	w := waveCmd()
	if err := w.Flags().Set("heal", "true"); err != nil {
		t.Fatal(err)
	}
	w.SetArgs([]string{"topic:proposed"})
	if err := w.Execute(); err != nil {
		t.Fatal(err)
	}

	s2, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s2.DB.Close()

	pa, err := s2.GetArtifact(propID)
	if err != nil {
		t.Fatal(err)
	}
	if pa.Status != "proposed" || pa.SupersededRsn != "" || pa.VerifyStatus != "verified" {
		t.Fatalf("proposed candidate must survive wave --heal untouched, got status=%s rsn=%q verify=%s",
			pa.Status, pa.SupersededRsn, pa.VerifyStatus)
	}

	aa, err := s2.GetArtifact(accID)
	if err != nil {
		t.Fatal(err)
	}
	if aa.Status != "superseded" || aa.SupersededRsn != "wave drift" {
		t.Fatalf("accepted drift must still heal alongside a proposed sibling, got status=%s rsn=%q",
			aa.Status, aa.SupersededRsn)
	}
}
