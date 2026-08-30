package cli

import (
	"fmt"
	"strings"
	"time"

	"github.com/mha-home-lab/ward/internal/store"
	"github.com/spf13/cobra"
)

// kpisCmd (control-scorecard, build order P2): routing-control telemetry for
// the "verified memory enables cheaper routing" thesis. Cheap-hit = the route
// was cheap AND the attempt ran successfully (execution_success=1); a failed
// cheap attempt is a cheap MISS, not a hit. Deliberately outcome-first — this
// never zeroes in on an individual engineer (that is ward scorecard's job).
func kpisCmd() *cobra.Command {
	var window string
	cmd := &cobra.Command{
		Use:   "kpis",
		Short: "routing-control telemetry: cheap-hit, escalation, memory-miss rates",
		Example: `  ward kpis
  ward kpis --window 7d
  ward kpis --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s, err := store.Open()
			if err != nil {
				return failErr(err)
			}
			defer s.DB.Close()

			since := ""
			if window != "" {
				d, err := parseWindow(window)
				if err != nil {
					return failErr(err)
				}
				since = time.Now().UTC().Add(-d).Format("2006-01-02T15:04:05Z")
			}
			r, err := s.RoutingKPIs(since)
			if err != nil {
				return failErr(err)
			}
			if jsonOut {
				printJSON(r)
				return nil
			}
			printLine("== routing-control KPIs ==")
			printLine(fmt.Sprintf("  window       %s → %s", r.WindowFrom, r.WindowTo))
			printLine(fmt.Sprintf("  decisions    %d (cheap %d, cheap+success %d)", r.Total, r.Cheap, r.CheapSuccess))
			printLine(fmt.Sprintf("  cheap-hit    %.1f%%", r.CheapHitRate))
			printLine(fmt.Sprintf("  escalations  %.1f%%", r.EscalationRate))
			printLine(fmt.Sprintf("  verify pass  %.1f%%", r.VerifyPassRate))
			printLine(fmt.Sprintf("  memory miss  %.1f%%", r.MissRate))
			if r.Total == 0 {
				printLine("  (no routing decisions in window — run a workflow to populate)")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&window, "window", "", "window (e.g. 24h, 7d, 2w); empty = all history")
	return cmd
}

// parseWindow turns a human window ("24h", "7d", "2w") into a duration; bare
// Go durations pass through untouched.
func parseWindow(w string) (time.Duration, error) {
	w = strings.TrimSpace(w)
	switch {
	case strings.HasSuffix(w, "w"):
		h, err := time.ParseDuration(strings.TrimSuffix(w, "w") + "h")
		if err != nil {
			return 0, err
		}
		return h * 24 * 7, nil
	case strings.HasSuffix(w, "d"):
		h, err := time.ParseDuration(strings.TrimSuffix(w, "d") + "h")
		if err != nil {
			return 0, err
		}
		return h * 24, nil
	}
	return time.ParseDuration(w)
}
