package cli

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

var versionText = "dispatch version"

func TestVersionCommand(t *testing.T) {
	t.Run("Print the version (test runtime)", func(t *testing.T) {
		t.Parallel()

		cmd := versionCommand()
		stdout := &bytes.Buffer{}
		cmd.SetOut(stdout)

		if err := cmd.Execute(); err != nil {
			t.Fatalf("Received unexpected error: %v", err)
		}

		assert.Equal(t, versionText+" , build \n", stdout.String())
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

		// get git version tag
		cmdGitVersion := exec.Command("git", "describe", "--tags", "--abbrev=0")
		stdoutVersion := &bytes.Buffer{}
		cmdGitVersion.Stdout = stdoutVersion

		if err := cmdGitVersion.Run(); err != nil {
			t.Fatalf("Received unexpected error: %v", err)
		}

		if err := cmdGitHash.Run(); err != nil {
			t.Fatalf("Received unexpected error: %v", err)
		}

		revision := stdout.String()[:8]                             // get the first 8 characters of the git commit hash
		tagVersion := strings.TrimSpace(stdoutVersion.String())[1:] // remove the 'v' prefix
		assert.Equal(t, fmt.Sprintf("%s %s, build %s\n", versionText, tagVersion, revision), stderr.String())
	})
}
