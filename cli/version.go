package cli

import (
	"fmt"
	"runtime/debug"

	"github.com/spf13/cobra"
)

func versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Match dispatch -v,--version output:
			fmt.Println("dispatch version " + version())
			return nil
		},
	}
}

func version() string {
	version := "devel"
	if info, ok := debug.ReadBuildInfo(); ok {
		switch info.Main.Version {
		case "":
		case "(devel)":
		default:
			version = info.Main.Version
		}
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				version += " " + setting.Value
			}
		}
	}
	return version
}
