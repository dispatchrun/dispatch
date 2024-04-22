package cli

import (
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	sdkv1 "buf.build/gen/go/stealthrocket/dispatch-proto/protocolbuffers/go/dispatch/sdk/v1"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	grayColor = lipgloss.Color("#7D7D7D")

	redColor    = lipgloss.Color("#FF0000")
	greenColor  = lipgloss.Color("#00FF00")
	yellowColor = lipgloss.Color("#FFAA00")

	pendingStyle = lipgloss.NewStyle().Foreground(grayColor)
	retryStyle   = lipgloss.NewStyle().Foreground(yellowColor)
	errorStyle   = lipgloss.NewStyle().Foreground(redColor)
	okStyle      = lipgloss.NewStyle().Foreground(greenColor)

	errorCauseStyle = lipgloss.NewStyle().Foreground(grayColor)
	spinnerStyle    = lipgloss.NewStyle().Foreground(grayColor)
)

type DispatchID string

type TUI struct {
	mu sync.Mutex

	roots map[DispatchID]struct{}
	nodes map[DispatchID]node

	spinner spinner.Model
}

type node struct {
	function string
	failures int
	status   sdkv1.Status
	error    error
	done     bool

	calls    map[string]int
	children map[DispatchID]struct{}
}

var _ tea.Model = (*TUI)(nil)

type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(time.Second/10, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

func (t *TUI) Init() tea.Cmd {
	t.spinner = spinner.New()
	return tea.Batch(t.spinner.Tick, tick())
}

func (t *TUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return t, tick()
	case spinner.TickMsg:
		var cmd tea.Cmd
		t.spinner, cmd = t.spinner.Update(msg)
		return t, cmd
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return t, tea.Quit
		}
	}
	return t, nil
}

func (t *TUI) View() string {
	return t.render()
}

func (t *TUI) ObserveRequest(req *sdkv1.RunRequest) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.roots == nil {
		t.roots = map[DispatchID]struct{}{}
	}
	if t.nodes == nil {
		t.nodes = map[DispatchID]node{}
	}

	rootID := t.parseID(req.RootDispatchId)
	parentID := t.parseID(req.ParentDispatchId)
	id := t.parseID(req.DispatchId)

	// Upsert the root.
	t.roots[rootID] = struct{}{}
	root, ok := t.nodes[rootID]
	if !ok {
		root = node{}
	}
	t.nodes[rootID] = root

	// TODO: setup expiry for the root

	// Upsert the node.
	n, ok := t.nodes[id]
	if !ok {
		n = node{}
	}
	n.function = req.Function
	t.nodes[id] = n

	// Upsert the parent and link its child, if applicable.
	if parentID != "" {
		parent, ok := t.nodes[parentID]
		if !ok {
			parent = node{}
			if parentID != rootID {
				panic("not implemented")
			}
		}
		if parent.children == nil {
			parent.children = map[DispatchID]struct{}{}
		}
		if _, ok := parent.children[id]; !ok {
			if n, ok := parent.calls[req.Function]; ok && n > 0 {
				parent.calls[req.Function] = n - 1
			}
			parent.children[id] = struct{}{}
		}
		t.nodes[parentID] = parent
	}
}

func (t *TUI) ObserveResponse(req *sdkv1.RunRequest, err error, httpRes *http.Response, res *sdkv1.RunResponse) {
	t.mu.Lock()
	defer t.mu.Unlock()

	id := t.parseID(req.DispatchId)
	n := t.nodes[id]

	if res != nil {
		n.status = res.Status
		if res.Status != sdkv1.Status_STATUS_OK {
			n.failures++
		}
		if res.Status == sdkv1.Status_STATUS_INCOMPATIBLE_STATE {
			// TODO: wipe state?
		}
		switch d := res.Directive.(type) {
		case *sdkv1.RunResponse_Exit:
			n.done = terminalStatus(res.Status)
			if d.Exit.TailCall != nil {
				n = node{function: d.Exit.TailCall.Function} // reset
			} else {
				// TODO: show result?
			}
		case *sdkv1.RunResponse_Poll:
			if n.calls == nil {
				n.calls = map[string]int{}
			}
			for _, call := range d.Poll.Calls {
				n.calls[call.Function]++
			}
		}
	} else if httpRes != nil {
		n.failures++
		n.error = fmt.Errorf("unexpected HTTP status code %d", httpRes.StatusCode)
		n.done = terminalHTTPStatusCode(httpRes.StatusCode)
	} else if err != nil {
		n.failures++
		n.error = err
	}

	t.nodes[id] = n
}

func (t *TUI) parseID(id string) DispatchID {
	// TODO: [16]byte
	return DispatchID(id)
}

func (t *TUI) render() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	var b strings.Builder
	var i int
	for rootID := range t.roots {
		if i > 0 {
			b.WriteByte('\n')
		}
		t.renderTo(rootID, 0, &b)
		i++
	}

	return b.String()
}

func (t *TUI) renderTo(id DispatchID, indent int, b *strings.Builder) {
	// t.mu must be locked.

	n := t.nodes[id]

	function := "(?)"
	if n.function != "" {
		function = n.function
	}
	for i := 0; i < indent; i++ {
		b.WriteByte(' ')
	}

	// TODO: show function call arguments?

	if n.done {
		if n.status == sdkv1.Status_STATUS_OK {
			b.WriteString(okStyle.Render(function))
		} else {
			b.WriteString(errorStyle.Render(function))
			if n.error != nil {
				b.WriteString(errorCauseStyle.Render(fmt.Sprintf(" (%s)", n.error.Error())))
			} else {
				b.WriteString(errorCauseStyle.Render(fmt.Sprintf(" (%s)", n.status)))
			}
		}
	} else {
		style := pendingStyle
		if n.failures > 0 {
			style = retryStyle
		}
		b.WriteString(style.Render(function))

		if n.failures > 0 {
			if n.error != nil {
				b.WriteString(errorCauseStyle.Render(fmt.Sprintf(" (%s)", n.error.Error())))
			} else {
				b.WriteString(errorCauseStyle.Render(fmt.Sprintf(" (%s)", n.status)))
			}
		}
		b.WriteByte(' ')
		b.WriteString(spinnerStyle.Render(t.spinner.View()))
	}

	b.WriteByte('\n')

	childIndent := indent + 2
	for id := range n.children {
		t.renderTo(id, childIndent, b)
	}

	for function, count := range n.calls {
		for i := 0; i < count; i++ {
			for i := 0; i < childIndent; i++ {
				b.WriteByte(' ')
			}
			b.WriteString(pendingStyle.Render(function))
			b.WriteByte(' ')
			b.WriteString(spinnerStyle.Render(t.spinner.View()))
			b.WriteByte('\n')
		}
	}
}

func terminalStatus(status sdkv1.Status) bool {
	switch status {
	case sdkv1.Status_STATUS_TIMEOUT,
		sdkv1.Status_STATUS_THROTTLED,
		sdkv1.Status_STATUS_TEMPORARY_ERROR,
		sdkv1.Status_STATUS_INCOMPATIBLE_STATE,
		sdkv1.Status_STATUS_DNS_ERROR,
		sdkv1.Status_STATUS_TCP_ERROR,
		sdkv1.Status_STATUS_TLS_ERROR,
		sdkv1.Status_STATUS_HTTP_ERROR:
		return false
	default:
		return true
	}
}

func terminalHTTPStatusCode(code int) bool {
	switch code / 100 {
	case 4:
		return code != http.StatusRequestTimeout && code != http.StatusTooManyRequests
	case 5:
		return code == http.StatusNotImplemented
	default:
		return true
	}
}
