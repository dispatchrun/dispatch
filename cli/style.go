package cli

import (
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	dialogBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#874BFD")).
			Margin(1, 2).
			Padding(1, 2).
			BorderTop(true).
			BorderLeft(true).
			BorderRight(true).
			BorderBottom(true)

	successStyle = lipgloss.NewStyle().Foreground(greenColor)

	failureStyle = lipgloss.NewStyle().Foreground(redColor)
)

type errMsg struct{ error }

type resultMsg struct{ string }

type spinnerModel struct {
	spinner spinner.Model
	exit    bool
	err     error
	hello   string
	result  string
	fn      func() (tea.Msg, error)
}

func newSpinnerModel(hello string, fn func() (tea.Msg, error)) spinnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	return spinnerModel{
		spinner: s,
		hello:   hello,
		fn:      fn,
	}
}

func runSpinner(fn func() (tea.Msg, error)) tea.Cmd {
	return func() tea.Msg {
		result, err := fn()
		if err != nil {
			return errMsg{err}
		}
		if result == nil {
			return resultMsg{}
		}
		return resultMsg{result.(string)}
	}
}

func (m spinnerModel) Init() tea.Cmd {
	return tea.Batch(
		m.spinner.Tick,
		runSpinner(m.fn),
	)
}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			m.exit = true
			return m, tea.Quit
		default:
			return m, nil
		}
	case errMsg:
		m.err = msg
		return m, tea.Quit
	case resultMsg:
		m.result = msg.string
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m spinnerModel) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %s\n", m.err.Error())
	}
	if m.result != "" {
		return m.result
	}
	str := fmt.Sprintf("%s %s...press q to quit\n", m.spinner.View(), m.hello)
	if m.exit {
		return str + "\n"
	}
	return str
}

func success(msg string) {
	fmt.Println(successStyle.Render(msg))
}

func failure(msg string) {
	fmt.Println(failureStyle.Render(msg) + "\n")
}

func simple(msg string) {
	fmt.Println(msg)
}

func dialog(msg string, args ...interface{}) {
	fmt.Println(dialogBoxStyle.Render(fmt.Sprintf(msg, args...)))
}
