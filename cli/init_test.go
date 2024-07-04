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
		file, err := os.Create(tempFile)
		assert.Nil(t, err)

		result, _ := directoryExists(tempFile)
		assert.False(t, result)

		// Clean up
		err = file.Close()
		assert.Nil(t, err)
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
		file, err := os.Create(tempFile)
		assert.Nil(t, err)

		result, _ := isDirectoryEmpty(tempDir)
		assert.False(t, result)

		// Clean up
		err = file.Close()
		assert.Nil(t, err)
	})

	t.Run("downloadAndExtractTemplates downloads and extracts templates", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()

		err := downloadAndExtractTemplates(tempDir)
		assert.Nil(t, err)

		// Check if the templates directory was created
		result, _ := isDirectoryEmpty(tempDir)
		assert.False(t, result)
	})

	t.Run("getLatestCommitSHA returns the latest commit SHA", func(t *testing.T) {
		t.Parallel()

		sha, err := getLatestCommitSHA("https://api.github.com/repos/dispatchrun/dispatch/branches/main")
		assert.Nil(t, err)
		assert.Regexp(t, "^[a-f0-9]{40}$", sha)
	})

	t.Run("getLatestCommitSHA returns an error for invalid URL", func(t *testing.T) {
		t.Parallel()

		_, err := getLatestCommitSHA("invalidurl")
		assert.NotNil(t, err)
	})

	t.Run("copyFile copies file", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()

		src := tempDir + "/srcfile"
		dest := tempDir + "/destfile"

		file, err := os.Create(src)
		assert.Nil(t, err)

		err = copyFile(src, dest)
		assert.Nil(t, err)

		_, err = os.Stat(dest)
		assert.Nil(t, err)

		// Clean up
		err = file.Close()
		assert.Nil(t, err)
	})

	t.Run("copyDir copies directory", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()

		src := tempDir + "/srcdir"
		dest := tempDir + "/destdir"

		err := os.Mkdir(src, 0755)
		assert.Nil(t, err)

		err = copyDir(src, dest)
		assert.Nil(t, err)

		_, err = os.Stat(dest)
		assert.Nil(t, err)
	})

	t.Run("readDirectories returns all subdirectories", func(t *testing.T) {
		t.Parallel()

		tempDir := t.TempDir()

		dir1 := tempDir + "/dir1"
		err := os.Mkdir(dir1, 0755)
		assert.Nil(t, err)

		dir2 := tempDir + "/dir2"
		err = os.Mkdir(dir2, 0755)
		assert.Nil(t, err)

		dirs, err := readDirectories(tempDir)
		assert.Nil(t, err)
		assert.ElementsMatch(t, []string{"dir1", "dir2"}, dirs)
	})
}
