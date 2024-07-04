package cli

import (
	"context"

	"github.com/spf13/cobra"
)

var (
	DispatchCmdLong = `Welcome to Dispatch!

To get started, use the login command to authenticate with Dispatch or create an account.

Documentation: https://docs.dispatch.run
Discord: https://dispatch.run/discord
Support: support@dispatch.run
`
)

func createMainCommand() *cobra.Command {
	cmd := &cobra.Command{
		Version: version(),
		Use:     "dispatch",
		Long:    DispatchCmdLong,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return loadEnvFromFile(DotEnvFilePath)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	cmd.PersistentFlags().StringVarP(&DispatchApiKeyCli, "api-key", "k", "", "Dispatch API key (env: DISPATCH_API_KEY)")
	cmd.PersistentFlags().StringVarP(&DotEnvFilePath, "env-file", "", "", "Path to .env file")

	cmd.AddGroup(&cobra.Group{
		ID:    "management",
		Title: "Account Management Commands:",
	})
	cmd.AddGroup(&cobra.Group{
		ID:    "dispatch",
		Title: "Dispatch Commands:",
	})

	// Passing the global variables to the commands make testing in parallel possible.
	cmd.AddCommand(loginCommand())
	cmd.AddCommand(initCommand())
	cmd.AddCommand(switchCommand(DispatchConfigPath))
	cmd.AddCommand(verificationCommand())
	cmd.AddCommand(runCommand())
	cmd.AddCommand(versionCommand())

	return cmd
}

// Main is the entry point of the command line.
func Main() error {
	return createMainCommand().ExecuteContext(context.Background())
}
