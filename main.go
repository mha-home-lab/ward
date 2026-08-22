package main

import (
	"os"

	"github.com/mha-home-lab/ward/internal/cli"
)

func main() {
	root := cli.NewRoot()
	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
