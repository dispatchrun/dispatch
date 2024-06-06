package cli

import (
	"github.com/spf13/cobra"
)

var Version string
var Revision string

func versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Match dispatch -v,--version output:
			cmd.Println("dispatch version " + version())
			return nil
		},
	}
}

func version() string {
	return Version + ", build " + Revision
}

func InitVersions(version string, revision string) {
	Version = version
	Revision = revision
}
