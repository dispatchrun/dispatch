package cli

import (
	"runtime/debug"
	"strings"

	"github.com/spf13/cobra"
	"os/exec"
)

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
	version := "devel "
	output, _ := exec.Command("git", "describe", "--tags", "--abbrev=0").Output()
	if info, ok := debug.ReadBuildInfo(); ok {
		switch info.Main.Version {
		case "":
		case "(devel)":
			version += strings.TrimSpace(string(output)[1:]) + ", build"
		default:
			version = info.Main.Version
		}
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				version += " " + setting.Value[:8]
			}
		}
	}
	return version
}
