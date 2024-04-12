package cli

import (
	"fmt"
	"net/http"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func verificationCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "verification",
		Short: "Manage verification keys",
		Long: `Manage Dispatch verification keys.

Verification keys are used by your Dispatch applications to verify that
function calls were sent by Dispatch.

See the documentation for more information:
  https://docs.dispatch.run/dispatch/getting-started/production-deployment
`,
		GroupID: "management",
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:          "rollout",
		Short:        "Rollout a new verification key",
		SilenceUsage: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return runConfigFlow()
		},
		RunE: rolloutKey,
	})
	cmd.AddCommand(&cobra.Command{
		Use:          "get",
		Short:        "Get the active verification key",
		SilenceUsage: true,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return runConfigFlow()
		},
		RunE: getKey,
	})
	return cmd
}

type ListSigningKeys struct {
	Keys []Key `json:"keys"`
}

type SigningKey struct {
	Key Key `json:"key"`
}

type Key struct {
	SigningKeyID  string `json:"signingKeyId"`
	AsymmetricKey struct {
		PublicKey string `json:"publicKey"`
	} `json:"asymmetricKey"`
}

// TODO: create better output for created signing key
func rolloutKey(cmd *cobra.Command, args []string) error {
	// TODO: instantiate the api in main?
	api := &dispatchApi{client: http.DefaultClient, apiKey: DispatchApiKey}

	fn := func() (tea.Msg, error) {
		skey, err := api.CreateSigningKey()
		if err != nil {
			return "", fmt.Errorf("failed to create key: %w", err)
		}
		return fmt.Sprintf("New key:\n\n%s", skey.Key.AsymmetricKey.PublicKey), nil
	}

	p := tea.NewProgram(newSpinnerModel("Creating a new verification key", fn))
	_, err := p.Run()
	return err
}

// TODO: build table from keys
func getKey(cmd *cobra.Command, args []string) error {
	// TODO: instantiate the api in main?
	api := &dispatchApi{client: http.DefaultClient, apiKey: DispatchApiKey}

	fn := func() (tea.Msg, error) {
		skeys, err := api.ListSigningKeys()
		if err != nil {
			return "", fmt.Errorf("failed to list keys: %w", err)
		}
		if len(skeys.Keys) == 0 {
			return "", fmt.Errorf("Key not found. Use `dispatch verification rollout` to create the first key.")
		}
		return skeys.Keys[0].AsymmetricKey.PublicKey, nil
	}

	p := tea.NewProgram(newSpinnerModel("Fetching active verification key", fn))
	_, err := p.Run()
	return err
}
