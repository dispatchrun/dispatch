//go:build !docs

package cli

import "github.com/spf13/cobra"

var isDocsBuild = false

func generateDocs(_ *cobra.Command, _ string, _ string) {
	// do nothing if the build tag "docs" is not set
}
