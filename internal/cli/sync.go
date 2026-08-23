package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

// syncCmd is the oracle's push mechanism: recompile EVERY accepted
// portable:<topic> body of knowledge into the user-level skills directory
// (~/.config/opencode/skills/), so agents in ANY project - including ones
// ward has never touched - load current lessons at session start. Chips are
// caches; sync is how the cache stays honest.
func syncCmd() *cobra.Command {
	var dir string
	c := &cobra.Command{
		Use:   "skill-sync",
		Short: "push all portable:<topic> knowledge to the global skills directory",
		Example: `  ward skill-sync
  ward skill-sync --dir ~/.config/opencode/skills --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			topics, err := s.PortableTopics()
			if err != nil {
				return failErr(err)
			}
			if topics == nil {
				topics = []string{}
			}
			if dir == "" {
				home, _ := os.UserHomeDir()
				dir = filepath.Join(home, ".config", "opencode", "skills")
			}
			// The sync happens in BOTH modes: --json is a machine-readable
			// report of the same work, never a silent dry-run.
			synced := make([]map[string]any, 0, len(topics))
			for _, t := range topics {
				name := chipNameFor(strings.TrimPrefix(t, "portable:"))
				srcs, err := s.SearchArtifactsTagged("", "", "", t, 50)
				if err != nil || len(srcs) == 0 {
					continue
				}
				path := filepath.Join(dir, name, "SKILL.md")
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return failErr(err)
				}
				if err := os.WriteFile(path, []byte(renderChip(name, t, s.Home, srcs)), 0o644); err != nil {
					return failErr(err)
				}
				synced = append(synced, map[string]any{"topic": t, "sources": len(srcs), "path": path})
			}
			if jsonOut {
				printJSON(map[string]any{"dir": dir, "topics": topics, "synced": synced})
				return nil
			}
			if len(topics) == 0 {
				printLine("no portable:<topic> knowledge accepted yet")
				return nil
			}
			for _, sy := range synced {
				fmt.Printf("synced %-24s (%d sources) -> %s\n", sy["topic"], sy["sources"], sy["path"])
			}
			return nil
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "override target skills directory")
	return c
}
