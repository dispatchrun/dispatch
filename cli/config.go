package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

var (
	DispatchApiKey         string
	DispatchApiKeyCli      string
	DispatchApiKeyLocation string

	DispatchApiUrl           string
	DispatchBridgeUrl        string
	DispatchBridgeHostHeader string
	DispatchConsoleUrl       string

	DispatchConfigPath string
)

func init() {
	DispatchApiUrl = os.Getenv("DISPATCH_API_URL")
	if DispatchApiUrl == "" {
		DispatchApiUrl = "https://api.dispatch.run"
	}
	DispatchBridgeUrl = os.Getenv("DISPATCH_BRIDGE_URL")
	if DispatchBridgeUrl == "" {
		DispatchBridgeUrl = "https://bridge.dispatch.run"
	}
	DispatchBridgeHostHeader = os.Getenv("DISPATCH_BRIDGE_HOST_HEADER")

	DispatchConsoleUrl = os.Getenv("DISPATCH_CONSOLE_URL")
	if DispatchConsoleUrl == "" {
		DispatchConsoleUrl = "https://console.dispatch.run"
	}

	if configPath := os.Getenv("DISPATCH_CONFIG_PATH"); configPath != "" {
		DispatchConfigPath = configPath
	} else {
		// https://specifications.freedesktop.org/basedir-spec/basedir-spec-latest.html
		configHome := os.Getenv("XDG_CONFIG_HOME")
		if configHome == "" {
			configHome = "$HOME/.config"
		}
		DispatchConfigPath = filepath.Join(os.ExpandEnv(configHome), "dispatch/config.toml")
	}
}

type Config struct {
	// Warning is printed as a comment at the beginning of the configuration file.
	Warning string `toml:",commented"`

	// Active is the active organization.
	Active string `toml:"active,omitempty"`

	// Organization is the set of organizations and their API keys.
	Organization map[string]Organization `toml:"Organizations"`
}

type Organization struct {
	APIKey string `toml:"api_key"`
}

func CreateConfig(path string, config *Config) error {
	pathdir := filepath.Dir(path)
	if err := os.MkdirAll(pathdir, 0755); err != nil {
		return fmt.Errorf("failed to create config directory %v: %w", pathdir, err)
	}
	fh, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("failed to create config file %v: %w", path, err)
	}
	defer fh.Close()
	return writeConfig(fh, config)
}

func writeConfig(w io.Writer, config *Config) error {
	e := toml.NewEncoder(w)
	return e.Encode(config)
}

// TODO: validate configuration to ensure only one organization is active.
func LoadConfig(path string) (*Config, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	return loadConfig(bufio.NewReader(fh))
}

func loadConfig(r io.Reader) (*Config, error) {
	d := toml.NewDecoder(r)
	var c Config
	if err := d.Decode(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

func runConfigFlow() error {
	config, err := LoadConfig(DispatchConfigPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to load configuration from %s: %v", DispatchConfigPath, err)
		}
	}

	if config.Active != "" {
		org, ok := config.Organization[config.Active]
		if !ok {
			return fmt.Errorf("invalid active organization '%s' found in configuration. Please run `dispatch login` or `dispatch switch`", config.Active)
		}
		DispatchApiKey = org.APIKey
		DispatchApiKeyLocation = "config"
	}

	if key := os.Getenv("DISPATCH_API_KEY"); key != "" {
		DispatchApiKey = key
		DispatchApiKeyLocation = "env"
	}

	if key := DispatchApiKeyCli; key != "" {
		DispatchApiKey = key
		DispatchApiKeyLocation = "cli"
	}

	if DispatchApiKey == "" {
		if len(config.Organization) > 0 {
			return fmt.Errorf("No organization selected. Please run `dispatch switch` to select one.")
		}
		return fmt.Errorf("Please run `dispatch login` to login to Dispatch. Alternatively, set the DISPATCH_API_KEY environment variable, or provide an --api-key (-k) on the command line.")
	}
	return nil
}
