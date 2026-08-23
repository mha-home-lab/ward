package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

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
	root.AddCommand(briefCmd(), initCmd(), memoryCmd(), verifyCmd(), routeCmd(), routerCmd(), runCmd(), captureCmd(), taskCmd(), explainCmd(), rejectCmd(), doctorCmd(), workflowCmd(), tickCmd(), harvestCmd(), skillCmd(), versionCmd())
	return root
}

// versionCmd exists because agents type `ward version`, not `ward --version`,
// and a silent exit-1 on a stale/unknown binary is exactly how binary drift
// goes unnoticed (found the hard way during dogfooding).
func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "print the ward version",
		RunE: func(cmd *cobra.Command, args []string) error {
			if jsonOut {
				printJSON(map[string]string{"version": Version})
			} else {
				printLine("ward version " + Version)
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
	return err
}
