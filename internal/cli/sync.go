package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

// syncCmd is the oracle's push mechanism: recompile EVERY accepted
// portable:<topic> body of knowledge into the user-level skills directory
// (~/.config/opencode/skills/), so agents in ANY project - including ones
// ward has never touched - load current lessons at session start. Chips are
// caches; sync is how the cache stays honest.
//
// Because sync writes directly to the GLOBAL vault, it is a hard-gate point for
// the transferability lint (spec decisions: the gate belongs where knowledge
// actually leaves the repo — pack → global dir / skill-sync). Cheat-sheet
// sources are excluded from every chip (or the whole chip skipped if none
// survive); --force --reason overrides and records the exception.
func syncCmd() *cobra.Command {
	var dir, reason string
	var force, cleanup bool
	c := &cobra.Command{
		Use:   "skill-sync",
		Short: "push all portable:<topic> knowledge to the global skills directory",
		Example: `  ward skill-sync
  ward skill-sync --dir ~/.config/opencode/skills --json
  ward skill-sync --force --reason "known-good one-off reference"`,
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
			type topicResult struct {
				Topic      string
				SrcCount   int
				Path       string
				Removed    []store.Artifact
				Overridden []store.Artifact
				Skipped    string // reason the chip was not written
			}
			var results []topicResult
			for _, t := range topics {
				name := chipNameFor(t)
				srcs, err := s.ArtifactsForPortableTopic(t)
				if err != nil || len(srcs) == 0 {
					continue
				}
				var g gateOutcome
				if g, err = gateTransferability(s, &srcs, force, reason); err != nil {
					return failErr(err)
				}
				path := filepath.Join(dir, name, "SKILL.md")
				r := topicResult{Topic: t, SrcCount: len(srcs), Path: path, Removed: g.removed, Overridden: g.overridden}
				if len(srcs) == 0 {
					if len(g.removed) > 0 {
						r.Skipped = "all sources scored as instance-specific cheat-sheets; not synced to the global vault"
					} else {
						r.Skipped = "no eligible portable sources"
					}
					results = append(results, r)
					continue
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return failErr(err)
				}
				if err := os.WriteFile(path, []byte(renderChip(name, t, s.Home, srcs)), 0o644); err != nil {
					return failErr(err)
				}
				results = append(results, r)
			}
			// cleanupLocal is the `--cleanup` safety check: a one-off port
			// session has its local .ward store removed AFTER a successful sync.
			// It runs ONLY at success sinks — never on the error returns above,
			// so a failed sync never has its evidence deleted out from under the
			// agent. Returns a JSON map describing what happened (empty when
			// cleanup is not requested, refused, or aborted) so the caller can
			// merge it into a single --json document, and prints one human line
			// when not in --json mode. Resolves cleanup BEFORE the store is
			// closed, and closes the store itself just before removal.
			cleanupLocal := func() map[string]any {
				if !cleanup {
					return map[string]any{}
				}
				req, cerr := s.HasTaskOrRunHistory()
				if cerr != nil {
					fmt.Fprintf(os.Stderr, "cleanup aborted: could not inspect store (%v); nothing removed\n", cerr)
					return map[string]any{"cleanup": map[string]any{"removed": false, "refused": "could not inspect store"}}
				}
				if req {
					fmt.Fprintln(os.Stderr, "cleanup refused: store has task/run history beyond this port session; --cleanup refuses to remove it")
					return map[string]any{"cleanup": map[string]any{"removed": false, "refused": "task/run history present"}}
				}
				// Nothing would have been synced (every topic gate failed) —
				// the agent should see that failure, not have its evidence
				// deleted out from under it.
				wrote := 0
				for _, r := range results {
					if r.Skipped == "" {
						wrote++
					}
				}
				if wrote == 0 {
					fmt.Fprintln(os.Stderr, "cleanup refused: every portable topic was skipped (nothing synced); not deleting the store under a failed sync")
					return map[string]any{"cleanup": map[string]any{"removed": false, "refused": "nothing synced"}}
				}
				home := s.Home
				s.DB.Close()
				if err := os.RemoveAll(home); err != nil {
					fmt.Fprintf(os.Stderr, "cleanup failed: %v\n", err)
					return map[string]any{"cleanup": map[string]any{"removed": false, "error": err.Error()}}
				}
				if !jsonOut {
					printLine("removed one-off port session store: " + home)
				}
				return map[string]any{"cleanup": map[string]any{"removed": true, "path": home}}
			}
			if jsonOut {
				synced := []map[string]any{}
				for _, r := range results {
					row := map[string]any{"topic": r.Topic, "sources": r.SrcCount, "path": r.Path}
					if r.Skipped != "" {
						row["skipped"] = r.Skipped
					}
					if len(r.Removed) > 0 {
						inst := []map[string]any{}
						for _, a := range r.Removed {
							inst = append(inst, map[string]any{"id": a.ID, "summary": a.Summary})
						}
						row["not_synced_to_global_vault"] = inst
					}
					if len(r.Overridden) > 0 {
						ovr := []map[string]any{}
						for _, a := range r.Overridden {
							ovr = append(ovr, map[string]any{"id": a.ID, "summary": a.Summary, "reason": reason})
						}
						row["force_included_with_reason"] = ovr
					}
					synced = append(synced, row)
				}
				out := map[string]any{"dir": dir, "topics": topics, "synced": synced}
				for k, v := range cleanupLocal() {
					out[k] = v
				}
				printJSON(out)
				return nil
			}
			if len(topics) == 0 {
				printLine("no portable:<topic> knowledge accepted yet")
				cleanupLocal()
				return nil
			}
			for _, r := range results {
				if r.Skipped != "" {
					fmt.Printf("skipped %-24s (%d sources) %s\n", r.Topic, r.SrcCount, r.Skipped)
					continue
				}
				fmt.Printf("synced %-24s (%d sources) -> %s\n", r.Topic, r.SrcCount, r.Path)
				for _, a := range r.Removed {
					fmt.Printf("    EXCLUDED %s: instance-specific, not synced to the global vault\n", a.ID)
				}
				for _, a := range r.Overridden {
					fmt.Printf("    FORCED %s: instance-specific cheat-sheet synced anyway (reason: %s)\n", a.ID, reason)
				}
			}
			cleanupLocal()
			return nil
		},
	}
	c.Flags().StringVar(&dir, "dir", "", "override target skills directory")
	c.Flags().StringVar(&reason, "reason", "", "required with --force: why a cheat-sheet source is being synced to the global vault anyway")
	c.Flags().BoolVar(&force, "force", false, "bypass the transferability lint for portable chips synced to the global skills dir (needs --reason)")
	c.Flags().BoolVar(&cleanup, "cleanup", false, "one-off port session: after a successful sync, remove the local .ward store (refuses if the store has task/run history)")
	return c
}
