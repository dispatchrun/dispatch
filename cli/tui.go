package cli

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	sdkv1 "buf.build/gen/go/stealthrocket/dispatch-proto/protocolbuffers/go/dispatch/sdk/v1"
	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/ansi"
)

const (
	refreshInterval         = time.Second / 10
	underscoreBlinkInterval = time.Second / 2
)

var (
	// Style for the viewport that contains everything.
	viewportStyle = lipgloss.NewStyle().Margin(1, 2)

	// Styles for the dispatch_ ASCII logo.
	logoStyle           = lipgloss.NewStyle().Foreground(defaultColor)
	logoUnderscoreStyle = lipgloss.NewStyle().Foreground(greenColor)

	// Style for the table of function calls.
	tableHeaderStyle = lipgloss.NewStyle().Foreground(defaultColor).Bold(true)
	selectedStyle    = lipgloss.NewStyle().Background(magentaColor)

	// Styles for function names and statuses in the table.
	pendingStyle = lipgloss.NewStyle().Foreground(grayColor)
	retryStyle   = lipgloss.NewStyle().Foreground(yellowColor)
	errorStyle   = lipgloss.NewStyle().Foreground(redColor)
	okStyle      = lipgloss.NewStyle().Foreground(greenColor)

	// Styles for other components inside the table.
	treeStyle = lipgloss.NewStyle().Foreground(grayColor)
)

const (
	pendingIcon = "•" // U+2022
	successIcon = "✔" // U+2714
	failureIcon = "✗" // U+2718
)

type DispatchID string

type TUI struct {
	ticks uint64

	// Storage for the function call hierarchies. Each function call
	// has a "root" node, and nodes can have zero or more children.
	//
	// FIXME: we never clean up items from these maps
	roots        map[DispatchID]struct{}
	orderedRoots []DispatchID
	nodes        map[DispatchID]node

	// Storage for logs.
	logs bytes.Buffer

	// TUI models / options / flags, used to display the information
	// above.
	viewport         viewport.Model
	selection        textinput.Model
	help             help.Model
	ready            bool
	activeTab        tab
	selectMode       bool
	tailMode         bool
	logsTabHelp      string
	functionsTabHelp string
	selectHelp       string
	windowHeight     int

	mu sync.Mutex
}

type tab int

const (
	functionsTab tab = iota
	logsTab
)

const tabCount = 2

var (
	tabKey = key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch tab"),
	)

	selectKey = key.NewBinding(
		key.WithKeys("s"),
		key.WithHelp("s", "select"),
	)

	tailKey = key.NewBinding(
		key.WithKeys("t"),
		key.WithHelp("t", "tail"),
	)

	quitKey = key.NewBinding(
		key.WithKeys("q", "ctrl+c", "esc"),
		key.WithHelp("q", "quit"),
	)

	exitSelectKey = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "exit select"),
	)

	quitInSelectKey = key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	)

	functionsTabKeyMap = []key.Binding{tabKey, selectKey, quitKey}
	logsTabKeyMap      = []key.Binding{tabKey, tailKey, quitKey}
	selectKeyMap       = []key.Binding{tabKey, exitSelectKey, quitInSelectKey}
)

type node struct {
	function string

	failures  int
	responses int

	status sdkv1.Status
	error  error

	running   bool
	suspended bool
	done      bool

	creationTime   time.Time
	expirationTime time.Time
	doneTime       time.Time

	children        map[DispatchID]struct{}
	orderedChildren []DispatchID
}

type tickMsg struct{}

func tick() tea.Cmd {
	// The TUI isn't in the driver's seat. Instead, we have the layer
	// up coordinating the interactions between the Dispatch API and
	// the local application. The layer up notifies the TUI of changes
	// via the FunctionCallObserver interface.
	//
	// To keep the TUI up to date, we have a ticker that sends messages
	// at a fixed interval.
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg {
		return tickMsg{}
	})
}

type focusSelectMsg struct{}

func focusSelect() tea.Msg {
	return focusSelectMsg{}
}

func (t *TUI) Init() tea.Cmd {
	// Note that t.viewport is initialized on the first tea.WindowSizeMsg.
	t.help = help.New()

	t.selection = textinput.New()
	t.selection.Focus() // input is visibile iff t.selectMode == true

	t.selectMode = false
	t.tailMode = true

	t.activeTab = functionsTab
	t.logsTabHelp = t.help.ShortHelpView(logsTabKeyMap)
	t.functionsTabHelp = t.help.ShortHelpView(functionsTabKeyMap)
	t.selectHelp = t.help.ShortHelpView(selectKeyMap)

	return tick()
}

func (t *TUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Here we handle "messages" such as key presses, window size changes,
	// refresh ticks, etc. Note that the TUI view is updated after messages
	// have been processed.
	var cmd tea.Cmd
	var cmds []tea.Cmd
	switch msg := msg.(type) {
	case tickMsg:
		t.ticks++
		cmds = append(cmds, tick())

	case focusSelectMsg:
		t.selectMode = true
		t.selection.SetValue("")

	case tea.WindowSizeMsg:
		// Initialize or resize the viewport.
		t.windowHeight = msg.Height
		height := msg.Height - 1 // reserve space for status bar
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
		if t.selectMode {
			switch msg.String() {
			case "esc":
				t.selectMode = false
			case "tab":
				t.selectMode = false
				t.activeTab = (t.activeTab + 1) % tabCount
			case "ctrl+c":
				return t, tea.Quit
			}
		} else {
			switch msg.String() {
			case "ctrl+c", "q", "esc":
				return t, tea.Quit
			case "s":
				cmds = append(cmds, focusSelect, textinput.Blink)
			case "t":
				t.tailMode = true
			case "tab":
				t.selectMode = false
				t.activeTab = (t.activeTab + 1) % tabCount
			case "up", "down", "left", "right", "pgup", "pgdown", "ctrl+u", "ctrl+d":
				t.tailMode = false
			}
		}
	}

	// Forward messages to the text input in select mode.
	if t.selectMode {
		t.selection, cmd = t.selection.Update(msg)
		cmds = append(cmds, cmd)
	}

	// Forward messages to the viewport, e.g. for scroll-back support.
	t.viewport, cmd = t.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return t, tea.Batch(cmds...)
}

// https://patorjk.com/software/taag/ (Ogre)
var dispatchAscii = []string{
	`     _ _                 _       _`,
	`  __| (_)___ _ __   __ _| |_ ___| |__`,
	` / _' | / __| '_ \ / _' | __/ __| '_ \`,
	`| (_| | \__ \ |_) | (_| | || (__| | | |`,
	` \__,_|_|___/ .__/ \__,_|\__\___|_| |_|`,
	`            |_|`,
}

var underscoreAscii = []string{
	" _____",
	"|_____|",
}

const underscoreIndex = 3

func (t *TUI) View() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	var viewportContent string
	var statusBarContent string
	helpContent := t.functionsTabHelp
	if !t.ready {
		viewportContent = t.logoView()
		statusBarContent = "Initializing..."
	} else {
		switch t.activeTab {
		case functionsTab:
			if len(t.roots) == 0 {
				viewportContent = t.logoView()
				statusBarContent = "Waiting for function calls..."
			} else {
				viewportContent = t.functionsView(time.Now())
				if len(t.nodes) == 1 {
					statusBarContent = "1 total function call"
				} else {
					statusBarContent = fmt.Sprintf("%d total function calls", len(t.nodes))
				}
				var inflightCount int
				for _, n := range t.nodes {
					if !n.done {
						inflightCount++
					}
				}
				statusBarContent += fmt.Sprintf(", %d in-flight", inflightCount)
			}
			if t.selectMode {
				statusBarContent = t.selection.View()
				helpContent = t.selectHelp
			}
		case logsTab:
			viewportContent = t.logs.String()
			helpContent = t.logsTabHelp
		}
	}

	t.viewport.SetContent(viewportContent)

	// Tail the output, unless the user has tried
	// to scroll back (e.g. with arrow keys).
	if t.tailMode {
		t.viewport.GotoBottom()
	}

	// Shrink the viewport so it contains the content and status bar only.
	t.viewport.Height = min(t.viewport.TotalLineCount()+1, t.windowHeight-1)

	var b strings.Builder
	b.WriteString(t.viewport.View())
	b.WriteByte('\n')
	if statusBarContent != "" {
		b.WriteString("  ")
		b.WriteString(statusBarContent)
		b.WriteString("\n\n")
	}
	b.WriteString("  ")
	b.WriteString(helpContent)
	return b.String()
}

func (t *TUI) logoView() string {
	showUnderscore := (t.ticks/uint64(underscoreBlinkInterval/refreshInterval))%2 == 0

	var b strings.Builder
	for i, line := range dispatchAscii {
		b.WriteString(logoStyle.Render(line))
		if showUnderscore {
			if i >= underscoreIndex && i-underscoreIndex < len(underscoreAscii) {
				b.WriteString(logoUnderscoreStyle.Render(underscoreAscii[i-underscoreIndex]))
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func (t *TUI) ObserveRequest(req *sdkv1.RunRequest) {
	// ObserveRequest is part of the FunctionCallObserver interface.
	// It's called after a request has been received from the Dispatch API,
	// and before the request has been sent to the local application.

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
	n.running = true
	n.suspended = false
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
			parent.children[id] = struct{}{}
			parent.orderedChildren = append(parent.orderedChildren, id)
		}
		t.nodes[parentID] = parent
	}
}

func (t *TUI) ObserveResponse(req *sdkv1.RunRequest, err error, httpRes *http.Response, res *sdkv1.RunResponse) {
	// ObserveResponse is part of the FunctionCallObserver interface.
	// It's called after a response has been received from the local
	// application, and before the response has been sent to Dispatch.

	t.mu.Lock()
	defer t.mu.Unlock()

	id := t.parseID(req.DispatchId)
	n := t.nodes[id]

	n.responses++
	n.error = nil
	n.status = 0
	n.running = false

	if res != nil {
		switch res.Status {
		case sdkv1.Status_STATUS_OK:
			// noop
		case sdkv1.Status_STATUS_INCOMPATIBLE_STATE:
			n = node{function: n.function} // reset
		default:
			n.failures++
		}

		switch d := res.Directive.(type) {
		case *sdkv1.RunResponse_Exit:
			n.status = res.Status
			n.done = terminalStatus(res.Status)
			if d.Exit.TailCall != nil {
				n = node{function: d.Exit.TailCall.Function} // reset
			} else if res.Status != sdkv1.Status_STATUS_OK && d.Exit.Result != nil {
				if e := d.Exit.Result.Error; e != nil && e.Type != "" {
					if e.Message == "" {
						n.error = fmt.Errorf("%s", e.Type)
					} else {
						n.error = fmt.Errorf("%s: %s", e.Type, e.Message)
					}
				}
			}
		case *sdkv1.RunResponse_Poll:
			n.suspended = true
		}
	} else if httpRes != nil {
		n.failures++
		n.error = fmt.Errorf("unexpected HTTP status code %d", httpRes.StatusCode)
		n.done = terminalHTTPStatusCode(httpRes.StatusCode)
	} else if err != nil {
		n.failures++
		n.error = err
	}

	if n.done && n.doneTime.IsZero() {
		n.doneTime = time.Now()
	}

	t.nodes[id] = n
}

func (t *TUI) Write(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.logs.Write(b)
}

func (t *TUI) Read(b []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	return t.logs.Read(b)
}

func (t *TUI) parseID(id string) DispatchID {
	return DispatchID(id)
}

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

func (t *TUI) functionsView(now time.Time) string {
	// Render function calls in a hybrid table/tree view.
	var b strings.Builder
	var rows rowBuffer
	for i, rootID := range t.orderedRoots {
		if i > 0 {
			b.WriteByte('\n')
		}

		// Buffer rows in memory.
		t.buildRows(now, rootID, nil, &rows)

		// Dynamically size the function call tree column.
		maxFunctionWidth := 0
		for i := range rows.rows {
			maxFunctionWidth = max(maxFunctionWidth, ansi.PrintableRuneWidth(rows.rows[i].function))
		}
		functionColumnWidth := max(9, min(50, maxFunctionWidth))

		// Render the table.
		b.WriteString(t.tableHeaderView(functionColumnWidth))
		for i := range rows.rows {
			b.WriteString(t.tableRowView(&rows.rows[i], functionColumnWidth))
		}

		rows.reset()
	}
	return b.String()
}

type row struct {
	id       int
	function string
	attempts int
	elapsed  time.Duration
	icon     string
	status   string
}

type rowBuffer struct {
	rows []row
	seq  int
}

func (b *rowBuffer) add(r row) {
	b.seq++
	r.id = b.seq
	b.rows = append(b.rows, r)
}

func (b *rowBuffer) reset() {
	b.rows = b.rows[:0]
}

func (t *TUI) tableHeaderView(functionColumnWidth int) string {
	columns := []string{
		left(functionColumnWidth, tableHeaderStyle.Render("Function")),
		right(8, tableHeaderStyle.Render("Attempts")),
		right(10, tableHeaderStyle.Render("Duration")),
		left(1, pendingIcon),
		left(40, tableHeaderStyle.Render("Status")),
	}
	if t.selectMode {
		idWidth := int(math.Log10(float64(len(t.nodes)))) + 1
		columns = append([]string{left(idWidth, strings.Repeat("#", idWidth))}, columns...)
	}
	return join(columns...)
}

func (t *TUI) tableRowView(r *row, functionColumnWidth int) string {
	attemptsStr := strconv.Itoa(r.attempts)

	var elapsedStr string
	if r.elapsed > 0 {
		elapsedStr = r.elapsed.String()
	} else {
		elapsedStr = "?"
	}

	values := []string{
		left(functionColumnWidth, r.function),
		right(8, attemptsStr),
		right(10, elapsedStr),
		left(1, r.icon),
		left(40, r.status),
	}

	id := strconv.Itoa(r.id)
	if t.selectMode {
		idWidth := int(math.Log10(float64(len(t.nodes)))) + 1
		paddedID := left(idWidth, id)
		if input := t.selection.Value(); input != "" && strings.Contains(id, input) {
			paddedID = selectedStyle.Render(paddedID)
		}
		values = append([]string{paddedID}, values...)
	}
	return join(values...)
}

func join(rows ...string) string {
	var b strings.Builder
	for i, row := range rows {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(row)
	}
	b.WriteByte('\n')
	return b.String()
}

func (t *TUI) buildRows(now time.Time, id DispatchID, isLast []bool, rows *rowBuffer) {
	// t.mu must be locked!

	n := t.nodes[id]

	// Render the tree prefix.
	var function strings.Builder
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
		function.WriteString(treeStyle.Render(s))
		function.WriteByte(' ')
	}

	// Determine what to print, based on the status of the function call.
	var style lipgloss.Style
	icon := pendingIcon
	if n.running || n.suspended {
		style = pendingStyle
	} else if n.done {
		if n.status == sdkv1.Status_STATUS_OK {
			style = okStyle
			icon = successIcon
		} else {
			style = errorStyle
			icon = failureIcon
		}
	} else if !n.expirationTime.IsZero() && n.expirationTime.Before(now) {
		n.error = errors.New("Expired")
		style = errorStyle
		n.done = true
		n.doneTime = n.expirationTime
		icon = failureIcon
	} else if n.failures > 0 {
		style = retryStyle
	} else {
		style = pendingStyle
	}

	// Render the function name.
	if n.function != "" {
		function.WriteString(style.Render(n.function))
	} else {
		function.WriteString(style.Render("(?)"))
	}

	// Render the status and icon.
	var status string
	if n.running {
		status = "Running"
	} else if n.suspended {
		status = "Suspended"
	} else if n.error != nil {
		status = n.error.Error()
	} else if n.status != sdkv1.Status_STATUS_UNSPECIFIED {
		status = statusString(n.status)
	} else {
		status = "Pending"
	}
	status = style.Render(status)
	icon = style.Render(icon)

	attempts := n.failures
	if n.running {
		attempts++
	} else if n.done && n.status == sdkv1.Status_STATUS_OK {
		attempts++
	} else if n.responses > n.failures {
		attempts++
	}
	attempts = max(attempts, 1)

	var elapsed time.Duration
	if !n.creationTime.IsZero() {
		var tail time.Time
		if !n.done {
			tail = now
		} else {
			tail = n.doneTime
		}
		elapsed = tail.Sub(n.creationTime).Truncate(time.Millisecond)
	}

	rows.add(row{
		function: function.String(),
		attempts: attempts,
		elapsed:  elapsed,
		icon:     icon,
		status:   status,
	})

	// Recursively render children.
	for i, id := range n.orderedChildren {
		last := i == len(n.orderedChildren)-1
		t.buildRows(now, id, append(isLast[:len(isLast):len(isLast)], last), rows)
	}
}

func statusString(status sdkv1.Status) string {
	switch status {
	case sdkv1.Status_STATUS_OK:
		return "OK"
	case sdkv1.Status_STATUS_TIMEOUT:
		return "Timeout"
	case sdkv1.Status_STATUS_THROTTLED:
		return "Throttled"
	case sdkv1.Status_STATUS_INVALID_ARGUMENT:
		return "Invalid response"
	case sdkv1.Status_STATUS_TEMPORARY_ERROR:
		return "Temporary error"
	case sdkv1.Status_STATUS_PERMANENT_ERROR:
		return "Permanent error"
	case sdkv1.Status_STATUS_INCOMPATIBLE_STATE:
		return "Incompatible state"
	case sdkv1.Status_STATUS_DNS_ERROR:
		return "DNS error"
	case sdkv1.Status_STATUS_TCP_ERROR:
		return "TCP error"
	case sdkv1.Status_STATUS_TLS_ERROR:
		return "TLS error"
	case sdkv1.Status_STATUS_HTTP_ERROR:
		return "HTTP error"
	case sdkv1.Status_STATUS_UNAUTHENTICATED:
		return "Unauthenticated"
	case sdkv1.Status_STATUS_PERMISSION_DENIED:
		return "Permission denied"
	case sdkv1.Status_STATUS_NOT_FOUND:
		return "Not found"
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
