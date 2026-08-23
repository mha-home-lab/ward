package cli

import (
	"fmt"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/mha-home-lab/ward/internal/verification"
	"github.com/spf13/cobra"
)

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
