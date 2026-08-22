package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpsertAgentBlockCreates(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "AGENTS.md")
	action, err := upsertAgentBlock(p)
	if err != nil {
		t.Fatal(err)
	}
	if action != "created" {
		t.Fatalf("want created, got %q", action)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{"# AGENTS", docStart, "ward brief", docEnd} {
		if !strings.Contains(body, want) {
			t.Fatalf("created file missing %q:\n%s", want, body)
		}
	}
}

func TestUpsertAgentBlockPreservesUserContent(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "AGENTS.md")
	user := "# My Project\n\nCustom notes that must survive.\n"
	if err := os.WriteFile(p, []byte(user), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := upsertAgentBlock(p); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(p)
	if !strings.Contains(string(body), "Custom notes that must survive.") {
		t.Fatalf("user content lost:\n%s", body)
	}
	if !strings.Contains(string(body), docStart) {
		t.Fatal("block not injected")
	}
}

func TestUpsertAgentBlockIdempotentAndRefreshes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "AGENTS.md")
	if _, err := upsertAgentBlock(p); err != nil {
		t.Fatal(err)
	}
	// Unchanged content -> unchanged.
	action, err := upsertAgentBlock(p)
	if err != nil {
		t.Fatal(err)
	}
	if action != "unchanged" {
		t.Fatalf("second run must be unchanged, got %q", action)
	}
	// Corrupted managed region -> refreshed in place, corruption gone.
	body, _ := os.ReadFile(p)
	corrupt := strings.Replace(string(body), docEnd, "CORRUPTED<!-- /ward:protocol -->", 1)
	if err := os.WriteFile(p, []byte(corrupt), 0o644); err != nil {
		t.Fatal(err)
	}
	action, err = upsertAgentBlock(p)
	if err != nil {
		t.Fatal(err)
	}
	if action != "refreshed" {
		t.Fatalf("want refreshed, got %q", action)
	}
	body, _ = os.ReadFile(p)
	if strings.Contains(string(body), "CORRUPTED") {
		t.Fatalf("corruption survived refresh:\n%s", body)
	}
	if strings.Count(string(body), docStart) != 1 {
		t.Fatalf("exactly one block expected:\n%s", body)
	}
}

func TestUpsertAgentBlockRefusesHalfMarkers(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "AGENTS.md")
	if err := os.WriteFile(p, []byte("junk\n"+docStart+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := upsertAgentBlock(p); err == nil {
		t.Fatal("half-present markers must be refused, not guessed at")
	}
}

func TestEnsureAgentDocsOnlyUpdatesExistingExtras(t *testing.T) {
	dir := t.TempDir()
	claude := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(claude, []byte("# claude\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	written, err := ensureAgentDocs(dir)
	if err != nil {
		t.Fatal(err)
	}
	// AGENTS.md is created; CLAUDE.md is updated; GEMINI.md is NOT invented.
	if written[filepath.Join(dir, "AGENTS.md")] != "created" {
		t.Fatalf("AGENTS.md should be created: %v", written)
	}
	if written[claude] != "updated" {
		t.Fatalf("CLAUDE.md should be updated: %v", written)
	}
	if _, err := os.Stat(filepath.Join(dir, "GEMINI.md")); !os.IsNotExist(err) {
		t.Fatal("GEMINI.md must not be invented when absent")
	}
}
