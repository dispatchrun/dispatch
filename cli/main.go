package cli

import (
	"context"

	"github.com/spf13/cobra"
)

// Main is the entry point of the command line.
func Main() error {
	cmd := &cobra.Command{
		Version: version(),
		Use:     "dispatch",
		Long: `Welcome to Dispatch!

To get started, use the login command to authenticate with Dispatch or create an account.

Documentation: https://docs.dispatch.run
Discord: https://dispatch.run/discord
Support: support@dispatch.run
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.PersistentFlags().StringVarP(&DispatchApiKeyCli, "api-key", "k", "", "Dispatch API key (env: DISPATCH_API_KEY)")

	cmd.AddGroup(&cobra.Group{
		ID:    "management",
		Title: "Account Management Commands:",
	})
	cmd.AddGroup(&cobra.Group{
		ID:    "dispatch",
		Title: "Dispatch Commands:",
	})

	cmd.AddCommand(loginCommand())
	cmd.AddCommand(switchCommand())
	cmd.AddCommand(runCommand())
	cmd.AddCommand(verificationCommand())
	cmd.AddCommand(versionCommand())
	return cmd.ExecuteContext(context.Background())
}
