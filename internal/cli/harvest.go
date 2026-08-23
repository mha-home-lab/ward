package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

// harvestCmd is the R&D data spine (.spec/research.md): aggregates the
// store's operational history into the five telemetry sections an architect
// needs before deciding what to research or fix. Observer-only: it never
// writes, never feeds routing.
func harvestCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "harvest",
		Short: "R&D report: tier distribution, cheap-hit rate, bounce leaders, dossier themes, drift",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()

			h := harvestReport{}
			h.Tiers = map[string]int{}
			h.HitRate = map[string]any{}

			decs, err := s.AllRoutingDecisions(2000)
			if err != nil {
				return failErr(err)
			}
			cheapVerified, misses, staleHits := 0, 0, 0
			for _, d := range decs {
				h.Tiers[d.Tier]++
				if d.Tier == "cheap" && d.MemoryHit && d.VerifyStatus == "verified" {
					cheapVerified++
				}
				if !d.MemoryHit {
					misses++
				} else if d.VerifyStatus != "verified" {
					staleHits++
				}
			}
			if len(decs) > 0 {
				h.HitRate["decisions"] = len(decs)
				h.HitRate["cheap_verified"] = cheapVerified
				h.HitRate["cheap_verified_pct"] = fmt.Sprintf("%.0f%%", 100*float64(cheapVerified)/float64(len(decs)))
				h.HitRate["memory_misses"] = misses
				h.HitRate["stale_hits_demoted"] = staleHits
			}

			tasks, err := s.ListTasks("", 500)
			if err != nil {
				return failErr(err)
			}
			h.Tasks = map[string]int{}
			var bounces []bounceView
			for _, t := range tasks {
				h.Tasks[t.Status]++
				if t.Escalation > 0 {
					bounces = append(bounces, bounceView{t.ID, t.Title, t.Escalation, t.TierFloor})
				}
			}
			sort.Slice(bounces, func(i, j int) bool { return bounces[i].Esc > bounces[j].Esc })
			if len(bounces) > 5 {
				bounces = bounces[:5]
			}
			h.BounceLeaders = bounces

			drift, _ := s.StaleArtifacts(30, 100)
			superseded, _ := s.ListArtifacts("superseded", "", "", 1000)
			driftCount := 0
			for _, a := range superseded {
				if a.SupersededRsn == "drift" {
					driftCount++
				}
			}
			h.Drift = map[string]int{
				"healed_superseded": driftCount,
				"rarely_reused":     len(drift),
			}

			acc, _ := s.ListArtifacts("accepted", "", "", 2000)
			reused := 0
			for _, a := range acc {
				if a.UsedCount > 0 {
					reused++
				}
			}
			h.Knowledge = map[string]int{
				"accepted": len(acc), "reused": reused,
			}

			runs, _ := s.OpenRuns()
			var dossiers []string
			supArt, _ := s.SearchArtifacts("dossier", "", "", 10)
			for _, a := range supArt {
				if tagsContain(a.Tags, "dossier") {
					line := a.Summary
					if i := strings.Index(line, ": escalation"); i > 0 {
						line = line[:i]
					}
					dossiers = append(dossiers, line)
				}
			}
			h.Dossiers = dossiers
			h.OpenRuns = len(runs)

			if jsonOut {
				printJSON(h)
			} else {
				printHumanHarvest(h)
			}
			return nil
		},
	}
	return c
}

type harvestReport struct {
	Tiers         map[string]int `json:"tiers"`
	HitRate       map[string]any `json:"hit_rate"`
	Tasks         map[string]int `json:"tasks_by_status"`
	BounceLeaders []bounceView   `json:"bounce_leaders,omitempty"`
	Knowledge     map[string]int `json:"knowledge"`
	Drift         map[string]int `json:"drift"`
	Dossiers      []string       `json:"dossier_themes,omitempty"`
	OpenRuns      int            `json:"open_runs"`
}

type bounceView struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Esc   int    `json:"escalation"`
	Floor string `json:"floor"`
}

func printHumanHarvest(h harvestReport) {
	fmt.Println("== ward harvest (R&D telemetry) ==")
	// Fixed-order rendering: ward's CLI contract is deterministic, parseable
	// output — map iteration would shuffle lines between identical runs.
	fmt.Println("routing tiers:")
	for _, tier := range []string{"cheap", "mid", "strong", "rejected"} {
		if n := h.Tiers[tier]; n > 0 {
			fmt.Printf("  %-7s %d\n", tier, n)
		}
	}
	fmt.Println("cheap+verified rate:")
	fmt.Printf("  %-20s %d decisions\n", "decisions:", h.HitRate["decisions"])
	fmt.Printf("  %-20s %v\n", "cheap_verified:", h.HitRate["cheap_verified"])
	fmt.Printf("  %-20s %v\n", "cheap_verified_pct:", h.HitRate["cheap_verified_pct"])
	fmt.Printf("  %-20s %v\n", "memory_misses:", h.HitRate["memory_misses"])
	fmt.Printf("  %-20s %v\n", "stale_hits_demoted:", h.HitRate["stale_hits_demoted"])
	fmt.Println("tasks:")
	statuses := []string{"open", "claimed", "done", "rejected"}
	for _, st := range statuses {
		if h.Tasks[st] > 0 {
			fmt.Printf("  %-9s %d\n", st, h.Tasks[st])
		}
	}
	if len(h.BounceLeaders) > 0 {
		fmt.Println("bounce leaders (authoring suspects):")
		for _, b := range h.BounceLeaders {
			fmt.Printf("  esc=%d floor=%s %s %.60s\n", b.Esc, b.Floor, b.ID, b.Title)
		}
	}
	fmt.Printf("knowledge: accepted=%d reused=%d | drift: healed=%d rarely-used=%d\n",
		h.Knowledge["accepted"], h.Knowledge["reused"], h.Drift["healed_superseded"], h.Drift["rarely_reused"])
	if len(h.Dossiers) > 0 {
		fmt.Println("dossier themes:")
		for _, d := range h.Dossiers {
			fmt.Println("  " + d)
		}
	}
	if h.OpenRuns > 0 {
		fmt.Printf("open runs: %d\n", h.OpenRuns)
	}
	fmt.Println("next: pick a finding, spawn an explorer per .spec/research.md")
}
