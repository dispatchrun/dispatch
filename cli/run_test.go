package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

var dispatchBinary = filepath.Join("../build", runtime.GOOS, runtime.GOARCH, "dispatch")

func TestRunCommand(t *testing.T) {
	t.Run("Run with non-existent env file", func(t *testing.T) {
		t.Parallel()

		buff, err := execRunCommand(&[]string{}, "run", "--env-file", "non-existent.env", "--", "echo", "hello")
		if err != nil {
			t.Fatal(err.Error())
		}

		errMsg := "no such file or directory\n"
		path := regexp.QuoteMeta(filepath.FromSlash("/dispatch/cli/non-existent.env"))
		if runtime.GOOS == "windows" {
			errMsg = "The system cannot find the file specified.\n"
		}
		assert.Regexp(t, "Error: failed to load env file from .+"+path+": open non-existent\\.env: "+errMsg, buff.String())
	})

	t.Run("Run with env file", func(t *testing.T) {
		t.Parallel()

		envFile, err := createEnvFile(t.TempDir(), []byte("CHARACTER=rick_sanchez"))
		defer os.Remove(envFile)
		if err != nil {
			t.Fatalf("Failed to write env file: %v", err)
		}

		buff, err := execRunCommand(&[]string{}, "run", "--env-file", envFile, "--", "printenv", "CHARACTER")
		if err != nil {
			t.Fatal(err.Error())
		}

		result, found := findEnvVariableInLogs(&buff)
		if !found {
			t.Fatalf("Expected printenv in the output: %s", buff.String())
		}
		assert.Equal(t, "rick_sanchez", result, fmt.Sprintf("Expected 'printenv | rick_sanchez' in the output, got 'printenv | %s'", result))
	})

	t.Run("Run with env variable", func(t *testing.T) {
		t.Parallel()

		// Set environment variables
		envVars := []string{"CHARACTER=morty_smith"}

		buff, err := execRunCommand(&envVars, "run", "--", "printenv", "CHARACTER")
		if err != nil {
			t.Fatal(err.Error())
		}

		result, found := findEnvVariableInLogs(&buff)
		if !found {
			t.Fatalf("Expected printenv in the output: %s", buff.String())
		}
		assert.Equal(t, "morty_smith", result, fmt.Sprintf("Expected 'printenv | morty_smith' in the output, got 'printenv | %s'", result))
	})

	t.Run("Run with env variable in command line has priority over the one in the env file", func(t *testing.T) {
		t.Parallel()

		envFile, err := createEnvFile(t.TempDir(), []byte("CHARACTER=rick_sanchez"))
		defer os.Remove(envFile)
		if err != nil {
			t.Fatalf("Failed to write env file: %v", err)
		}

		// Set environment variables
		envVars := []string{"CHARACTER=morty_smith"}
		buff, err := execRunCommand(&envVars, "run", "--env-file", envFile, "--", "printenv", "CHARACTER")
		if err != nil {
			t.Fatal(err.Error())
		}

		result, found := findEnvVariableInLogs(&buff)
		if !found {
			t.Fatalf("Expected printenv in the output: %s", buff.String())
		}
		assert.Equal(t, "morty_smith", result, fmt.Sprintf("Expected 'printenv | morty_smith' in the output, got 'printenv | %s'", result))
	})

	t.Run("Run with env variable in local env vars has priority over the one in the env file", func(t *testing.T) {
		// Do not use t.Parallel() here as we are manipulating the environment!

		// Set environment variables
		os.Setenv("CHARACTER", "morty_smith")
		defer os.Unsetenv("CHARACTER")

		envFile, err := createEnvFile(t.TempDir(), []byte("CHARACTER=rick_sanchez"))
		defer os.Remove(envFile)

		if err != nil {
			t.Fatalf("Failed to write env file: %v", err)
		}

		buff, err := execRunCommand(&[]string{}, "run", "--env-file", envFile, "--", "printenv", "CHARACTER")
		if err != nil {
			t.Fatal(err.Error())
		}

		result, found := findEnvVariableInLogs(&buff)
		if !found {
			t.Fatalf("Expected printenv in the output: %s\n\n", buff.String())
		}
		assert.Equal(t, "morty_smith", result, fmt.Sprintf("Expected 'printenv | morty_smith' in the output, got 'printenv | %s'", result))
	})
}

func execRunCommand(envVars *[]string, arg ...string) (bytes.Buffer, error) {
	// Create a context with a timeout to ensure the process doesn't run indefinitely
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// add the api key to the arguments so the command can run without `dispatch login` being run first
	arg = append(arg[:1], append([]string{"--api-key", "00000000"}, arg[1:]...)...)

	// Set up the command
	cmd := exec.CommandContext(ctx, dispatchBinary, arg...)

	if len(*envVars) != 0 {
		// Set environment variables
		cmd.Env = append(os.Environ(), *envVars...)
	}

	// Capture the standard error
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf

	// Start the command
	if err := cmd.Start(); err != nil {
		return errBuf, fmt.Errorf("Failed to start command: %w", err)
	}

	// Wait for the command to finish or for the context to timeout
	// We use Wait() instead of Run() so that we can capture the error
	// For example:
	//    FOO=bar ./build/darwin/amd64/dispatch run -- printenv FOO
	// This will exit with
	//    Error: command 'printenv FOO' exited unexpectedly
	// but also it will print...
	//    printenv | bar
	// to the logs and that is exactly what we want to test
	// If context timeout occurs, than something went wrong
	// and `dispatch run` is running indefinitely.
	if err := cmd.Wait(); err != nil {
		// Check if the error is due to context timeout (command running too long)
		if ctx.Err() == context.DeadlineExceeded {
			return errBuf, fmt.Errorf("Command timed out: %w", err)
		}
	}

	return errBuf, nil
}

func createEnvFile(path string, content []byte) (string, error) {
	envFile := filepath.Join(path, "test.env")
	err := os.WriteFile(envFile, content, 0600)
	return envFile, err
}

func findEnvVariableInLogs(buf *bytes.Buffer) (string, bool) {
	var result string
	found := false

	// Split the log into lines
	lines := strings.Split(buf.String(), "\n")

	// Iterate over each line and check for the condition
	for _, line := range lines {
		if strings.Contains(line, "printenv | ") {
			result = strings.Split(line, "printenv | ")[1]
			found = true
			break
		}
	}
	return result, found
}
