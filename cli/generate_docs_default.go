//go:build !docs

package cli

import "github.com/spf13/cobra"

const DispatchCmdLong = `Welcome to Dispatch!

To get started, use the login command to authenticate with Dispatch or create an account.

Documentation: https://docs.dispatch.run
Discord: https://dispatch.run/discord
Support: support@dispatch.run
`

const RunExampleText = "  dispatch run [options] -- <command>"

func generateDocs(_ *cobra.Command, _ string) {
	// do nothing if the build tag "docs" is not set
}
