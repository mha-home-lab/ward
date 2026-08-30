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
	Healed        bool
}

// sweepVerify re-runs every store-local accepted artifact's verify_cmd LIVE
// against the repo and persists the outcome. This is the drift detector: any
// local artifact that fails live verification is drifted, whether it was
// previously verified (then it flips to stale) or never verified (then it was
// already erroring) — an absolute count, not just this sweep's transitions.
// Returns what changed.
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
		if res.Status == "stale" || res.Status == "error" {
			drift++
		}
		if before != res.Status {
			changes = append(changes, verifyChange{ID: a.ID, Before: before, After: res.Status, Detail: res.Detail})
		}
	}
	return checked, drift, changes, nil
}

// tickCmd sweeps all store-local accepted artifacts, runs their verify_cmd
// LIVE, and reports drift (local artifacts failing live verification). With
// --heal it also closes the loop: every drifted artifact is superseded with
// reason "drift" so it can never vote cheap or pollute context again. Healing
// acts only on store-local artifacts (the sweep already excludes imports) and
// never stamps a status without evidence — supersede happens only after the
// live re-run failed.
func tickCmd() *cobra.Command {
	var repo string
	var heal bool
	c := &cobra.Command{
		Use:   "tick",
		Short: "maintenance sweep: re-verify local artifacts, report drift (--heal supersedes drift)",
		Example: `  ward tick
  ward tick --heal
  ward tick --heal --repo ../other-repo --json`,
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
			// Healing closes the loop: any store-local accepted artifact whose
			// live re-verification FAILED (fresh drift or a persisting zombie)
			// is superseded with reason "drift". It could not vote cheap
			// anyway; keeping it would only pollute search and context. We
			// inspect post-sweep statuses (not just this sweep's transitions)
			// so an artifact that failed on a previous tick is still healed.
			healedIDs := map[string]bool{}
			healed := 0
			if heal {
				accepted, err := s.ListArtifacts("accepted", "", "", 1000)
				if err != nil {
					return failErr(err)
				}
				for _, a := range accepted {
					if !a.Local || (a.VerifyStatus != "stale" && a.VerifyStatus != "error") {
						continue
					}
					if err := s.Supersede(a.ID, "", "drift"); err != nil {
						return failErr(err)
					}
					healedIDs[a.ID] = true
					healed++
				}
			}
			for i := range reports {
				reports[i].Healed = healedIDs[reports[i].ID]
			}
			// Free expired claims so their topics can be re-claimed (the
			// unique-index slot is released, not just the TTL blowing past).
			swept, err := s.SweepExpiredClaims()
			if err != nil {
				return failErr(err)
			}

			if jsonOut {
				printJSON(map[string]any{"checked": checked, "drift": drift, "healed": healed, "changed": reports, "claims_expired": swept})
			} else {
				printLine(fmt.Sprintf("checked %d local accepted artifacts; drift=%d; healed=%d; expired claims freed=%d", checked, drift, healed, swept))
				for _, r := range reports {
					mark := ""
					if r.Healed {
						mark = " [superseded]"
					}
					printLine(fmt.Sprintf("  %s %s -> %s (%s)%s", r.ID, r.Before, r.After, r.Detail, mark))
				}
			}
			return nil
		},
	}
	c.Flags().StringVar(&repo, "repo", "", "repo root for verification")
	c.Flags().BoolVar(&heal, "heal", false, "supersede artifacts that failed live re-verification (drift)")
	return c
}
