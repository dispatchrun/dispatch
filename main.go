package main

import (
	"os"

	"github.com/dispatchrun/dispatch/cli"
)

var (
	Version  string
	Revision string
)

func main() {
	cli.InitVersions(Version, Revision)
	if err := cli.Main(); err != nil {
		// The error is logged by the CLI library.
		// No need to log here too.
		os.Exit(1)
	}
}
