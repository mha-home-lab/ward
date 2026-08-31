package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mha-home-lab/ward/internal/orchestration"
	"github.com/mha-home-lab/ward/internal/store"
)

// seedPortableArtifact inserts an accepted artifact tagged portable:bash with
// the given content, returning its id.
func seedPortableArtifact(t *testing.T, content string) string {
	t.Helper()
	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	id, err := s.UpsertArtifact(store.Artifact{
		Kind:       "solution",
		Summary:    "bash portable lesson",
		Content:    content,
		Tags:       []string{"portable:bash"},
		Status:     "accepted",
		CreatedBy:  "test",
		Local:      true,
		VerifyCmd:  "grep -q OK gate.txt",
		VerifyKind: "grep",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetVerify(id, "verified"); err != nil {
		t.Fatal(err)
	}
	return id
}

// TestPackExcludesCheatSheetSource: a portable pack whose only source is an
// instance-specific cheat-sheet must veto the bundle (empty after the gate) and
// surface the failure — never silently sync a cheat-sheet to the global vault.
func TestPackExcludesCheatSheetSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WARD_HOME", home)
	t.Setenv("HOME", t.TempDir()) // global skills dir under a temp home
	chdirTemp(t)
	if err := os.WriteFile("gate.txt", []byte("OK"), 0o644); err != nil {
		t.Fatal(err)
	}
	seedPortableArtifact(t, "collatz prints exactly 'Error: Only positive integers are allowed'")

	out := filepath.Join(t.TempDir(), "skills", "ward-bash")
	pack := skillPackCmd()
	if err := pack.Flags().Set("tag", "portable:bash"); err != nil {
		t.Fatal(err)
	}
	if err := pack.Flags().Set("out", out); err != nil {
		t.Fatal(err)
	}
	pack.SetArgs([]string{"portable:bash"})
	if err := pack.Execute(); err == nil || !strings.Contains(err.Error(), "vetoed") {
		t.Fatalf("pack with only a cheat-sheet source must veto, got %v", err)
	}
}

// TestPackForceRecordsReason: --force --reason includes the cheat-sheet source
// anyway AND records the reason on the artifact, so the exception is auditable.
func TestPackForceRecordsReason(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WARD_HOME", home)
	t.Setenv("HOME", t.TempDir())
	chdirTemp(t)
	if err := os.WriteFile("gate.txt", []byte("OK"), 0o644); err != nil {
		t.Fatal(err)
	}
	id := seedPortableArtifact(t, "collatz prints exactly 'Only positive integers are allowed'")

	out := filepath.Join(t.TempDir(), "skills", "ward-bash")
	pack := skillPackCmd()
	if err := pack.Flags().Set("tag", "portable:bash"); err != nil {
		t.Fatal(err)
	}
	if err := pack.Flags().Set("out", out); err != nil {
		t.Fatal(err)
	}
	if err := pack.Flags().Set("force", "true"); err != nil {
		t.Fatal(err)
	}
	if err := pack.Flags().Set("reason", "shipping a known-good one-off for reference"); err != nil {
		t.Fatal(err)
	}
	pack.SetArgs([]string{"portable:bash"})
	if err := pack.Execute(); err != nil {
		t.Fatalf("--force --reason must bypass the veto, got %v", err)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("chip must be written under force: %v", err)
	}

	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	a, err := s.GetArtifact(id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.OverrideReason, "shipping a known-good") {
		t.Fatalf("override reason must be recorded on the artifact, got %q", a.OverrideReason)
	}
}

// TestPackForceWithoutReasonIsRejected: --force with no --reason is exactly the
// silent exception the gate exists to prevent.
func TestPackForceWithoutReasonIsRejected(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WARD_HOME", home)
	chdirTemp(t)
	seedPortableArtifact(t, "collatz prints exactly 'Error'")

	pack := skillPackCmd()
	if err := pack.Flags().Set("tag", "portable:bash"); err != nil {
		t.Fatal(err)
	}
	if err := pack.Flags().Set("out", filepath.Join(t.TempDir(), "skills", "ward-bash")); err != nil {
		t.Fatal(err)
	}
	if err := pack.Flags().Set("force", "true"); err != nil {
		t.Fatal(err)
	}
	pack.SetArgs([]string{"portable:bash"})
	if err := pack.Execute(); err == nil || !strings.Contains(err.Error(), "--reason") {
		t.Fatalf("--force without --reason must be rejected, got %v", err)
	}
}

// skillLintCmd resolves a chip on disk back to its sources. Build one rendered
// chip referencing a current cheat-sheet source and confirm lint flags it with
// non-zero exit and per-source signal detail under --why.
func TestSkillLintFlagsCheatSheetSource(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WARD_HOME", home)
	chdirTemp(t)

	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	good := store.Artifact{Kind: "solution", Summary: "bash lesson", Content: "the lesson is the positive-mod idiom; because the mechanism generalizes", Tags: []string{"portable:bash"}, Status: "accepted", Local: true}
	bad := store.Artifact{Kind: "solution", Summary: "collatz", Content: "collatz prints exactly 'Error: Only positive integers are allowed'", Tags: []string{"portable:bash"}, Status: "accepted", Local: true}
	goodID, err := s.UpsertArtifact(good)
	if err != nil {
		t.Fatal(err)
	}
	badID, err := s.UpsertArtifact(bad)
	if err != nil {
		t.Fatal(err)
	}
	good.ID, bad.ID = goodID, badID
	for _, id := range []string{goodID, badID} {
		if err := s.SetVerify(id, "verified"); err != nil {
			t.Fatal(err)
		}
	}
	s.DB.Close()

	// Render a chip body referencing both sources, write it to disk.
	chip := chipNameFor("bash")
	dir := filepath.Join(home, "chips", chip)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := renderChip(chip, "portable:bash", home, []store.Artifact{good, bad})
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// --why emits per-source signals; exit is non-zero for the cheat-sheet.
	lint := skillLintCmd()
	if err := lint.Flags().Set("why", "true"); err != nil {
		t.Fatal(err)
	}
	lint.SetArgs([]string{dir})
	err = lint.Execute()
	if err == nil || !strings.Contains(err.Error(), "cheat-sheets") {
		t.Fatalf("lint must flag a cheat-sheet source with non-zero exit, got %v", err)
	}
}

// TestSyncSkipsCheatSheetTopic: skill-sync writes to the global vault, so it is
// a hard-gate point (spec decisions). A portable topic whose ONLY source scores
// as an instance-specific cheat-sheet must NOT be written to disk and must be
// reported as skipped in the JSON report — the same leak pack guards against.
func TestSyncSkipsCheatSheetTopic(t *testing.T) {
	dir := newSurfaceStore(t)
	putLocalFact(t, dir, "collatz prints exactly 'Error: Only positive integers are allowed'", "collatz", "portable:bash")

	target := filepath.Join(t.TempDir(), "skills")
	out := jsonRun(t, syncCmd(), []string{"--dir", target})
	m := parseNoNull(t, out)
	synced := m["synced"].([]any)
	if len(synced) != 1 {
		t.Fatalf("one portable topic must be reported, got %d: %s", len(synced), out)
	}
	row := synced[0].(map[string]any)
	if row["skipped"] == nil {
		t.Fatalf("cheat-sheet topic must be skipped, got: %v", row)
	}
	if _, err := os.Stat(filepath.Join(target, "ward-bash", "SKILL.md")); err == nil {
		t.Fatalf("cheat-sheet chip must NOT be written to the global vault")
	}
}

// TestSyncForceIncludesCheatSheetWithReason mirrors pack --force: the sync gate
// has the same logged escape hatch, so a specific cheat-sheet source can still
// be synced, with the reason recorded on the artifact.
func TestSyncForceIncludesCheatSheetWithReason(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WARD_HOME", home)
	t.Chdir(t.TempDir())

	dir := filepath.Join(home, "skills")
	id := seedPortableArtifact(t, "collatz prints exactly 'Only positive integers are allowed'")

	sync := syncCmd()
	for k, v := range map[string]string{
		"dir": dir, "force": "true",
		"reason": "known-good reference, ship to global vault",
	} {
		if err := sync.Flags().Set(k, v); err != nil {
			t.Fatal(err)
		}
	}
	sync.SetArgs(nil)
	if err := sync.Execute(); err != nil {
		t.Fatalf("--force --reason must bypass the sync veto, got %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "ward-bash", "SKILL.md")); err != nil {
		t.Fatalf("forced chip must be written, got %v", err)
	}

	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	a, err := s.GetArtifact(id)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.OverrideReason, "ship to global vault") {
		t.Fatalf("sync force must record the override reason on the artifact, got %q", a.OverrideReason)
	}
}

func chdirTemp(t *testing.T) {
	t.Helper()
	t.Chdir(t.TempDir())
}

// TestCaptureWarnsButDoesNotBlock: a portable:* capture whose content reads like
// a cheat-sheet emits a non-fatal stderr warning with the fired signals, but the
// artifact IS still created with accepted status — capture warns, pack blocks.
func TestCaptureWarnsButDoesNotBlock(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WARD_HOME", home)
	t.Chdir(t.TempDir())

	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()

	wf := &orchestration.Workflow{Name: "t", Nodes: []orchestration.Node{
		{ID: "collatz", Kind: "channel", Run: "go test ./..."},
	}}
	buf, restore := captureStderr()
	id, capErr := captureNode(s, wf, wf.Nodes[0], "portable:bash", "", "", "", "collatz prints exactly 'Error: Only positive integers are allowed'")
	restore()
	stderr := buf.String()
	if capErr != nil {
		t.Fatal(capErr)
	}
	if !strings.Contains(stderr, "instance-specific") || !strings.Contains(stderr, "verbatim-output phrasing") {
		t.Fatalf("capture must warn with fired signals on stderr, got:\n%s", stderr)
	}

	// The artifact exists and is accepted — the warning did NOT block the write.
	a, err := s.GetArtifact(id)
	if err != nil {
		t.Fatal(err)
	}
	if a.Status != "accepted" {
		t.Fatalf("capture must not change artifact status on a portable warning, got %q", a.Status)
	}
}

// TestMemoryPutLint: the manual path (ward memory put) fires the SAME shared
// transferability lint as captureNode, so a portable:* put with cheat-sheet
// content warns on stderr instead of silently bypassing the automatic path.
func TestMemoryPutLint(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WARD_HOME", home)
	chdirTemp(t)

	cmd := memoryPutCmd()
	setPutFlags(cmd, t, "x", "true", "shell", "agent", "true")
	if err := cmd.Flags().Set("tags", "portable:test"); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Flags().Set("content", "collatz prints exactly 'Error'"); err != nil {
		t.Fatal(err)
	}
	buf, restore := captureStderr()
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	restore()
	if !strings.Contains(buf.String(), "instance-specific") {
		t.Fatalf("memory put must fire the shared transferability lint on stderr, got:\n%s", buf.String())
	}

	// The lint is warn-only: the artifact is still stored (accepted, light).
	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	all, _ := s.ListArtifacts("accepted", "", "", 10)
	if len(all) != 1 {
		t.Fatalf("expected 1 accepted artifact, got %d", len(all))
	}
}

// TestWarnIfCheatSheetSilentWhenNotPortable: the shared helper stays silent for
// non-portable tags (no lint fires on ordinary captures).
func TestWarnIfCheatSheetSilentWhenNotPortable(t *testing.T) {
	buf, restore := captureStderr()
	warnIfCheatSheet([]string{"topic:auth"}, "x", "collatz prints exactly 'Error'")
	restore()
	if buf.String() != "" {
		t.Fatalf("expected no output for non-portable tags, got:\n%s", buf.String())
	}
}

// TestWarnIfCheatSheetFirstPortableOnly: only the first portable:* tag is
// scored, matching the prior captureNode behavior.
func TestWarnIfCheatSheetFirstPortableOnly(t *testing.T) {
	buf, restore := captureStderr()
	warnIfCheatSheet([]string{"portable:one", "portable:two"}, "x", "collatz prints exactly 'Error'")
	restore()
	if !strings.Contains(buf.String(), "portable:one") {
		t.Fatalf("expected first portable tag to be scored, got:\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "portable:two") {
		t.Fatalf("expected only the first portable tag to be scored, got:\n%s", buf.String())
	}
}
