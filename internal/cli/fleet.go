package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/mha-home-lab/ward/internal/verification"
	"github.com/spf13/cobra"
)

func osSetenvImpl(key, val string) { os.Setenv(key, val) }
func verificationRun(a store.Artifact, repo string) verification.Result {
	return verification.Run(a, repo)
}

// waveCmd (rd:c3, promoted 06fad7dc) runs a regression wave: live re-verify
// every accepted artifact carrying a topic tag, superseding drift under --heal.
// This is tick scoped to a topic — the standing proof that tagged knowledge
// still holds, and the mechanism that keeps topic-vouching honest.
func waveCmd() *cobra.Command {
	var repo string
	var heal bool
	var topic string
	c := &cobra.Command{
		Use:   "wave <topic>",
		Short: "regression wave: live re-verify all artifacts tagged <topic> (--heal supersedes drift)",
		Example: `  ward wave topic:auth
  ward wave topic:auth --heal
  ward wave topic:auth --repo ../other-repo --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(fmt.Errorf("wave needs a topic tag"))
			}
			topic = args[0]
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()

			arts, err := s.SearchArtifactsTagged("", "", "", topic, 200)
			if err != nil {
				return failErr(err)
			}
			type result struct {
				ID       string `json:"id"`
				Before   string `json:"before"`
				After    string `json:"after"`
				Detail   string `json:"detail,omitempty"`
				Healed   bool   `json:"healed,omitempty"`
				NoVerify bool   `json:"no_verify_cmd,omitempty"`
			}
			results := make([]result, 0)
			verified, drifted := 0, 0
			for _, a := range arts {
				if a.VerifyCmd == "" {
					results = append(results, result{ID: a.ID, Before: a.VerifyStatus, After: "skipped", NoVerify: true})
					continue
				}
				res := verificationRun(a, repo)
				before := a.VerifyStatus
				_ = s.SetVerify(a.ID, res.Status)
				r := result{ID: a.ID, Before: before, After: res.Status}
				if res.Status == "verified" {
					verified++
				} else {
					drifted++
					if heal {
						if err := s.Supersede(a.ID, "", "wave drift"); err == nil {
							r.Healed = true
						}
					}
				}
				r.Detail = res.Detail
				results = append(results, r)
			}

			if jsonOut {
				printJSON(map[string]any{"topic": topic, "checked": len(arts), "verified": verified, "drifted": drifted, "results": results})
			} else {
				fmt.Printf("== wave %s: %d checked, %d verified, %d drifted ==\n", topic, len(arts), verified, drifted)
				for _, r := range results {
					mark := ""
					if r.Healed {
						mark = " [superseded]"
					}
					if r.NoVerify {
						fmt.Printf("  %s %s -> skipped (no verify_cmd)\n", r.ID, r.Before)
						continue
					}
					fmt.Printf("  %s %s -> %s%s\n", r.ID, r.Before, r.After, mark)
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&repo, "repo", "", "repo root for verification")
	c.Flags().BoolVar(&heal, "heal", false, "supersede drifted artifacts")
	return c
}

// fleetCmd (rd:c3, promoted f9a6b73c): one command, many stores. Read-only
// aggregation of harvest telemetry across project stores so an architect sees
// the whole estate instead of per-repo islands.
func fleetCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "fleet <store-dir>...",
		Short: "aggregate harvest telemetry across multiple ward stores (read-only)",
		Example: `  ward fleet ../a/.ward ../b/.ward
  ward fleet ~/play/*/.ward --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return failErr(fmt.Errorf("fleet needs at least one store dir (e.g. ../a/.ward ../b/.ward)"))
			}
			type row struct {
				Store         string `json:"store"`
				Decisions     int    `json:"decisions"`
				CheapVerified int    `json:"cheap_verified"`
				TasksDone     int    `json:"tasks_done"`
				TasksOpen     int    `json:"tasks_open"`
				Knowledge     int    `json:"accepted_knowledge"`
				Verified      int    `json:"verified"`
				DriftHealed   int    `json:"drift_healed"`
			}
			var rows []row
			// Each store is opened under its OWN WARD_HOME; the process env is
			// restored afterwards so this command leaves no global residue.
			prevHome := os.Getenv("WARD_HOME")
			defer func() {
				if prevHome == "" {
					_ = os.Unsetenv("WARD_HOME")
				} else {
					_ = os.Setenv("WARD_HOME", prevHome)
				}
			}()
			for _, dir := range args {
				home, err := filepath.Abs(dir)
				if err != nil {
					continue
				}
				os.Setenv("WARD_HOME", home)
				s, err := store.Open()
				if err != nil {
					rows = append(rows, row{Store: filepath.Base(filepath.Dir(home)), Decisions: -1})
					continue
				}
				decs, _ := s.AllRoutingDecisions(2000)
				cv := 0
				for _, d := range decs {
					if d.Tier == "cheap" && d.MemoryHit && d.VerifyStatus == "verified" {
						cv++
					}
				}
				tasks, _ := s.ListTasks("", 500)
				done, open := 0, 0
				for _, t := range tasks {
					switch t.Status {
					case "done":
						done++
					case "open", "claimed":
						open++
					}
				}
				acc, _ := s.ListArtifacts("accepted", "", "", 2000)
				verified := 0
				for _, a := range acc {
					if a.VerifyStatus == "verified" {
						verified++
					}
				}
				sup, _ := s.ListArtifacts("superseded", "", "", 2000)
				healed := 0
				for _, a := range sup {
					if a.SupersededRsn == "drift" || a.SupersededRsn == "wave drift" {
						healed++
					}
				}
				name := filepath.Base(filepath.Dir(home))
				rows = append(rows, row{Store: name, Decisions: len(decs), CheapVerified: cv,
					TasksDone: done, TasksOpen: open, Knowledge: len(acc), Verified: verified, DriftHealed: healed})
				s.DB.Close()
			}

			if jsonOut {
				printJSON(rows)
				return nil
			}
			sort.Slice(rows, func(i, j int) bool { return rows[i].Store < rows[j].Store })
			fmt.Println("== fleet ==")
			for _, r := range rows {
				if r.Decisions < 0 {
					fmt.Printf("  %-14s store error\n", r.Store)
					continue
				}
				pct := "0%"
				if r.Decisions > 0 {
					pct = fmt.Sprintf("%.0f%%", 100*float64(r.CheapVerified)/float64(r.Decisions))
				}
				fmt.Printf("  %-14s decisions=%-4d cheap=%d (%s) tasks done=%d open=%d | knowledge=%d verified=%d healed=%d\n",
					r.Store, r.Decisions, r.CheapVerified, pct, r.TasksDone, r.TasksOpen, r.Knowledge, r.Verified, r.DriftHealed)
			}
			fmt.Println("(read-only; run 'ward harvest' inside a repo for its full report)")
			return nil
		},
	}
	return c
}
