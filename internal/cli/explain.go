package cli

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

// explainCmd reconstructs the evidence chain behind a routing decision — the
// audit view that makes the verify gate believable. It shows the chosen tier
// AND the signals that produced it: which artifacts counted as the memory hit,
// their verify status as of NOW (re-read live, never a stale recap), the
// contention inputs, and the node's attempt history from the run's event log.
func explainCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "explain <runID> [node]",
		Short: "reconstruct why a node routed the way it did (evidence chain, not a verdict)",
		Example: `  ward explain run-3f2a
  ward explain run-3f2a implementation
  ward explain run-3f2a implementation --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(fmt.Errorf("need a run id"))
			}
			runID := args[0]
			nodeFilter := ""
			if len(args) > 1 {
				nodeFilter = args[1]
			}
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			r, err := s.LoadRun(runID)
			if err != nil {
				return failErr(err)
			}
			decs, err := s.RoutingDecisionsForRun(runID)
			if err != nil {
				return failErr(err)
			}
			events, err := s.LoadEvents(runID)
			if err != nil {
				return failErr(err)
			}
			// Node filter applies in BOTH modes (surface parity): a filtered
			// --json view shows that node's decisions and events only.
			if nodeFilter != "" {
				filtered := decs[:0]
				for _, d := range decs {
					if d.Node == nodeFilter {
						filtered = append(filtered, d)
					}
				}
				decs = filtered
				nodeEvents := events[:0]
				for _, e := range events {
					if e.Node == nodeFilter {
						nodeEvents = append(nodeEvents, e)
					}
				}
				events = nodeEvents
			}
			if decs == nil {
				decs = []store.RoutingDecision{}
			}
			if events == nil {
				events = []store.RunEvent{}
			}

			if jsonOut {
				printJSON(map[string]any{
					"run": r, "decisions": decs, "events": events,
				})
				return nil
			}

			printLine(fmt.Sprintf("run %s [%s] workflow=%s waiting=%s", r.ID, r.Status, r.WorkflowName, r.WaitingApproval))
			for _, d := range decs {
				printLine("")
				printLine(fmt.Sprintf("node %s", d.Node))
				printLine(fmt.Sprintf("  routed: tier=%s model=%s ceremony=%s at %s", d.Tier, d.Model, d.Ceremony, d.CreatedAt))
				printLine(fmt.Sprintf("  signals: memory_hit=%v verify=%s contention=%v escalated_from=%q",
					d.MemoryHit, d.VerifyStatus, d.Contention, d.EscalatedFrom))
				printLine("  because: " + d.Reason)
				if ids := contextIDs(d.Context); len(ids) > 0 {
					printLine("  evidence (verified context, re-checked now):")
					for _, id := range ids {
						a, err := s.GetArtifact(id)
						if err != nil {
							printLine(fmt.Sprintf("    %s: MISSING (%v)", id, err))
							continue
						}
						printLine(fmt.Sprintf("    %s verify=%s %q tags=%v cmd=%q",
							a.ID, a.VerifyStatus, a.Summary, a.Tags, a.VerifyCmd))
					}
				} else if !d.MemoryHit {
					printLine(fmt.Sprintf("  evidence: none — no accepted artifact tagged %q; the miss is the whole story", d.Node))
				} else {
					printLine("  evidence: hit present but nothing verified survived the live gate")
				}
				if d.ContentionJSON != "" && d.ContentionJSON != "null" {
					printLine("  contention inputs: " + d.ContentionJSON)
				}
				var nodeEvents []string
				for _, e := range events {
					if e.Node == d.Node {
						nodeEvents = append(nodeEvents, fmt.Sprintf("%s %s: %s", e.At, e.Action, e.Detail))
					}
				}
				if len(nodeEvents) > 0 {
					printLine("  attempts:")
					for _, l := range nodeEvents {
						printLine("    " + l)
					}
				}
			}
			if r.Status == "rejected" {
				printLine("")
				printLine("run was REJECTED — escalation budget spent. Dossier:")
				printDossier(s, runID)
			}
			return nil
		},
	}
	return c
}

func contextIDs(ctxJSON string) []string {
	ctxJSON = strings.TrimSpace(ctxJSON)
	if ctxJSON == "" || ctxJSON == "null" || ctxJSON == "[]" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(ctxJSON), &ids); err != nil {
		return nil
	}
	return ids
}
