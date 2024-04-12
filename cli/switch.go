package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func switchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "switch [organization]",
		Short: "Switch between organizations",
		Long: `Switch between Dispatch organizations.

The switch command is used to select which organization is used
when running a Dispatch application locally.

To manage your organizations, visit the Dispatch Console: https://console.dispatch.run/`,
		GroupID: "management",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := LoadConfig(DispatchConfigPath)
			if err != nil {
				if !errors.Is(err, os.ErrNotExist) {
					failure(fmt.Sprintf("Failed to load Dispatch configuration: %v", err))
				}
				simple("Please run `dispatch login` to login to Dispatch.")
				return nil
			}

			// List organizations if no arguments were provided.
			if len(args) == 0 {
				fmt.Println("Available organizations:")
				for org := range cfg {
					fmt.Println("-", org)
				}
				return nil
			}

			// Otherwise, try to switch to the specified organization.
			name := args[0]
			_, ok := cfg[name]
			if !ok {
				failure(fmt.Sprintf("Organization '%s' not found", name))

				fmt.Println("Available organizations:")
				for org := range cfg {
					fmt.Println("-", org)
				}
				return nil
			}

			simple(fmt.Sprintf("Switched to organization: %v", name))
			for org, keys := range cfg {
				if keys.Active && org != name {
					keys.Active = false
					cfg[org] = keys
				}
				if org == name {
					keys.Active = true
					cfg[org] = keys
				}
			}
			return CreateConfig(DispatchConfigPath, cfg)
		},
	}
	return cmd
}
