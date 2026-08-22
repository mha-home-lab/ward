package cli

import (
	"fmt"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/mha-home-lab/ward/internal/verification"
	"github.com/spf13/cobra"
)

// tickCmd sweeps all store-local accepted artifacts, runs their verify_cmd
// LIVE, and reports drift (previously verified -> no longer verified).
func tickCmd() *cobra.Command {
	var repo string
	c := &cobra.Command{
		Use:   "tick",
		Short: "maintenance sweep: re-verify local artifacts, report drift",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			accepted, err := s.ListArtifacts("accepted", "", "", 1000)
			if err != nil {
				return failErr(err)
			}
			type rep struct {
				ID, Before, After, Detail string
			}
			var reports []rep
			var drift int
			for _, a := range accepted {
				if !a.Local {
					continue
				}
				res := verification.Run(a, repo)
				before := a.VerifyStatus
				_ = s.SetVerify(a.ID, res.Status)
				if before == "verified" && res.Status != "verified" {
					drift++
				}
				if before != res.Status {
					reports = append(reports, rep{a.ID, before, res.Status, res.Detail})
				}
			}
			// Free expired claims so their topics can be re-claimed (the
			// unique-index slot is released, not just the TTL blowing past).
			swept, err := s.SweepExpiredClaims()
			if err != nil {
				return failErr(err)
			}

			if jsonOut {
				printJSON(map[string]any{"checked": len(accepted), "drift": drift, "changed": reports, "claims_expired": swept})
			} else {
				printLine(fmt.Sprintf("checked %d local accepted artifacts; drift=%d; expired claims freed=%d", len(accepted), drift, swept))
				for _, r := range reports {
					printLine(fmt.Sprintf("  %s %s -> %s (%s)", r.ID, r.Before, r.After, r.Detail))
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&repo, "repo", "", "repo root for verification")
	return c
}
