package cli

import (
	"fmt"
	"strings"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

// rejectCmd surfaces the reject dossier: the evidence packet a human receives
// when the escalation budget is spent. The dossier was written by the engine at
// rejection time from the run's own event log and routing decisions; this
// command only reads it back. `ward handoff` items point here too.
func rejectCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "reject <runID>",
		Short: "show the reject dossier for an exhausted run (evidence, not a void)",
		Example: `  ward reject run-9c1d
  ward reject run-9c1d --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(fmt.Errorf("need a run id"))
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			r, err := s.LoadRun(args[0])
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				dossiers, _ := s.SearchArtifacts("reject:"+args[0], "", "", 10)
				if dossiers == nil {
					dossiers = []store.Artifact{}
				}
				printJSON(map[string]any{"run": r, "dossiers": dossiers})
				return nil
			}
			printLine(fmt.Sprintf("run %s [%s]", r.ID, r.Status))
			if r.Status != "rejected" {
				printLine("(not rejected — no dossier expected)")
			}
			printDossier(s, args[0])
			return nil
		},
	}
	return c
}

// printDossier prints every dossier artifact tagged reject:<runID>.
func printDossier(s *store.Store, runID string) {
	res, err := s.SearchArtifacts("reject:"+runID, "", "", 10)
	if err != nil {
		return
	}
	found := false
	for _, a := range res {
		if !tagsContain(a.Tags, "reject:"+runID) {
			continue
		}
		found = true
		printLine("")
		printLine("[" + a.ID + "] " + a.Summary)
		for _, l := range strings.Split(strings.TrimRight(a.Content, "\n"), "\n") {
			printLine("  " + l)
		}
	}
	if !found && !jsonOut {
		printLine("no dossier found (runs rejected before v0.5 have none)")
	}
}

func tagsContain(tags []string, v string) bool {
	for _, t := range tags {
		if t == v {
			return true
		}
	}
	return false
}
