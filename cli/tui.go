package cli

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	sdkv1 "buf.build/gen/go/stealthrocket/dispatch-proto/protocolbuffers/go/dispatch/sdk/v1"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const refreshInterval = time.Second / 2

var (
	grayColor   = lipgloss.Color("#7D7D7D")
	whiteColor  = lipgloss.Color("#FFFFFF")
	redColor    = lipgloss.Color("#FF0000")
	greenColor  = lipgloss.Color("#00FF00")
	yellowColor = lipgloss.Color("#FFAA00")

	pendingStyle = lipgloss.NewStyle().Foreground(grayColor)
	retryStyle   = lipgloss.NewStyle().Foreground(yellowColor)
	errorStyle   = lipgloss.NewStyle().Foreground(redColor)
	okStyle      = lipgloss.NewStyle().Foreground(greenColor)

	spinnerStyle = lipgloss.NewStyle().Foreground(grayColor)
	statusStyle  = lipgloss.NewStyle().Foreground(grayColor)
	treeStyle    = lipgloss.NewStyle().Foreground(grayColor)

	logoStyle           = lipgloss.NewStyle().Foreground(whiteColor)
	logoUnderscoreStyle = lipgloss.NewStyle().Foreground(greenColor)

	viewportStyle = lipgloss.NewStyle().Margin(1, 2)
)

type DispatchID string

type TUI struct {
	mu sync.Mutex

	roots        map[DispatchID]struct{}
	orderedRoots []DispatchID

	nodes map[DispatchID]node

	spinner  spinner.Model
	viewport viewport.Model
	help     help.Model
	ready    bool

	keys      []key.Binding
	activeTab tab
	tail      bool

	logs   bytes.Buffer
	logsMu sync.Mutex
}

type tab int

const (
	functionsTab tab = iota
	logsTab
)

const tabCount = 2

type node struct {
	function string
	failures int
	status   sdkv1.Status
	error    error
	done     bool

	creationTime   time.Time
	expirationTime time.Time

	calls            map[string]int
	outstandingCalls int

	children        map[DispatchID]struct{}
	orderedChildren []DispatchID
}

var _ tea.Model = (*TUI)(nil)

type tickMsg struct{}

func tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

func (t *TUI) Init() tea.Cmd {
	t.spinner = spinner.New()
	t.help = help.New()
	// t.viewport is initialized on the first tea.WindowSizeMsg

	t.keys = []key.Binding{
		key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch tabs"),
		),
		key.NewBinding(
			key.WithKeys("t"),
			key.WithHelp("t", "tail"),
		),
		key.NewBinding(
			key.WithKeys("q", "ctrl+c", "esc"),
			key.WithHelp("q", "quit"),
		),
	}

	t.tail = true
	t.activeTab = functionsTab

	return tea.Batch(t.spinner.Tick, tick())
}

func (t *TUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tickMsg:
		cmds = append(cmds, tick())
	case spinner.TickMsg:
		t.spinner, cmd = t.spinner.Update(msg)
		cmds = append(cmds, cmd)
	case tea.WindowSizeMsg:
		height := msg.Height - 1 // reserve space for help
		width := msg.Width
		if !t.ready {
			t.viewport = viewport.New(width, height)
			t.viewport.Style = viewportStyle
			t.ready = true
		} else {
			t.viewport.Width = width
			t.viewport.Height = height
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q", "esc":
			return t, tea.Quit
		case "t":
			t.tail = true
		case "tab":
			t.activeTab = (t.activeTab + 1) % tabCount
		case "up", "down", "left", "right", "pgup", "pgdown", "ctrl+u", "ctrl+d":
			t.tail = false
		}
	case tea.MouseMsg:
		if msg.Action == tea.MouseActionPress && (msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown) {
			t.tail = false
		}
	}
	t.viewport, cmd = t.viewport.Update(msg)
	cmds = append(cmds, cmd)
	return t, tea.Batch(cmds...)
}

// https://patorjk.com/software/taag/ (Larry 3D)
var dispatchAscii = []string{
	logoStyle.Render("  __                                __           __"),
	logoStyle.Render(" /\\ \\  __                          /\\ \\__       /\\ \\                   "),
	logoStyle.Render(" \\_\\ \\/\\_\\    ____  _____      __  \\ \\ ,_\\   ___\\ \\ \\___               "),
	logoStyle.Render(" /'_` \\/\\ \\  /',__\\/\\ '__`\\  /'__`\\ \\ \\ \\/  /'___\\ \\  _ `\\             "),
	logoStyle.Render("/\\ \\L\\ \\ \\ \\/\\__, `\\ \\ \\L\\ \\/\\ \\L\\.\\_\\ \\ \\_/\\ \\__/\\ \\ \\ \\ \\") + logoUnderscoreStyle.Render("  _______ "),
	logoStyle.Render("\\ \\___,_\\ \\_\\/\\____/\\ \\ ,__/\\ \\__/.\\_\\\\ \\__\\ \\____\\\\ \\_\\ \\_\\") + logoUnderscoreStyle.Render("/\\______\\"),
	logoStyle.Render(" \\/__,_ /\\/_/\\/___/  \\ \\ \\/  \\/__/\\/_/ \\/__/\\/____/ \\/_/\\/_/") + logoUnderscoreStyle.Render("\\/______/"),
	logoStyle.Render("                      \\ \\_\\                                  "),
	logoStyle.Render("                       \\/_/                                  "),
	"",
}

func (t *TUI) View() string {
	if !t.ready {
		return statusStyle.Render(strings.Join(append(dispatchAscii, "Initializing..."), "\n"))
	}

	switch t.activeTab {
	case functionsTab:
		t.viewport.SetContent(t.render(time.Now()))
	case logsTab:
		t.viewport.SetContent(t.logs.String())
	}

	// Tail the output, unless the user has tried to scroll back.
	if t.tail {
		t.viewport.GotoBottom()
	}

	return t.viewport.View() + "\n" + t.help.ShortHelpView(t.keys)
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
	if _, ok := t.roots[rootID]; !ok {
		t.roots[rootID] = struct{}{}
		t.orderedRoots = append(t.orderedRoots, rootID)
	}
	root, ok := t.nodes[rootID]
	if !ok {
		root = node{}
	}
	t.nodes[rootID] = root

	// Upsert the node.
	n, ok := t.nodes[id]
	if !ok {
		n = node{}
	}
	n.function = req.Function
	n.error = nil // clear previous error
	n.status = 0  // clear previous status
	if req.CreationTime != nil {
		n.creationTime = req.CreationTime.AsTime()
	}
	if n.creationTime.IsZero() {
		n.creationTime = time.Now()
	}
	if req.ExpirationTime != nil {
		n.expirationTime = req.ExpirationTime.AsTime()
	}
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
				parent.outstandingCalls--
			}
			parent.children[id] = struct{}{}
			parent.orderedChildren = append(parent.orderedChildren, id)
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
		if res.Status != sdkv1.Status_STATUS_OK {
			n.failures++
		}
		// TODO: wipe in-memory state if INCOMPATIBLE_STATE status is observed?
		switch d := res.Directive.(type) {
		case *sdkv1.RunResponse_Exit:
			n.status = res.Status
			n.done = terminalStatus(res.Status)
			if d.Exit.TailCall != nil {
				n = node{function: d.Exit.TailCall.Function} // reset
			}
			// TODO: show result (output value / error)?
		case *sdkv1.RunResponse_Poll:
			if n.calls == nil {
				n.calls = map[string]int{}
			}
			for _, call := range d.Poll.Calls {
				n.calls[call.Function]++
				n.outstandingCalls++
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

func (t *TUI) Write(b []byte) (int, error) {
	t.logsMu.Lock()
	defer t.logsMu.Unlock()

	return t.logs.Write(b)
}

func (t *TUI) parseID(id string) DispatchID {
	// TODO: [16]byte
	return DispatchID(id)
}

func (t *TUI) render(now time.Time) string {
	t.mu.Lock()
	defer t.mu.Unlock()

	if len(t.roots) == 0 {
		return statusStyle.Render(strings.Join(append(dispatchAscii, "Waiting for function calls..."), "\n"))
	}

	var b strings.Builder
	var i int
	for _, rootID := range t.orderedRoots {
		if i > 0 {
			b.WriteByte('\n')
		}
		t.renderTo(now, rootID, nil, &b)
		i++
	}

	return b.String()
}

func (t *TUI) renderTo(now time.Time, id DispatchID, isLast []bool, b *strings.Builder) {
	// t.mu must be locked.
	n := t.nodes[id]

	// Print the tree prefix.
	for i, last := range isLast {
		var s string
		if i == len(isLast)-1 {
			if last {
				s = "└─"
			} else {
				s = "├─"
			}
		} else {
			if last {
				s = "  "
			} else {
				s = "│ "
			}
		}
		b.WriteString(treeStyle.Render(s))
		b.WriteByte(' ')
	}

	// Determine what to print, based on the status of the function call.
	var functionStyle lipgloss.Style
	var errorCauseStyle lipgloss.Style
	showError := false
	showSpinner := false
	if n.done {
		if n.status == sdkv1.Status_STATUS_OK {
			functionStyle = okStyle
		} else {
			functionStyle = errorStyle
			errorCauseStyle = errorStyle
			showError = true
		}
	} else if !n.expirationTime.IsZero() && n.expirationTime.Before(now) {
		n.error = errors.New("expired")
		functionStyle = errorStyle
		errorCauseStyle = errorStyle
		showError = true
	} else {
		functionStyle = pendingStyle
		if n.failures > 0 {
			functionStyle = retryStyle
			errorCauseStyle = retryStyle
			showError = true
		}
		showSpinner = true
	}

	// Render the function call.
	if n.function != "" {
		b.WriteString(functionStyle.Render(n.function))
	} else {
		b.WriteString(functionStyle.Render("<?>"))
	}
	// TODO: parse/show arguments?
	if showError && (n.error != nil || n.status != sdkv1.Status_STATUS_UNSPECIFIED) {
		b.WriteString(statusStyle.Render(" ("))
		if n.error != nil {
			b.WriteString(errorCauseStyle.Render(n.error.Error()))
		} else if n.status != sdkv1.Status_STATUS_UNSPECIFIED {
			b.WriteString(errorCauseStyle.Render(statusString(n.status)))
		}
		b.WriteString(statusStyle.Render(")"))
	}
	if showSpinner {
		b.WriteByte(' ')
		b.WriteString(spinnerStyle.Render(t.spinner.View()))
	}

	b.WriteByte('\n')

	// Recursively render children.
	for i, id := range n.orderedChildren {
		last := i == len(n.orderedChildren)-1
		t.renderTo(now, id, append(isLast[:len(isLast):len(isLast)], last), b)
	}

	// FIXME: hard to render calls before we know the Dispatch ID..
	//  We either need correlation ID on RunRequest, or dispatch_ids on
	//  PollResult after making calls
	//
	// for function, count := range n.calls {
	// 	for i := 0; i < count; i++ {
	// 		for i := 0; i < childIndent; i++ {
	// 			b.WriteByte(' ')
	// 		}
	// 		b.WriteString(pendingStyle.Render(function))
	// 		b.WriteByte(' ')
	// 		b.WriteString(spinnerStyle.Render(t.spinner.View()))
	// 		b.WriteByte('\n')
	// 	}
	// }
}

func statusString(status sdkv1.Status) string {
	switch status {
	case sdkv1.Status_STATUS_OK:
		return "ok"
	case sdkv1.Status_STATUS_TIMEOUT:
		return "timeout"
	case sdkv1.Status_STATUS_THROTTLED:
		return "throttled"
	case sdkv1.Status_STATUS_INVALID_ARGUMENT:
		return "invalid response"
	case sdkv1.Status_STATUS_TEMPORARY_ERROR:
		return "temporary error"
	case sdkv1.Status_STATUS_PERMANENT_ERROR:
		return "permanent error"
	case sdkv1.Status_STATUS_INCOMPATIBLE_STATE:
		return "incompatible state"
	case sdkv1.Status_STATUS_DNS_ERROR:
		return "DNS error"
	case sdkv1.Status_STATUS_TCP_ERROR:
		return "TCP error"
	case sdkv1.Status_STATUS_TLS_ERROR:
		return "TLS error"
	case sdkv1.Status_STATUS_HTTP_ERROR:
		return "HTTP error"
	case sdkv1.Status_STATUS_UNAUTHENTICATED:
		return "unauthenticated"
	case sdkv1.Status_STATUS_PERMISSION_DENIED:
		return "permission denied"
	case sdkv1.Status_STATUS_NOT_FOUND:
		return "not found"
	default:
		return status.String()
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
