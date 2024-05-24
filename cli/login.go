package cli

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os/exec"
	"runtime"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
)

func loginCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Login to Dispatch",
		Long: `Login to Dispatch.

The login command will open a browser window where you can create a Dispatch
account or login to an existing account.

After authenticating with Dispatch, the API key will be persisted locally.`,
		GroupID: "management",
		RunE: func(cmd *cobra.Command, args []string) error {
			token, err := generateToken()
			if err != nil {
				return err
			}

			_ = open(fmt.Sprintf("%s/cli-login?token=%s", DispatchConsoleUrl, token))

			dialog(`Opening the browser for you to sign in to Dispatch.

If the browser does not open, please visit the following URL:

%s`, DispatchConsoleUrl+"/cli-login?token="+token)

			console := &console{}

			var loginErr error
			var loggedIn bool

			p := tea.NewProgram(newSpinnerModel("Logging in...", func() (tea.Msg, error) {
				if err := console.Login(token); err != nil {
					loginErr = err
					return nil, err
				}
				loggedIn = true
				return nil, nil
			}))
			if _, err = p.Run(); err != nil {
				return err
			}

			if loginErr != nil {
				failure(cmd, "Authentication failed. Please contact support at support@dispatch.run")
				fmt.Printf("Error: %s\n", loginErr)
			} else if loggedIn {
				success("Authentication successful")
				fmt.Printf(
					"Configuration file created at %s\n",
					DispatchConfigPath,
				)
			}
			return nil
		},
	}
	return cmd
}

func generateToken() (string, error) {
	bytes := make([]byte, 32)
	_, err := rand.Read(bytes)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

func open(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start"}
	case "darwin":
		cmd = "open"
	default: // "linux", "freebsd", "openbsd", "netbsd"
		cmd = "xdg-open"
	}
	args = append(args, url)
	return exec.Command(cmd, args...).Start()
}
