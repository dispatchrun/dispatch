//go:build docs

package cli

import (
	"bytes"
	"os"
	"path"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

const DispatchCmdLong = "This is the main command for Dispatch CLI. Add a subcommand to make it useful."

const RunExampleText = "```\ndispatch run [options] -- <command>\n```"

func generateDocs(cmd *cobra.Command, title string) {
	cmd.DisableAutoGenTag = true

	// create docs directory
	_ = os.Mkdir("./docs", 0755)

	out := new(bytes.Buffer)

	err := doc.GenMarkdownCustom(cmd, out, func(name string) string {
		// err := doc.GenMarkdownCustom(cmd, out, func(name string) string {
		base := strings.TrimSuffix(name, path.Ext(name))
		return "/cli/" + strings.ToLower(base) + "/"
	})
	if err != nil {
		panic(err)
	}

	// Define the text to be replaced and the replacement text
	oldText := []byte("## " + title)
	newText := []byte("---\ntitle: " + title + "\n---")

	// Perform the replacement on the buffer's content
	updatedContent := bytes.Replace(out.Bytes(), oldText, newText, 1)

	// Reset the buffer and write the updated content back to it
	out.Reset()
	out.Write(updatedContent)

	// write markdown to file
	file, err := os.Create("./docs/" + strings.ReplaceAll(title, " ", "_") + ".md")
	if err != nil {
		panic(err)
	}

	_, err = file.Write(out.Bytes())
	if err != nil {
		panic(err)
	}

	defer file.Close()

	// if command has subcommands, generate markdown for each subcommand
	if cmd.HasSubCommands() {
		for _, c := range cmd.Commands() {
			// if c.Use starts with "help", skip it
			if c.Name() == "help" {
				continue
			}
			generateDocs(c, title+" "+c.Name())
		}
	}
}
