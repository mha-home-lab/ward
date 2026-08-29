package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRegistryRoundTrip(t *testing.T) {
	// Isolate the registry file so we don't clobber the real one.
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	// os.UserConfigDir honors XDG_CONFIG_HOME on unix.
	home, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if got := registryPath(); got != filepath.Join(home, "ward", "projects.json") {
		t.Fatalf("registryPath = %s, want %s", got, filepath.Join(home, "ward", "projects.json"))
	}

	if err := RegisterProject("demo", "/tmp/demo/.ward"); err != nil {
		t.Fatal(err)
	}
	h, ok := ProjectHome("demo")
	if !ok || h != "/tmp/demo/.ward" {
		t.Fatalf("ProjectHome(demo) = %q,%v; want /tmp/demo/.ward,true", h, ok)
	}
	// env override wins
	t.Setenv("WARD_PROJECT_DEMO_HOME", "/env/override/.ward")
	if h, ok := ProjectHome("demo"); !ok || h != "/env/override/.ward" {
		t.Fatalf("env override failed: %q,%v", h, ok)
	}
	// unknown
	if _, ok := ProjectHome("nope"); ok {
		t.Fatal("unknown project should not resolve")
	}
}

func TestOpenForNameRoutesToProjectStore(t *testing.T) {
	dir := t.TempDir()
	projHome := filepath.Join(dir, "demo.ward")
	if err := RegisterProject("demo", projHome); err != nil {
		t.Fatal(err)
	}
	s, err := OpenForName("demo")
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	// The opened store must be the registered project's store, not the cwd's.
	if s.Home != projHome {
		t.Fatalf("OpenForName opened %s, want %s", s.Home, projHome)
	}
	// Schema is initialized (tasks table exists).
	if _, err := s.ListTasks("", 1); err != nil {
		t.Fatalf("store not initialized: %v", err)
	}
	// Empty name falls back to the default (cwd/.ward) store.
	def, err := OpenForName("")
	if err != nil {
		t.Fatal(err)
	}
	defer def.DB.Close()
	if def.Home != Home() {
		t.Fatalf("OpenForName(\"\") home = %s, want %s", def.Home, Home())
	}
	// Unknown project errors with a helpful message.
	if _, err := OpenForName("ghost"); err == nil {
		t.Fatal("expected error for unknown project")
	}
}
