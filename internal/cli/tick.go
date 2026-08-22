package cli

import (
	"fmt"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/mha-home-lab/ward/internal/verification"
	"github.com/spf13/cobra"
)

// verifyChange records one artifact whose live verification status moved.
type verifyChange struct {
	ID            string
	Before, After string
	Detail        string
}

// sweepVerify re-runs every store-local accepted artifact's verify_cmd LIVE
// against the repo and persists the outcome. This is the drift detector: a
// previously-verified artifact that fails is stale, and the router treats any
// non-verified status as a memory MISS. Returns what changed.
func sweepVerify(s *store.Store, repo string) (checked, drift int, changes []verifyChange, err error) {
	accepted, err := s.ListArtifacts("accepted", "", "", 1000)
	if err != nil {
		return 0, 0, nil, err
	}
	for _, a := range accepted {
		if !a.Local {
			continue
		}
		res := verification.Run(a, repo)
		before := a.VerifyStatus
		if err := s.SetVerify(a.ID, res.Status); err != nil {
			return checked, drift, changes, err
		}
		checked++
		if before == "verified" && res.Status != "verified" {
			drift++
		}
		if before != res.Status {
			changes = append(changes, verifyChange{ID: a.ID, Before: before, After: res.Status, Detail: res.Detail})
		}
	}
	return checked, drift, changes, nil
}

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
			checked, drift, reports, err := sweepVerify(s, repo)
			if err != nil {
				return failErr(err)
			}
			// Free expired claims so their topics can be re-claimed (the
			// unique-index slot is released, not just the TTL blowing past).
			swept, err := s.SweepExpiredClaims()
			if err != nil {
				return failErr(err)
			}

			if jsonOut {
				printJSON(map[string]any{"checked": checked, "drift": drift, "changed": reports, "claims_expired": swept})
			} else {
				printLine(fmt.Sprintf("checked %d local accepted artifacts; drift=%d; expired claims freed=%d", checked, drift, swept))
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
