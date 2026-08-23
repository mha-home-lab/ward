package cli

import (
	"fmt"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

// scorecardCmd (rd:c3, deferred twice then built under reflection R4):
// engineer performance from pool outcomes. Environment failures surface as
// open-released tasks with no attribution drift — the architect reads bounce
// ratios against done counts, never a single number.
func scorecardCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scorecard",
		Short: "engineer performance from dispatch-pool outcomes (done/bounced/rejected)",
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()
			sc, err := s.EngineerScorecards()
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(sc)
				return nil
			}
			printLine("== engineer scorecards (outcome-based) ==")
			for _, e := range sc {
				printLine(fmt.Sprintf("  %-12s done=%d bounced=%d rejected=%d holding=%d", e.Agent, e.Done, e.Bounced, e.Rejected, e.Held))
			}
			if len(sc) == 0 {
				printLine("  (no attributed work yet)")
			}
			return nil
		},
	}
}
