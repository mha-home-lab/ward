package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var jsonOut bool

// NewRoot builds the full ward command tree.
func NewRoot() *cobra.Command {
	root := &cobra.Command{
		Use:   "ward",
		Short: "verify-gated model routing for local coding agents",
		Long: "WARD routes each unit of work to the cheapest model that can do it\n" +
			"correctly, using verified prior knowledge as a routing signal. Unverified,\n" +
			"stale, or imported artifacts count as a memory MISS and never vote for cheap.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().BoolVar(&jsonOut, "json", false, "emit JSON instead of human text")
	root.AddCommand(initCmd(), memoryCmd(), verifyCmd(), routeCmd(), routerCmd(), runCmd(), doctorCmd(), workflowCmd())
	return root
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
