package main

import (
	"fmt"
	"os"

	"github.com/mha-home-lab/ward/internal/cli"
)

func main() {
	root := cli.NewRoot()
	if err := root.Execute(); err != nil {
		// failErr already printed its one line (text or JSON); cobra-level
		// errors (bad flags, unknown commands) would otherwise vanish into a
		// silent exit 1 — print them here, exactly once.
		if !cli.IsPrinted(err) {
			fmt.Fprintln(os.Stderr, "error: "+err.Error())
		}
		os.Exit(1)
	}
}
