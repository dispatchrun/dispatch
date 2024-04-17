package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"

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

const defaultConfigPath = "$HOME/.dispatch.toml"

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
		DispatchConfigPath = os.ExpandEnv(defaultConfigPath)
	}
}

type Config map[string]Keys

type Keys struct {
	Active bool   `toml:"active,omitempty"`
	ApiKey string `toml:"api_key"`
}

func CreateConfig(path string, config Config) error {
	fh, err := os.Create(os.ExpandEnv(path))
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer fh.Close()
	return writeConfig(fh, config)
}

func writeConfig(w io.Writer, config Config) error {
	e := toml.NewEncoder(w)
	return e.Encode(config)
}

// TODO: validate configuration to ensure only one organization is active.
func LoadConfig(path string) (Config, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	return loadConfig(bufio.NewReader(fh))
}

func loadConfig(r io.Reader) (Config, error) {
	d := toml.NewDecoder(r)
	f := make(Config)
	if err := d.Decode(&f); err != nil {
		return nil, err
	}
	return f, nil
}

func runConfigFlow() error {
	config, err := LoadConfig(DispatchConfigPath)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("failed to load configuration from %s: %v", DispatchConfigPath, err)
		}
	}

	if len(config) == 1 {
		for org := range config {
			k := config[org]
			k.Active = true
			config[org] = k
		}
	}

	for _, keys := range config {
		if keys.Active {
			DispatchApiKey = keys.ApiKey
			DispatchApiKeyLocation = "config"
		}
	}

	if key := DispatchApiKeyCli; key != "" {
		DispatchApiKey = key
		DispatchApiKeyLocation = "cli"
	}

	if DispatchApiKey == "" {
		if len(config) > 0 {
			return fmt.Errorf("No organization selected. Please run `dispatch switch` to select one.")
		}
		return fmt.Errorf("Please run `dispatch login` to login to Dispatch. Alternatively, provide an --api-key (-k) on the command line.")
	}
	return nil
}
