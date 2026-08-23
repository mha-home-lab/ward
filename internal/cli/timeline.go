package cli

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

// timelineCmd is the architect's span view (rd:c1, promoted 56b8cec6): one
// unified stream of what the fleet DID — routing decisions as spans (with
// per-node duration computed from run events), task transitions, captures.
// Observer-only; joins existing tables, no new state.
func timelineCmd() *cobra.Command {
	var limit int
	c := &cobra.Command{
		Use:   "timeline",
		Short: "unified activity stream: routing spans with durations, task transitions, captures",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()

			type span struct {
				At       string
				Kind     string
				Key      string
				Detail   string
				Duration string
				sortKey  string
			}
			out := make([]span, 0, limit)

			decs, err := s.AllRoutingDecisions(limit)
			if err != nil {
				return failErr(err)
			}
			for _, d := range decs {
				dur := ""
				if d.RunID != "" {
					events, err := s.LoadEvents(d.RunID)
					if err == nil {
						var start, end string
						for _, e := range events {
							if e.Node != d.Node {
								continue
							}
							if start == "" || e.At < start {
								start = e.At
							}
							if e.At > end {
								end = e.At
							}
						}
						if start != "" && end > start {
							dur = fmtDuration(start, end)
						}
					}
				}
				out = append(out, span{
					At: d.CreatedAt, Kind: "route",
					Key:      fmt.Sprintf("%s/%s", shortRun(d.RunID), d.Node),
					Detail:   fmt.Sprintf("%s hit=%v verify=%s", d.Tier, d.MemoryHit, d.VerifyStatus),
					Duration: dur, sortKey: d.CreatedAt + "|route|" + d.Node,
				})
			}

			tasks, err := s.ListTasks("", limit)
			if err != nil {
				return failErr(err)
			}
			for _, t := range tasks {
				if t.Status == "open" && t.ClaimedBy == "" {
					continue // never touched; noise in a timeline
				}
				out = append(out, span{
					At: t.UpdatedAt, Kind: "task",
					Key:     t.ID,
					Detail:  fmt.Sprintf("%s floor=%s esc=%d by=%s", t.Status, t.TierFloor, t.Escalation, orDefault(t.ClaimedBy, "-")),
					sortKey: t.UpdatedAt + "|task|" + t.ID,
				})
			}

			caps, err := s.ListArtifacts("accepted", "", "", limit)
			if err != nil {
				return failErr(err)
			}
			for _, a := range caps {
				if !strings.HasPrefix(a.Summary, "work") && a.CreatedBy != "ward" {
					continue // captures only
				}
				out = append(out, span{
					At: a.CreatedAt, Kind: "capture",
					Key:     a.ID,
					Detail:  fmt.Sprintf("%s verify=%s tags=%v", a.Kind, a.VerifyStatus, a.Tags),
					sortKey: a.CreatedAt + "|capture|" + a.ID,
				})
			}

			sort.Slice(out, func(i, j int) bool { return out[i].sortKey > out[j].sortKey })
			if len(out) > limit {
				out = out[:limit]
			}

			if jsonOut {
				printJSON(out)
				return nil
			}
			fmt.Println("== ward timeline (newest first) ==")
			for _, sp := range out {
				line := fmt.Sprintf("%s  %-8s %-22s %s", sp.At, sp.Kind, sp.Key, sp.Detail)
				if sp.Duration != "" {
					line += " (" + sp.Duration + ")"
				}
				fmt.Println(line)
			}
			return nil
		},
	}
	c.Flags().IntVar(&limit, "limit", 40, "max entries")
	return c
}

func shortRun(id string) string { return strings.TrimPrefix(id, "run-") }

// fmtDuration renders an ISO-timestamp delta compactly (same-day seconds,
// otherwise coarse hours/days).
func fmtDuration(start, end string) string {
	const layout = "2006-01-02T15:04:05Z"
	st, err1 := time.Parse(layout, start)
	en, err2 := time.Parse(layout, end)
	if err1 != nil || err2 != nil {
		return ""
	}
	d := en.Sub(st)
	switch {
	case d < 0:
		return ""
	case d.Seconds() < 1:
		return "<1s"
	case d.Minutes() < 1:
		return fmt.Sprintf("%.0fs", d.Seconds())
	case d.Hours() < 1:
		return fmt.Sprintf("%.0fm", d.Minutes())
	default:
		return fmt.Sprintf("%.1fh", d.Hours())
	}
}
