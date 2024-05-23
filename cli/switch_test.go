package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

type (
	testCase struct {
		name          string
		args          []string
		configExists  bool
		configContent string
	}

	expectedOutput struct {
		stdout string
		stderr string
	}
)

func TestSwitchCommand(t *testing.T) {
	tcs := []struct {
		in  testCase
		out expectedOutput
	}{
		{
			in: testCase{
				name:         "Config file doesn't exist",
				args:         []string{"org1"},
				configExists: false,
			},
			out: expectedOutput{
				stdout: "Please run `dispatch login` to login to Dispatch.\n",
			},
		},
		{
			in: testCase{
				name:         "No arguments provided",
				args:         []string{},
				configExists: true,
				configContent: `
	# Warning = 'THIS FILE IS GENERATED. DO NOT EDIT!'
	active = 'x-s-org'
	
	[Organizations]
	[Organizations.x-s-org]
	api_key = 'x'
	`,
			},
			out: expectedOutput{
				stdout: "Available organizations:\n- x-s-org\n",
			},
		},
		{
			in: testCase{
				name:         "Switch to non-existing organization",
				args:         []string{"random"},
				configExists: true,
				configContent: `
	# Warning = 'THIS FILE IS GENERATED. DO NOT EDIT!'
	active = 'x-s-org'
	
	[Organizations]
	[Organizations.x-s-org]
	api_key = 'x'
	`,
			},
			out: expectedOutput{
				stdout: "Organization 'random' not found\n\nAvailable organizations:\n- x-s-org\n",
			},
		},
		{
			in: testCase{
				name:         "Switch to existing organization",
				args:         []string{"x-s-org"},
				configExists: true,
				configContent: `
	# Warning = 'THIS FILE IS GENERATED. DO NOT EDIT!'
	active = 'x-s-org'
	
	[Organizations]
	[Organizations.x-s-org]
	api_key = 'x'
	`,
			},
			out: expectedOutput{
				stdout: "Switched to organization: x-s-org\n",
			},
		},
	}

	for _, tc := range tcs {
		t.Run(tc.in.name, func(t *testing.T) {
			// Set up the config file
			configPath := setupConfig(t, tc.in)

			// Set the DispatchConfigPath (global variable)
			DispatchConfigPath = configPath

			// Create buffer writer
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}

			// Create command in test
			cmd := switchCommand()

			// Set streams for command
			cmd.SetOut(stdout)
			cmd.SetErr(stderr)

			// Set args for command
			cmd.SetArgs(tc.in.args)

			// Execute command
			if err := cmd.Execute(); err != nil {
				t.Fatalf("Received unexpected error: %v", err)
			}

			assert.Equal(t, tc.out.stdout, stdout.String())
			assert.Equal(t, tc.out.stderr, stderr.String())
		})
	}
}

func setupConfig(t *testing.T, tc testCase) string {
	tempDir, err := os.MkdirTemp("", "dispatch-test")
	assert.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	configPath := filepath.Join(tempDir, "config.yaml")
	if tc.configExists {
		err = os.WriteFile(configPath, []byte(tc.configContent), 0600)
		assert.NoError(t, err)
	} else {
		os.Remove(DispatchConfigPath)
	}

	return configPath
}
