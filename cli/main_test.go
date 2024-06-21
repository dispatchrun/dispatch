package cli

import (
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
)

var expectedCommands = []string{"login", "switch [organization]", "verification", "run", "version", "init <template> [path]"}

func TestMainCommand(t *testing.T) {
	t.Run("Main command", func(t *testing.T) {
		t.Parallel()

		cmd := createMainCommand()
		assert.NotNil(t, cmd, "Expected main command to be created")

		groups := cmd.Groups()
		assert.Len(t, groups, 2, "Expected 2 groups")
		assert.Equal(t, "management", groups[0].ID, "Expected first group to be 'management'")
		assert.Equal(t, "dispatch", groups[1].ID, "Expected second group to be 'dispatch'")

		commands := cmd.Commands()

		// Extract the command IDs
		commandIDs := make([]string, 0, len(commands))
		for _, command := range commands {
			commandIDs = append(commandIDs, command.Use)
		}

		// Sort slices alphabetically
		sort.Strings(expectedCommands)
		assert.Equal(t, expectedCommands, commandIDs, "All commands should be present in the main command")
	})
}
