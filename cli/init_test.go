package cli

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInitCommand(t *testing.T) {
	t.Run("directoryExists returns false for non-existent directory", func(t *testing.T) {
		t.Parallel()

		result, _ := directoryExists("nonexistentdirectory")
		assert.False(t, result)
	})

	t.Run("directoryExists returns true for existing directory", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()

		result, _ := directoryExists(tempDir)
		assert.True(t, result)
	})

	t.Run("directoryExists returns false for file", func(t *testing.T) {
		t.Parallel()

		tempFile := t.TempDir() + "/tempfile"
		_, err := os.Create(tempFile)
		assert.Nil(t, err)

		result, _ := directoryExists(tempFile)
		assert.False(t, result)

		os.Remove(tempFile)
	})

	t.Run("isDirectoryEmpty returns true for empty directory", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()

		result, _ := isDirectoryEmpty(tempDir)
		assert.True(t, result)
	})

	t.Run("isDirectoryEmpty returns false for non-empty directory", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()

		tempFile := tempDir + "/tempfile"
		_, err := os.Create(tempFile)
		assert.Nil(t, err)

		result, _ := isDirectoryEmpty(tempDir)
		assert.False(t, result)

		os.Remove(tempFile)
	})
}
