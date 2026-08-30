package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

// TestSkillInstallGlobalLocalizesAndVerifies proves the feedforward loop
// closes (control-skill-localize): a global chip installs as ONE local
// artifact; a passing user-supplied gate marks it verified (votes cheap), a
// failing gate leaves status=error and the command errors explicitly.
func TestSkillInstallGlobalLocalizesAndVerifies(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WARD_HOME", home)
	t.Chdir(t.TempDir())

	// A fake global chip on disk, exactly as skill-sync would have written it.
	chip := chipNameFor("control-antiwindup")
	global := filepath.Join(t.TempDir(), "skills")
	chipDir := filepath.Join(global, chip)
	if err := os.MkdirAll(chipDir, 0o755); err != nil {
		t.Fatal(err)
	}
	chipBody := "# " + chip + "\n\ngate cleanup lesson body\n"
	if err := os.WriteFile(filepath.Join(chipDir, "SKILL.md"), []byte(chipBody), 0o644); err != nil {
		t.Fatal(err)
	}

	// A real gate that reads the repo state (not a no-op placeholder).
	if err := os.WriteFile("gate.txt", []byte("OK"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := skillInstallCmd()
	if err := inst.Flags().Set("dir", global); err != nil {
		t.Fatal(err)
	}
	if err := inst.Flags().Set("verify-cmd", "grep -q OK gate.txt"); err != nil {
		t.Fatal(err)
	}
	inst.SetArgs([]string{"portable:control-antiwindup"})
	if err := inst.Execute(); err != nil {
		t.Fatal(err)
	}

	// Exactly one accepted, store-local, VERIFIED artifact for the topic.
	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	acc, _ := s.SearchArtifactsTagged("", "", "", "topic:portable:control-antiwindup", 10)
	if len(acc) != 1 {
		t.Fatalf("expected exactly one localized artifact, got %d", len(acc))
	}
	a := acc[0]
	if !a.Local || a.VerifyStatus != "verified" || a.VerifyKind != "shell" {
		t.Fatalf("localized artifact must be local+verified+shell, got %+v", a)
	}
	if !strings.Contains(a.Content, "gate cleanup lesson body") {
		t.Fatalf("artifact must carry the chip body, got: %q", a.Content)
	}

	// list-global sees the chip.
	lg := skillListGlobalCmd()
	if err := lg.Flags().Set("dir", global); err != nil {
		t.Fatal(err)
	}
	out := captureOutput(t, lg)
	if !strings.Contains(out, chip) {
		t.Fatalf("list-global must list the seeded chip, got:\n%s", out)
	}
}

// TestSkillInstallFailingGateErrorsButKeepsArtifact: a gate that fails must
// NOT become a verified hit, yet the artifact stays for later repair.
func TestSkillInstallFailingGateErrorsButKeepsArtifact(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WARD_HOME", home)
	t.Chdir(t.TempDir())

	chip := chipNameFor("metrics-attribution")
	global := filepath.Join(t.TempDir(), "skills")
	if err := os.MkdirAll(filepath.Join(global, chip), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(global, chip, "SKILL.md"), []byte("# chip\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	inst := skillInstallCmd()
	if err := inst.Flags().Set("dir", global); err != nil {
		t.Fatal(err)
	}
	if err := inst.Flags().Set("verify-cmd", "grep -q MISSING nope.txt"); err != nil {
		t.Fatal(err)
	}
	inst.SetArgs([]string{"portable:metrics-attribution"})
	if err := inst.Execute(); err == nil {
		t.Fatal("a failing gate must error explicitly")
	}

	s, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	bad, _ := s.SearchArtifactsTagged("", "", "", "topic:portable:metrics-attribution", 10)
	if len(bad) != 1 || bad[0].VerifyStatus != "error" {
		t.Fatalf("failed install must keep an error artifact, got %+v", bad)
	}
}

// TestSkillInstallMissingVerifyCmdIsRejected: no gate = phantom success.
func TestSkillInstallMissingVerifyCmdIsRejected(t *testing.T) {
	t.Setenv("WARD_HOME", t.TempDir())
	t.Chdir(t.TempDir())
	inst := skillInstallCmd()
	if err := inst.Flags().Set("dir", filepath.Join(t.TempDir(), "no-such-dir")); err != nil {
		t.Fatal(err)
	}
	inst.SetArgs([]string{"portable:nothing"})
	if err := inst.Execute(); err == nil || !strings.Contains(err.Error(), "verify-cmd") {
		t.Fatalf("install without --verify-cmd must be rejected, got %v", err)
	}
}

// captureOutput runs cmd capturing printLine/os.Stdout writes.
func captureOutput(t *testing.T, cmd *cobra.Command) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	cmd.SetArgs(nil)
	err = cmd.Execute()
	os.Stdout = old
	w.Close()
	b, _ := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
