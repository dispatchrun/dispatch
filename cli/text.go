package cli

import (
	"strings"

	"github.com/muesli/reflow/ansi"
)

func whitespace(width int) string {
	return strings.Repeat(" ", width)
}

func padding(width int, s string) int {
	return width - ansi.PrintableRuneWidth(s)
}

func truncate(width int, s string) string {
	var truncated bool
	for len(s) > 0 && ansi.PrintableRuneWidth(s) > width {
		s = s[:len(s)-1]
		truncated = true
	}
	if truncated {
		s = s + "\033[0m"
	}
	return s
}

func right(width int, s string) string {
	if ansi.PrintableRuneWidth(s) > width {
		return truncate(width-3, s) + "..."
	}
	return whitespace(padding(width, s)) + s
}

func left(width int, s string) string {
	if ansi.PrintableRuneWidth(s) > width {
		return truncate(width-3, s) + "..."
	}
	return s + whitespace(padding(width, s))
}

func join(rows ...string) string {
	var b strings.Builder
	for i, row := range rows {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(row)
	}
	return b.String()
}

func clearANSI(s string) string {
	var isANSI bool
	var b strings.Builder
	for _, c := range s {
		if c == ansi.Marker {
			isANSI = true
		} else if isANSI {
			if ansi.IsTerminator(c) {
				isANSI = false
			}
		} else {
			b.WriteRune(c)
		}
	}
	return b.String()
}
