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
			if dir == "" {
				home, _ := os.UserHomeDir()
				dir = filepath.Join(home, ".config", "opencode", "skills")
			}
			if jsonOut {
				printJSON(map[string]any{"topics": topics, "dir": dir})
				return nil
			}
			if len(topics) == 0 {
				printLine("no portable:<topic> knowledge accepted yet")
				return nil
			}
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
				if err := os.WriteFile(path, []byte(renderChip(name, t, srcs)), 0o644); err != nil {
					return failErr(err)
				}
				fmt.Printf("synced %-24s (%d sources) -> %s\n", t, len(srcs), path)
			}
			return nil
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "override target skills directory")
	return c
}
