package cli

import (
	"bytes"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
)

var versionText = "dispatch version devel"

func TestVersionCommand(t *testing.T) {
	t.Run("Print the version (test runtime)", func(t *testing.T) {
		t.Parallel()

		cmd := versionCommand()
		stdout := &bytes.Buffer{}
		cmd.SetOut(stdout)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("Received unexpected error: %v", err)
		}

		assert.Equal(t, versionText+"\n", stdout.String())
	})

	t.Run("Print the version (binary)", func(t *testing.T) {
		t.Parallel()

		cmd := exec.Command(dispatchBinary, "version")
		stderr := &bytes.Buffer{}
		cmd.Stderr = stderr

		if err := cmd.Run(); err != nil {
			t.Fatalf("Received unexpected error: %v", err)
		}

		// get git commit hash
		cmdGitHash := exec.Command("git", "rev-parse", "HEAD")
		stdout := &bytes.Buffer{}
		cmdGitHash.Stdout = stdout

		if err := cmdGitHash.Run(); err != nil {
			t.Fatalf("Received unexpected error: %v", err)
		}

		version := stdout.String()

		assert.Equal(t, versionText+" "+version, stderr.String())
	})
}
