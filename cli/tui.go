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

const (
	pendingIcon = "•" // U+2022
	successIcon = "✔" // U+2714
	failureIcon = "✗" // U+2718
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
	pendingStyle   = lipgloss.NewStyle().Foreground(grayColor)
	suspendedStyle = lipgloss.NewStyle().Foreground(grayColor)
	retryStyle     = lipgloss.NewStyle().Foreground(yellowColor)
	errorStyle     = lipgloss.NewStyle().Foreground(redColor)
	okStyle        = lipgloss.NewStyle().Foreground(greenColor)

	// Styles for other components inside the table.
	treeStyle = lipgloss.NewStyle().Foreground(grayColor)

	// Styles for the function call detail tab.
	detailHeaderStyle      = lipgloss.NewStyle().Foreground(grayColor)
	detailLowPriorityStyle = lipgloss.NewStyle().Foreground(grayColor)
)

type TUI struct {
	ticks uint64

	// Storage for the function call hierarchies.
	//
	// FIXME: we never clean up items from these maps
	roots        map[DispatchID]struct{}
	orderedRoots []DispatchID
	calls        map[DispatchID]functionCall

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
	logoHelp         string
	logsTabHelp      string
	functionsTabHelp string
	detailTabHelp    string
	selectHelp       string
	windowHeight     int
	selected         *DispatchID

	err error

	mu sync.Mutex
}

type tab int

const (
	functionsTab tab = iota
	logsTab
	detailTab
)

const tabCount = 3

var (
	tabKey = key.NewBinding(
		key.WithKeys("tab"),
		key.WithHelp("tab", "switch tab"),
	)

	selectModeKey = key.NewBinding(
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

	selectKey = key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select function"),
	)

	exitSelectKey = key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "exit select"),
	)

	quitInSelectKey = key.NewBinding(
		key.WithKeys("ctrl+c"),
		key.WithHelp("ctrl+c", "quit"),
	)

	logoKeyMap         = []key.Binding{tabKey, quitKey}
	functionsTabKeyMap = []key.Binding{tabKey, selectModeKey, quitKey}
	detailTabKeyMap    = []key.Binding{tabKey, quitKey}
	logsTabKeyMap      = []key.Binding{tabKey, tailKey, quitKey}
	selectKeyMap       = []key.Binding{selectKey, exitSelectKey, tabKey, quitInSelectKey}
)

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
	t.logoHelp = t.help.ShortHelpView(logoKeyMap)
	t.logsTabHelp = t.help.ShortHelpView(logsTabKeyMap)
	t.functionsTabHelp = t.help.ShortHelpView(functionsTabKeyMap)
	t.detailTabHelp = t.help.ShortHelpView(detailTabKeyMap)
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
		cmds = append(cmds, textinput.Blink)

	case tea.WindowSizeMsg:
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
				t.activeTab = functionsTab
				t.viewport.YOffset = 0 // reset
				t.tailMode = true
			case "enter":
				if t.selected != nil {
					t.selectMode = false
					t.activeTab = detailTab
					t.viewport.YOffset = 0 // reset
				}
			case "ctrl+c":
				return t, tea.Quit
			}
		} else {
			switch msg.String() {
			case "esc":
				if t.activeTab == detailTab {
					t.activeTab = functionsTab
					t.viewport.YOffset = 0 // reset
					t.tailMode = true
				} else {
					return t, tea.Quit
				}
			case "ctrl+c", "q":
				return t, tea.Quit
			case "s":
				// Don't accept s/select until at least one function
				// call has been received.
				if len(t.calls) > 0 && t.err == nil {
					cmds = append(cmds, focusSelect)
				}
			case "t":
				t.tailMode = true
			case "tab":
				t.selectMode = false
				t.activeTab = (t.activeTab + 1) % tabCount
				if t.activeTab == detailTab && t.selected == nil {
					t.activeTab = functionsTab
				}
				t.viewport.YOffset = 0 // reset
				t.tailMode = true
			case "up", "down", "left", "right", "pgup", "pgdown", "ctrl+u", "ctrl+d":
				t.tailMode = false
			}
		}
	}

	// Forward messages to the text input in select mode.
	if t.selectMode {
		t.selection, cmd = t.selection.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	// Forward messages to the viewport, e.g. for scroll-back support.
	t.viewport, cmd = t.viewport.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	cmd = nil
	switch {
	case len(cmds) == 1:
		cmd = cmds[0]
	case len(cmds) > 1:
		cmd = tea.Batch(cmds...)
	}

	return t, cmd
}

func (t *TUI) View() string {
	t.mu.Lock()
	defer t.mu.Unlock()

	var viewportContent string
	var statusBarContent string
	var helpContent string
	if !t.ready {
		viewportContent = t.logoView()
		statusBarContent = "Initializing..."
		helpContent = t.logoHelp
	} else {
		switch t.activeTab {
		case functionsTab:
			if len(t.roots) == 0 {
				viewportContent = t.logoView()
				statusBarContent = "Waiting for function calls..."
				helpContent = t.logoHelp
			} else {
				viewportContent = t.functionsView(time.Now())
				if len(t.calls) == 1 {
					statusBarContent = "1 total function call"
				} else {
					statusBarContent = fmt.Sprintf("%d total function calls", len(t.calls))
				}
				var inflightCount int
				for _, n := range t.calls {
					if !n.done {
						inflightCount++
					}
				}
				statusBarContent += fmt.Sprintf(", %d in-flight", inflightCount)
				helpContent = t.functionsTabHelp
			}
			if t.selectMode {
				statusBarContent = t.selection.View()
				helpContent = t.selectHelp
			}
		case detailTab:
			id := *t.selected
			viewportContent = t.detailView(id)
			helpContent = t.detailTabHelp
		case logsTab:
			viewportContent = t.logs.String()
			helpContent = t.logsTabHelp
		}
	}

	if t.err != nil {
		statusBarContent = errorStyle.Render(t.err.Error())
	}

	t.viewport.SetContent(viewportContent)

	// Shrink the viewport so it contains the content and status bar only.
	footerHeight := 1
	if statusBarContent != "" {
		footerHeight = 3
	}
	maxViewportHeight := max(t.windowHeight-footerHeight, 8)
	t.viewport.Height = min(t.viewport.TotalLineCount()+1, maxViewportHeight)

	// Tail the output, unless the user has tried
	// to scroll back (e.g. with arrow keys).
	if t.tailMode && !t.viewport.AtBottom() {
		t.viewport.GotoBottom()
	}

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

func (t *TUI) functionsView(now time.Time) string {
	t.selected = nil

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
	b.WriteByte('\n')
	return b.String()
}

func (t *TUI) tableHeaderView(functionColumnWidth int) string {
	columns := []string{
		left(functionColumnWidth, tableHeaderStyle.Render("Function")),
		right(8, tableHeaderStyle.Render("Attempt")),
		right(10, tableHeaderStyle.Render("Duration")),
		left(1, pendingIcon),
		left(35, tableHeaderStyle.Render("Status")),
	}
	if t.selectMode {
		idWidth := int(math.Log10(float64(len(t.calls)))) + 1
		columns = append([]string{left(idWidth, strings.Repeat("#", idWidth))}, columns...)
	}
	return join(columns...) + "\n"
}

func (t *TUI) tableRowView(r *row, functionColumnWidth int) string {
	attemptStr := strconv.Itoa(r.attempt)

	var durationStr string
	if r.duration > 0 {
		durationStr = r.duration.String()
	} else {
		durationStr = "?"
	}

	values := []string{
		left(functionColumnWidth, r.function),
		right(8, attemptStr),
		right(10, durationStr),
		left(1, r.icon),
		left(35, r.status),
	}

	id := strconv.Itoa(r.index)
	var selected bool
	if t.selectMode {
		idWidth := int(math.Log10(float64(len(t.calls)))) + 1
		paddedID := left(idWidth, id)
		if input := strings.TrimSpace(t.selection.Value()); input != "" && id == input {
			selected = true
			t.selected = &r.id
		}
		values = append([]string{paddedID}, values...)
	}
	result := join(values...)
	if selected {
		result = selectedStyle.Render(clearANSI(result))
	}
	return result + "\n"
}

func (t *TUI) detailView(id DispatchID) string {
	now := time.Now()

	n := t.calls[id]

	style, _, status := n.status(now)

	var view strings.Builder

	add := func(name, value string) {
		const padding = 16
		view.WriteString(right(padding, detailHeaderStyle.Render(name+":")))
		view.WriteByte(' ')
		view.WriteString(value)
		view.WriteByte('\n')
	}

	const timestampFormat = "2006-01-02T15:04:05.000"

	add("ID", detailLowPriorityStyle.Render(string(id)))
	add("Function", n.function())
	add("Status", style.Render(status))
	add("Creation time", detailLowPriorityStyle.Render(n.creationTime.Local().Format(timestampFormat)))
	if !n.expirationTime.IsZero() && !n.done {
		add("Expiration time", detailLowPriorityStyle.Render(n.expirationTime.Local().Format(timestampFormat)))
	}
	add("Duration", n.duration(now).String())
	add("Attempts", strconv.Itoa(n.attempt()))
	add("Requests", strconv.Itoa(len(n.timeline)))

	var result strings.Builder
	result.WriteString(view.String())

	for _, rt := range n.timeline {
		view.Reset()

		result.WriteByte('\n')

		// TODO: show request # and/or attempt #?

		add("Timestamp", detailLowPriorityStyle.Render(rt.request.ts.Local().Format(timestampFormat)))
		req := rt.request.proto
		switch d := req.Directive.(type) {
		case *sdkv1.RunRequest_Input:
			if rt.request.input == "" {
				rt.request.input = anyString(d.Input)
			}
			add("Input", rt.request.input)

		case *sdkv1.RunRequest_PollResult:
			add("Input", detailLowPriorityStyle.Render(fmt.Sprintf("<%d bytes of state>", len(d.PollResult.CoroutineState))))
			// TODO: show call results
			// TODO: show poll error
		}

		if rt.response.ts.IsZero() {
			add("Status", "Running")
		} else {
			if res := rt.response.proto; res != nil {
				switch d := res.Directive.(type) {
				case *sdkv1.RunResponse_Exit:
					var statusStyle lipgloss.Style
					if res.Status == sdkv1.Status_STATUS_OK {
						statusStyle = okStyle
					} else if terminalStatus(res.Status) {
						statusStyle = errorStyle
					} else {
						statusStyle = retryStyle
					}
					add("Status", statusStyle.Render(statusString(res.Status)))

					if result := d.Exit.Result; result != nil {
						if rt.response.output == "" {
							rt.response.output = anyString(result.Output)
						}
						add("Output", rt.response.output)

						if result.Error != nil {
							errorMessage := result.Error.Type
							if result.Error.Message != "" {
								errorMessage += ": " + result.Error.Message
							}
							add("Error", statusStyle.Render(errorMessage))
						}
					}
					if tailCall := d.Exit.TailCall; tailCall != nil {
						add("Tail call", tailCall.Function)
					}

				case *sdkv1.RunResponse_Poll:
					add("Status", suspendedStyle.Render("Suspended"))
					add("Output", detailLowPriorityStyle.Render(fmt.Sprintf("<%d bytes of state>", len(d.Poll.CoroutineState))))

					if len(d.Poll.Calls) > 0 {
						var calls strings.Builder
						for i, call := range d.Poll.Calls {
							if i > 0 {
								calls.WriteString(", ")
							}
							calls.WriteString(call.Function)
						}
						add("Calls", truncate(50, calls.String()))
					}
				}
			} else if c := rt.response.httpStatus; c != 0 {
				add("Error", errorStyle.Render(fmt.Sprintf("%d %s", c, http.StatusText(c))))
			} else if rt.response.err != nil {
				add("Error", errorStyle.Render(rt.response.err.Error()))
			}

			latency := rt.response.ts.Sub(rt.request.ts)
			add("Latency", latency.String())
		}
		result.WriteString(view.String())
	}

	return result.String()
}

type row struct {
	id       DispatchID
	index    int
	function string
	attempt  int
	duration time.Duration
	icon     string
	status   string
}

type rowBuffer struct {
	rows []row
	seq  int
}

func (b *rowBuffer) add(r row) {
	b.seq++
	r.index = b.seq
	b.rows = append(b.rows, r)
}

func (b *rowBuffer) reset() {
	b.rows = b.rows[:0]
}

func (t *TUI) buildRows(now time.Time, id DispatchID, isLast []bool, rows *rowBuffer) {
	n := t.calls[id]

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

	style, icon, status := n.status(now)

	function.WriteString(style.Render(n.function()))

	rows.add(row{
		id:       id,
		function: function.String(),
		attempt:  n.attempt(),
		duration: n.duration(now),
		icon:     style.Render(icon),
		status:   style.Render(status),
	})

	// Recursively render children.
	for i, id := range n.orderedChildren {
		last := i == len(n.orderedChildren)-1
		t.buildRows(now, id, append(isLast[:len(isLast):len(isLast)], last), rows)
	}
}

type DispatchID string

type functionCall struct {
	lastFunction string
	lastStatus   sdkv1.Status
	lastError    error

	failures int
	polls    int

	running   bool
	suspended bool
	done      bool

	creationTime   time.Time
	expirationTime time.Time
	doneTime       time.Time

	children        map[DispatchID]struct{}
	orderedChildren []DispatchID

	timeline []*roundtrip
}

type roundtrip struct {
	request  runRequest
	response runResponse
}

type runRequest struct {
	ts    time.Time
	proto *sdkv1.RunRequest
	input string
}

type runResponse struct {
	ts         time.Time
	proto      *sdkv1.RunResponse
	httpStatus int
	err        error
	output     string
}

func (n *functionCall) function() string {
	if n.lastFunction != "" {
		return n.lastFunction
	}
	return "(?)"
}

func (n *functionCall) status(now time.Time) (style lipgloss.Style, icon, status string) {
	icon = pendingIcon
	if n.running {
		style = pendingStyle
	} else if n.suspended {
		style = suspendedStyle
	} else if n.done {
		if n.lastStatus == sdkv1.Status_STATUS_OK {
			style = okStyle
			icon = successIcon
		} else {
			style = errorStyle
			icon = failureIcon
		}
	} else if !n.expirationTime.IsZero() && n.expirationTime.Before(now) {
		n.lastError = errors.New("Expired")
		style = errorStyle
		n.done = true
		n.doneTime = n.expirationTime
		icon = failureIcon
	} else if n.failures > 0 {
		style = retryStyle
	} else {
		style = pendingStyle
	}

	if n.running {
		status = "Running"
	} else if n.suspended {
		status = "Suspended"
	} else if n.lastError != nil {
		status = n.lastError.Error()
	} else if n.lastStatus != sdkv1.Status_STATUS_UNSPECIFIED {
		status = statusString(n.lastStatus)
	} else {
		status = "Pending"
	}

	return
}

func (n *functionCall) attempt() int {
	attempt := len(n.timeline) - n.polls
	if n.suspended {
		attempt++
	}
	return attempt
}

func (n *functionCall) duration(now time.Time) time.Duration {
	var duration time.Duration
	if !n.creationTime.IsZero() {
		var start time.Time
		if !n.creationTime.IsZero() && n.creationTime.Before(n.timeline[0].request.ts) {
			start = n.creationTime
		} else {
			start = n.timeline[0].request.ts
		}
		var end time.Time
		if !n.done {
			end = now
		} else {
			end = n.doneTime
		}
		duration = end.Sub(start).Truncate(time.Millisecond)
	}
	return max(duration, 0)
}

func (t *TUI) ObserveRequest(now time.Time, req *sdkv1.RunRequest) {
	// ObserveRequest is part of the FunctionCallObserver interface.
	// It's called after a request has been received from the Dispatch API,
	// and before the request has been sent to the local application.

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.roots == nil {
		t.roots = map[DispatchID]struct{}{}
	}
	if t.calls == nil {
		t.calls = map[DispatchID]functionCall{}
	}

	rootID := DispatchID(req.RootDispatchId)
	parentID := DispatchID(req.ParentDispatchId)
	id := DispatchID(req.DispatchId)

	// Upsert the root.
	if _, ok := t.roots[rootID]; !ok {
		t.roots[rootID] = struct{}{}
		t.orderedRoots = append(t.orderedRoots, rootID)
	}
	root, ok := t.calls[rootID]
	if !ok {
		root = functionCall{}
	}
	t.calls[rootID] = root

	// Upsert the function call.
	n, ok := t.calls[id]
	if !ok {
		n = functionCall{}
	}
	n.lastFunction = req.Function
	n.running = true
	n.suspended = false
	if req.CreationTime != nil {
		n.creationTime = req.CreationTime.AsTime()
	}
	if n.creationTime.IsZero() {
		n.creationTime = now
	}
	if req.ExpirationTime != nil {
		n.expirationTime = req.ExpirationTime.AsTime()
	}
	n.timeline = append(n.timeline, &roundtrip{request: runRequest{ts: now, proto: req}})
	t.calls[id] = n

	// Upsert the parent and link its child, if applicable.
	if parentID != "" {
		parent, ok := t.calls[parentID]
		if !ok {
			parent = functionCall{}
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
		t.calls[parentID] = parent
	}
}

func (t *TUI) ObserveResponse(now time.Time, req *sdkv1.RunRequest, err error, httpRes *http.Response, res *sdkv1.RunResponse) {
	// ObserveResponse is part of the FunctionCallObserver interface.
	// It's called after a response has been received from the local
	// application, and before the response has been sent to Dispatch.

	t.mu.Lock()
	defer t.mu.Unlock()

	id := DispatchID(req.DispatchId)
	n := t.calls[id]

	rt := n.timeline[len(n.timeline)-1]
	rt.response.ts = now
	rt.response.proto = res
	rt.response.err = err
	if res == nil && httpRes != nil {
		rt.response.httpStatus = httpRes.StatusCode
	}

	n.lastError = nil
	n.lastStatus = 0
	n.running = false

	if res != nil {
		switch res.Status {
		case sdkv1.Status_STATUS_OK:
			// noop
		case sdkv1.Status_STATUS_INCOMPATIBLE_STATE:
			n = functionCall{lastFunction: n.lastFunction} // reset
		default:
			n.failures++
		}

		switch d := res.Directive.(type) {
		case *sdkv1.RunResponse_Exit:
			n.lastStatus = res.Status
			n.done = terminalStatus(res.Status)
			if d.Exit.TailCall != nil {
				n = functionCall{lastFunction: d.Exit.TailCall.Function} // reset
			} else if res.Status != sdkv1.Status_STATUS_OK && d.Exit.Result != nil {
				if e := d.Exit.Result.Error; e != nil && e.Type != "" {
					if e.Message == "" {
						n.lastError = fmt.Errorf("%s", e.Type)
					} else {
						n.lastError = fmt.Errorf("%s: %s", e.Type, e.Message)
					}
				}
			}
		case *sdkv1.RunResponse_Poll:
			n.suspended = true
			n.polls++
		}
	} else if httpRes != nil {
		n.failures++
		n.lastError = fmt.Errorf("unexpected HTTP status code %d", httpRes.StatusCode)
		n.done = terminalHTTPStatusCode(httpRes.StatusCode)
	} else if err != nil {
		n.failures++
		n.lastError = err
	}

	if n.done && n.doneTime.IsZero() {
		n.doneTime = now
	}

	t.calls[id] = n
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

func (t *TUI) SetError(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.err = err
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
