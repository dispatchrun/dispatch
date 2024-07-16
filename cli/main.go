package cli

import (
	"context"

	"github.com/spf13/cobra"
)

func createMainCommand() *cobra.Command {
	cmd := &cobra.Command{
		Version: version(),
		Use:     "dispatch",
		Long:    DispatchCmdLong,
		Short:   "Main command for Dispatch CLI",
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

	// Generate markdown documentation
	generateDocs(cmd, "dispatch")

	return cmd
}

// Main is the entry point of the command line.
func Main() error {
	return createMainCommand().ExecuteContext(context.Background())
}
