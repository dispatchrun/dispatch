//go:build docs

package cli

import (
	"os"
	"path"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

var isDocsBuild = true

func generateDocs(cmd *cobra.Command, filename string, title string) {
	cmd.DisableAutoGenTag = true

	// create docs directory
	_ = os.Mkdir("./docs", 0755)

	err := doc.GenMarkdownTreeCustom(cmd, "./docs",
		func(_ string) string {
			return `---
title: ` + title + `
---

`
		},
		func(name string) string {
			// err := doc.GenMarkdownCustom(cmd, out, func(name string) string {
			base := strings.TrimSuffix(name, path.Ext(name))
			return "/cli/" + strings.ToLower(base) + "/"
		})
	if err != nil {
		panic(err)
	}

	// if command has subcommands, generate markdown for each subcommand
	if cmd.HasSubCommands() {
		for _, c := range cmd.Commands() {
			// if c.Use starts with "help", skip it
			if strings.HasPrefix(c.Use, "help") {
				continue
			}
			generateDocs(c, filename, title+" "+c.Use)
		}
	}
}
