package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime/debug"

	"github.com/spf13/cobra"
)

// PrintedError marks an error whose message was ALREADY emitted (by failErr,
// in text or JSON form). main uses it to avoid double-printing; anything else
// reaching main (flag parse errors, unknown commands — which cobra does NOT
// print while SilenceErrors is set) must still surface as a one-line error,
// never a silent exit 1.
type PrintedError struct{ Err error }

func (p PrintedError) Error() string { return p.Err.Error() }
func (p PrintedError) Unwrap() error { return p.Err }

// versionString appends the embedded VCS revision when built inside a git
// repo (go build sets vcs.revision automatically); plain builds report dev.
func versionString() string {
	v := Version
	if bi, ok := debug.ReadBuildInfo(); ok {
		var rev, mod string
		for _, kv := range bi.Settings {
			switch kv.Key {
			case "vcs.revision":
				rev = kv.Value
			case "vcs.modified":
				if kv.Value == "true" {
					mod = "-dirty"
				}
			}
		}
		if len(rev) > 7 {
			v = Version + "-" + rev[:7] + mod
		}
	}
	return v
}

var jsonOut bool

// Version is stamped at build time via -ldflags; default reports dev.
var Version = "dev"

// NewRoot builds the full ward command tree.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:     "ward",
		Short:   "verify-gated model routing for local coding agents",
		Version: Version,
		Long: "WARD routes each unit of work to the cheapest model that can do it\n" +
			"correctly, using verified prior knowledge as a routing signal. Unverified,\n" +
			"stale, or imported artifacts count as a memory MISS and never vote for cheap.\n\n" +
			"Start every session with:  ward brief [topic]",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().BoolVar(&jsonOut, "json", false, "emit JSON instead of human text")
	root.AddCommand(briefCmd(), initCmd(), memoryCmd(), verifyCmd(), routeCmd(), routerCmd(), runCmd(), captureCmd(), taskCmd(), explainCmd(), rejectCmd(), doctorCmd(), workflowCmd(), tickCmd(), harvestCmd(), skillCmd(), timelineCmd(), waveCmd(), scorecardCmd(), syncCmd(), docCmd(), versionCmd())
	return root
}

// versionCmd exists because agents type `ward version`, not `ward --version`,
// and a silent exit-1 on a stale/unknown binary is exactly how binary drift
// goes unnoticed (found the hard way during dogfooding).
func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "print the ward version",
		Example: `  ward version
  ward version --json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOut {
				printJSON(map[string]string{"version": versionString()})
			} else {
				printLine("ward version " + versionString())
			}
			return nil
		},
	}
}

func printJSON(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Fprintln(os.Stdout, string(b))
}

func printLine(s string) {
	fmt.Fprintln(os.Stdout, s)
}

func failErr(err error) error {
	if err == nil {
		return nil
	}
	if jsonOut {
		printJSON(map[string]string{"error": err.Error()})
	} else {
		fmt.Fprintln(os.Stderr, "error: "+err.Error())
	}
	return PrintedError{Err: err}
}

// IsPrinted reports whether the error was already emitted to the user.
func IsPrinted(err error) bool {
	var p PrintedError
	return errors.As(err, &p)
}
