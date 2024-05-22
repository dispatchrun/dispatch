package cli

import (
	"fmt"
	"log/slog"
	"path/filepath"

	"github.com/joho/godotenv"
)

var DotEnvFilePath string

func loadEnvFromFile(path string) error {
	if path != "" {
		absolutePath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("failed to get absolute path for %s: %v", path, err)
		}
		if err := godotenv.Load(path); err != nil {
			return fmt.Errorf("failed to load env file from %s: %v", absolutePath, err)
		}
		slog.Info("loading environment variables from file", "path", absolutePath)
	}
	setVariables()
	return nil
}
