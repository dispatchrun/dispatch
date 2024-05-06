package cli

import "github.com/charmbracelet/lipgloss"

var (
	defaultColor = lipgloss.NoColor{}

	// See https://www.hackitu.de/termcolor256/
	grayColor    = lipgloss.ANSIColor(102)
	redColor     = lipgloss.ANSIColor(124)
	greenColor   = lipgloss.ANSIColor(34)
	yellowColor  = lipgloss.ANSIColor(142)
	magentaColor = lipgloss.ANSIColor(127)
)
