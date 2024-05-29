package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

		// Create a context with a timeout to ensure the process doesn't run indefinitely
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		// Set up the command
		cmd := exec.CommandContext(ctx, dispatchBinary, "run", "--env-file", "non-existent.env", "--", "echo", "hello")

		// Capture the standard error
		var errBuf bytes.Buffer
		cmd.Stderr = &errBuf

		// Start the command
		if err := cmd.Start(); err != nil {
			t.Fatalf("Failed to start command: %v", err)
		}

		// Wait for the command to finish or for the context to timeout
		if err := cmd.Wait(); err != nil {
			// Check if the error is due to context timeout (command running too long)
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatalf("Command timed out")
			}
		}

		assert.Regexp(t, "Error: failed to load env file from .+: open .+: no such file or directory\n", errBuf.String())
	})
	t.Run("Run with env file", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		envFile := filepath.Join(tempDir, "test.env")
		err := os.WriteFile(envFile, []byte("RICK_SANCHEZ=pickle"), 0600)
		if err != nil {
			t.Fatalf("Failed to write env file: %v", err)
		}

		// Create a context with a timeout to ensure the process doesn't run indefinitely
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		// Set up the command
		cmd := exec.CommandContext(ctx, dispatchBinary, "run", "--env-file", envFile, "--", "printenv", "RICK_SANCHEZ")

		// Capture the standard error
		var errBuf bytes.Buffer
		cmd.Stderr = &errBuf

		// Start the command
		if err := cmd.Start(); err != nil {
			t.Fatalf("Failed to start command: %v", err)
		}

		// Wait for the command to finish or for the context to timeout
		if err := cmd.Wait(); err != nil {
			// Check if the error is due to context timeout (command running too long)
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatalf("Command timed out")
			}
		}

		var result string
		found := false
		// Split the log into lines
		lines := strings.Split(errBuf.String(), "\n")
		// Iterate over each line and check for the condition
		for _, line := range lines {
			if strings.Contains(line, "printenv | ") {
				result = strings.Split(line, "printenv | ")[1]
				found = true
				break
			}
		}
		if !found {
			t.Fatal("Expected printenv in the output")
		}
		assert.Equal(t, "pickle", result, fmt.Sprintf("Expected 'printenv | pickle' in the output, got 'printenv | %s'", result))
	})
	t.Run("Run with env variable", func(t *testing.T) {
		t.Parallel()

		// Create a context with a timeout to ensure the process doesn't run indefinitely
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// Set up the command
		cmd := exec.CommandContext(ctx, dispatchBinary, "run", "--", "printenv", "MORTY_SMITH")

		// Set environment variables
		cmd.Env = append(os.Environ(), "MORTY_SMITH=evil_morty")

		// Capture the standard error
		var errBuf bytes.Buffer
		cmd.Stderr = &errBuf

		// Start the command
		if err := cmd.Start(); err != nil {
			t.Fatalf("Failed to start command: %v", err)
		}

		// Wait for the command to finish or for the context to timeout
		if err := cmd.Wait(); err != nil {
			// Check if the error is due to context timeout (command running too long)
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatalf("Command timed out")
			}
		}

		var result string
		found := false
		// Split the log into lines
		lines := strings.Split(errBuf.String(), "\n")
		// Iterate over each line and check for the condition
		for _, line := range lines {
			if strings.Contains(line, "printenv | ") {
				result = strings.Split(line, "printenv | ")[1]
				found = true
				break
			}
		}
		if !found {
			t.Fatal("Expected printenv in the output")
		}
		assert.Equal(t, "evil_morty", result, fmt.Sprintf("Expected 'printenv | evil_morty' in the output, got 'printenv | %s'", result))
	})

	t.Run("Run with env variable in command line has priority over the one in the env file", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()
		envFile := filepath.Join(tempDir, "test.env")
		err := os.WriteFile(envFile, []byte("RICK_SANCHEZ=pickle"), 0600)
		if err != nil {
			t.Fatalf("Failed to write env file: %v", err)
		}

		// Create a context with a timeout to ensure the process doesn't run indefinitely
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()

		// Set up the command
		cmd := exec.CommandContext(ctx, dispatchBinary, "run", "--env-file", envFile, "--", "printenv", "RICK_SANCHEZ")

		// Set environment variables
		cmd.Env = append(os.Environ(), "RICK_SANCHEZ=not_pickle")

		// Capture the standard error
		var errBuf bytes.Buffer
		cmd.Stderr = &errBuf

		// Start the command
		if err := cmd.Start(); err != nil {
			t.Fatalf("Failed to start command: %v", err)
		}

		// Wait for the command to finish or for the context to timeout
		if err := cmd.Wait(); err != nil {
			// Check if the error is due to context timeout (command running too long)
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatalf("Command timed out")
			}
		}

		var result string
		found := false
		// Split the log into lines
		lines := strings.Split(errBuf.String(), "\n")
		// Iterate over each line and check for the condition
		for _, line := range lines {
			if strings.Contains(line, "printenv | ") {
				result = strings.Split(line, "printenv | ")[1]
				found = true
				break
			}
		}
		if !found {
			t.Fatal("Expected 'printenv | pickle' in the output")
		}
		assert.True(t, found, fmt.Sprintf("Expected 'printenv | not_pickle' in the output, got 'printenv | %s'", result))
	})
	t.Run("Run with env variable in local env vars has priority over the one in the env file", func(t *testing.T) {
		// Do not use t.Parallel() here as we are manipulating the environment!

		tempDir := t.TempDir()
		envFile := filepath.Join(tempDir, "test.env")
		err := os.WriteFile(envFile, []byte("RICK_SANCHEZ=pickle"), 0600)
		if err != nil {
			t.Fatalf("Failed to write env file: %v", err)
		}

		// Set environment variables
		os.Setenv("RICK_SANCHEZ", "not_pickle")

		// Create a context with a timeout to ensure the process doesn't run indefinitely
		ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		defer os.Unsetenv("RICK_SANCHEZ")

		// Set up the command
		cmd := exec.CommandContext(ctx, dispatchBinary, "run", "--env-file", envFile, "--", "printenv", "RICK_SANCHEZ")

		// Capture the standard error
		var errBuf bytes.Buffer
		cmd.Stderr = &errBuf

		// Start the command
		if err := cmd.Start(); err != nil {
			t.Fatalf("Failed to start command: %v", err)
		}

		// Wait for the command to finish or for the context to timeout
		if err := cmd.Wait(); err != nil {
			// Check if the error is due to context timeout (command running too long)
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatalf("Command timed out")
			}
		}

		var result string
		found := false
		// Split the log into lines
		lines := strings.Split(errBuf.String(), "\n")
		// Iterate over each line and check for the condition
		for _, line := range lines {
			if strings.Contains(line, "printenv | ") {
				result = strings.Split(line, "printenv | ")[1]
				found = true
				break
			}
		}
		if !found {
			t.Fatal("Expected in the output")
		}
		assert.True(t, found, fmt.Sprintf("Expected 'printenv | not_pickle' in the output, got 'printenv | %s'", result))
	})
}
