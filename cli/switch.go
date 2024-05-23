package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	SwitchCmdLong = `Switch between Dispatch organizations.

The switch command is used to select which organization is used
when running a Dispatch application locally.
	
To manage your organizations, visit the Dispatch Console: https://console.dispatch.run/`
)

func switchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "switch [organization]",
		Short:   "Switch between organizations",
		Long:    SwitchCmdLong,
		GroupID: "management",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(DispatchConfigPath)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					failure(cmd, fmt.Sprintf("Failed to load Dispatch configuration: %v", err))
				}

				// User must login to create a configuration file.
				simple(cmd, "Please run `dispatch login` to login to Dispatch.")
				return nil
			}

			// List organizations if no arguments were provided.
			if len(args) == 0 {
				simple(cmd, "Available organizations:")
				for org := range cfg.Organization {
					simple(cmd, "-", org)
				}
				return nil
			}

			// Otherwise, try to switch to the specified organization.
			name := args[0]
			_, ok := cfg.Organization[name]
			if !ok {
				failure(cmd, fmt.Sprintf("Organization '%s' not found", name))

				simple(cmd, "Available organizations:")
				for org := range cfg.Organization {
					simple(cmd, "-", org)
				}
				return nil
			}

			simple(cmd, fmt.Sprintf("Switched to organization: %v", name))
			cfg.Active = name
			return CreateConfig(DispatchConfigPath, cfg)
		},
	}
	return cmd
}
