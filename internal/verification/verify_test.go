package verification

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/mha-home-lab/ward/internal/store"
)

func TestRunLocalVsImported(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("HELLO world\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	local := store.Artifact{Kind: "solution", Local: true, VerifyKind: "grep", VerifyCmd: "HELLO::f.txt"}
	got := Run(local, dir)
	if got.Status != "verified" {
		t.Fatalf("local grep pass: got %s (%s)", got.Status, got.Detail)
	}

	localFail := store.Artifact{Kind: "solution", Local: true, VerifyKind: "grep", VerifyCmd: "NOPE::f.txt"}
	got = Run(localFail, dir)
	if got.Status != "error" {
		t.Fatalf("local grep fail: got %s (%s)", got.Status, got.Detail)
	}

	// Imported artifacts must NEVER execute verify_cmd.
	imported := store.Artifact{Kind: "solution", Local: false, VerifyKind: "grep", VerifyCmd: "HELLO::f.txt"}
	got = Run(imported, dir)
	if got.Status != "unknown" {
		t.Fatalf("imported should be unknown, got %s (%s)", got.Status, got.Detail)
	}

	// hash requires a stored baseline; drift is detected.
	sum := sha256Sum(t, filepath.Join(dir, "f.txt"))
	hashArt := store.Artifact{Kind: "config", Local: true, VerifyKind: "hash",
		VerifyCmd: "sha256::f.txt", Content: sum + "\n"}
	got = Run(hashArt, dir)
	if got.Status != "verified" {
		t.Fatalf("hash baseline match: got %s (%s)", got.Status, got.Detail)
	}
	hashDrift := store.Artifact{Kind: "config", Local: true, VerifyKind: "hash",
		VerifyStatus: "verified", VerifyCmd: "sha256::f.txt", Content: "deadbeef\n"}
	got = Run(hashDrift, dir)
	if got.Status != "stale" {
		t.Fatalf("hash drift: got %s (%s)", got.Status, got.Detail)
	}
}

func TestRunShell(t *testing.T) {
	dir := t.TempDir()
	ok := store.Artifact{Kind: "solution", Local: true, VerifyKind: "shell", VerifyCmd: "true"}
	if got := Run(ok, dir); got.Status != "verified" {
		t.Fatalf("shell true: got %s", got.Status)
	}
	bad := store.Artifact{Kind: "solution", Local: true, VerifyKind: "shell", VerifyCmd: "false"}
	if got := Run(bad, dir); got.Status != "error" {
		t.Fatalf("shell false: got %s", got.Status)
	}
}

func sha256Sum(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
