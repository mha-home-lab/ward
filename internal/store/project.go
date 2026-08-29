package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// ProjectRegistry maps a logical project name to the directory that holds its
// `.ward` store. It exists so a request about project X can be filed (and read)
// from ANY working directory — without it, an agent working in project Y silently
// writes project X's feature request into Y's store, where X's own agents never
// see it. That misplacement is exactly the failure this file prevents.
type ProjectRegistry map[string]string

func registryPath() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = os.Getenv("HOME")
	}
	return filepath.Join(base, "ward", "projects.json")
}

func loadRegistry() (ProjectRegistry, error) {
	b, err := os.ReadFile(registryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return ProjectRegistry{}, nil
		}
		return nil, err
	}
	var reg ProjectRegistry
	if err := json.Unmarshal(b, &reg); err != nil {
		return nil, err
	}
	if reg == nil {
		reg = ProjectRegistry{}
	}
	return reg, nil
}

func saveRegistry(reg ProjectRegistry) error {
	p := registryPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}

// ProjectHome resolves the `.ward` store directory for a logical project name.
// Precedence: WARD_PROJECT_<NAME>_HOME env, then the on-disk registry. Returns
// ("", false) when the name is unknown.
func ProjectHome(name string) (string, bool) {
	if name == "" {
		return "", false
	}
	if v := os.Getenv("WARD_PROJECT_" + strings.ToUpper(name) + "_HOME"); v != "" {
		return v, true
	}
	reg, err := loadRegistry()
	if err != nil {
		return "", false
	}
	if h, ok := reg[name]; ok {
		return h, true
	}
	return "", false
}

// RegisterProject records name -> homeDir (the `.ward` directory) in the registry.
func RegisterProject(name, homeDir string) error {
	reg, err := loadRegistry()
	if err != nil {
		return err
	}
	if reg == nil {
		reg = ProjectRegistry{}
	}
	reg[name] = homeDir
	return saveRegistry(reg)
}

// ListProjects returns the current registry (name -> homeDir).
func ListProjects() (ProjectRegistry, error) {
	return loadRegistry()
}

// OpenForName opens the store for a logical project (resolved via ProjectHome), or
// the default store for the current cwd/WARD_HOME when name is empty.
func OpenForName(name string) (*Store, error) {
	if name == "" {
		return Open()
	}
	home, ok := ProjectHome(name)
	if !ok {
		return nil, fmt.Errorf("unknown project %q: register it with 'ward project register %s <path-to-.ward-dir>' or set WARD_PROJECT_%s_HOME",
			name, name, strings.ToUpper(name))
	}
	return openDB(filepath.Join(home, "ward.db"))
}

// openDB opens (creating if needed) a ward.db at the given path and ensures schema.
func openDB(path string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, err
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		return nil, err
	}
	s := &Store{DB: db, Home: filepath.Dir(path)}
	if err := s.Init(); err != nil {
		return nil, err
	}
	return s, nil
}
