package ui

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kjaymiller/glancectl/internal/glanceconf"
	"github.com/kjaymiller/glancectl/internal/sources"
)

type pane int

const (
	paneLeft pane = iota
	paneMiddle
	paneRight
	numPanes
)

type Options struct {
	Config       *glanceconf.Config
	Workdir      string        // where to run `just`
	RefreshEvery time.Duration // services + counts + cards refresh
	HTTPTimeout  time.Duration // per-request HTTP timeout
}

type Model struct {
	opts Options

	width, height int
	focus         pane

	// left pane: active alerts + actions. Only actions take the cursor.
	recipes []sources.Recipe
	leftCur int

	// middle pane: cards built from MiddleWidgets, scrolled vertically.
	midWidgets []glanceconf.Widget
	midData    []any // per-widget fetched data, parallel to midWidgets
	midOffset  int
	weather    *sources.CachedWeather

	// alertIdx points into midWidgets/midData at the alerts widget, which
	// renders in the left pane instead of the feed. -1 when the config has
	// no alerts widget.
	alertIdx int

	// right pane: bookmarks.
	bookmarks []bookmarkEntry
	bmCur     int

	// footer counts
	alertCount  int
	updateCount int

	// runner output
	running    bool
	runTitle   string
	runOutput  strings.Builder
	statusLine string
}

type bookmarkEntry struct {
	IsHeader bool
	Title    string
	URL      string
}

func New(opts Options) Model {
	if opts.RefreshEvery == 0 {
		opts.RefreshEvery = 30 * time.Second
	}
	if opts.HTTPTimeout == 0 {
		opts.HTTPTimeout = 5 * time.Second
	}
	m := Model{opts: opts, alertIdx: -1}

	for _, g := range opts.Config.Bookmarks() {
		m.bookmarks = append(m.bookmarks, bookmarkEntry{IsHeader: true, Title: g.Title})
		for _, l := range g.Links {
			m.bookmarks = append(m.bookmarks, bookmarkEntry{Title: l.Title, URL: l.URL})
		}
	}
	if len(m.bookmarks) > 0 && m.bookmarks[0].IsHeader {
		m.bmCur = 1
	}

	m.midWidgets = opts.Config.MiddleWidgets()
	m.midData = make([]any, len(m.midWidgets))
	for i, w := range m.midWidgets {
		if w.Type == "weather" && m.weather == nil {
			m.weather = sources.NewCachedWeather(w.Location, w.Units)
		}
		// The alerts widget stays in midWidgets so it refreshes with the
		// rest; only its rendering moves to the left pane.
		if m.alertIdx < 0 && w.Type == "custom-api" && contains(strings.ToLower(w.Title), "alert") {
			m.alertIdx = i
		}
	}
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.refreshCountsCmd(),
		m.refreshRecipesCmd(),
		m.tickCmd(),
	}
	for i := range m.midWidgets {
		cmds = append(cmds, m.refreshCardCmd(i))
	}
	return tea.Batch(cmds...)
}

// ── messages ──────────────────────────────────────────────────────────

type tickMsg time.Time
type countsMsg struct{ alerts, updates int }
type recipesMsg []sources.Recipe
type cardMsg struct {
	idx  int
	data any
}
type runResultMsg struct {
	output []byte
	err    error
}

// ── commands ──────────────────────────────────────────────────────────

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(m.opts.RefreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) refreshCountsCmd() tea.Cmd {
	cfg := m.opts.Config
	timeout := m.opts.HTTPTimeout
	return func() tea.Msg {
		out := countsMsg{}
		if w := cfg.FindCustomAPI("alert"); w != nil {
			if v, err := sources.FetchJSON(context.Background(), w.URL, w.Headers, timeout); err == nil {
				out.alerts = sources.CountAlerts(v)
			}
		}
		if w := cfg.FindCustomAPI("update"); w != nil {
			if v, err := sources.FetchJSON(context.Background(), w.URL, w.Headers, timeout); err == nil {
				out.updates = sources.CountActionableUpdates(v)
			}
		}
		return out
	}
}

func (m Model) refreshRecipesCmd() tea.Cmd {
	wd := m.opts.Workdir
	return func() tea.Msg {
		r, err := sources.ListRecipes(wd)
		if err != nil {
			return recipesMsg(nil)
		}
		return recipesMsg(r)
	}
}

func (m Model) refreshCardCmd(idx int) tea.Cmd {
	if idx < 0 || idx >= len(m.midWidgets) {
		return nil
	}
	w := m.midWidgets[idx]
	timeout := m.opts.HTTPTimeout
	weather := m.weather
	return func() tea.Msg {
		ctx := context.Background()
		var data any
		var err error
		switch {
		case w.Type == "weather" && weather != nil:
			data, err = weather.Fetch(ctx, timeout)
		case w.Type == "custom-api" && contains(strings.ToLower(w.Title), "kuma"):
			data, err = sources.FetchKumaUptime(ctx, w.URL, w.Headers, timeout)
		case w.Type == "custom-api" && contains(strings.ToLower(w.Title), "prometheus"):
			data, err = sources.FetchPromRange(ctx, w.URL, w.Parameters, timeout)
		// First among the custom-api cases: "systems" collides with none of
		// the sibling substrings, but a title like "System Updates" would be
		// eaten by the "update" case below, so match it before them.
		case w.Type == "custom-api" && contains(strings.ToLower(w.Title), "system"):
			data, err = sources.FetchServiceStatus(ctx, w.URL, w.Headers, timeout)
		case w.Type == "custom-api" && contains(strings.ToLower(w.Title), "brave"):
			data, err = sources.FetchSchedule(ctx, w.URL, w.Parameters, timeout)
		case w.Type == "custom-api" && contains(strings.ToLower(w.Title), "update"):
			var v any
			v, err = sources.FetchJSON(ctx, w.URL, w.Headers, timeout)
			if err == nil {
				data = sources.ActionableUpdates(v)
			}
		case w.Type == "custom-api" && contains(strings.ToLower(w.Title), "channels"):
			data, err = sources.FetchYtdlChannels(ctx, w.URL, w.Headers, timeout)
		case w.Type == "custom-api" && contains(strings.ToLower(w.Title), "runs"):
			data, err = sources.FetchYtdlRuns(ctx, w.URL, w.Headers, timeout)
		case w.Type == "custom-api" && contains(strings.ToLower(w.Title), "alert"):
			var v any
			v, err = sources.FetchJSON(ctx, w.URL, w.Headers, timeout)
			if err == nil {
				data = sources.CountAlerts(v)
			}
		}
		if err != nil {
			data = err
		}
		return cardMsg{idx: idx, data: data}
	}
}

func (m *Model) runRecipe(name string) tea.Cmd {
	wd := m.opts.Workdir
	m.running = true
	m.runTitle = "just " + name
	m.runOutput.Reset()
	return func() tea.Msg {
		cmd := exec.Command("just", name)
		cmd.Dir = wd
		out, err := cmd.CombinedOutput()
		return runResultMsg{output: out, err: err}
	}
}

// ── update ────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		// A resize can hide the focused pane; move focus somewhere visible
		// so keys keep acting on something the user can see.
		if !m.paneVisible(m.focus) {
			m.focus = paneMiddle
		}
		return m, nil

	case tickMsg:
		cmds := []tea.Cmd{m.refreshCountsCmd(), m.tickCmd()}
		for i := range m.midWidgets {
			cmds = append(cmds, m.refreshCardCmd(i))
		}
		return m, tea.Batch(cmds...)

	case countsMsg:
		m.alertCount = msg.alerts
		m.updateCount = msg.updates
		return m, nil

	case recipesMsg:
		m.recipes = []sources.Recipe(msg)
		return m, nil

	case cardMsg:
		if msg.idx >= 0 && msg.idx < len(m.midData) {
			m.midData[msg.idx] = msg.data
		}
		return m, nil

	case runResultMsg:
		m.runOutput.Write(msg.output)
		m.running = false
		if msg.err != nil {
			m.statusLine = "✗ " + msg.err.Error()
		} else {
			m.statusLine = "✓ " + m.runTitle + " finished"
		}
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "tab":
		m.focus = m.nextPane(+1)
		return m, nil
	case "shift+tab":
		m.focus = m.nextPane(-1)
		return m, nil
	case "r":
		cmds := []tea.Cmd{m.refreshCountsCmd(), m.refreshRecipesCmd()}
		for i := range m.midWidgets {
			cmds = append(cmds, m.refreshCardCmd(i))
		}
		return m, tea.Batch(cmds...)
	case "y":
		m.statusLine = m.yank()
		return m, nil
	case "esc":
		m.runOutput.Reset()
		m.runTitle = ""
		m.statusLine = ""
		return m, nil
	case "up", "k":
		m.moveCursor(-1)
		return m, nil
	case "down", "j":
		m.moveCursor(+1)
		return m, nil
	case "enter":
		return m.activate()
	}
	return m, nil
}

func (m *Model) moveCursor(d int) {
	switch m.focus {
	case paneLeft:
		if n := len(m.recipes); n > 0 {
			m.leftCur = clamp(m.leftCur+d, 0, n-1)
		}
	case paneMiddle:
		m.midOffset = clamp(m.midOffset+d, 0, m.midScrollMax())
	case paneRight:
		if n := len(m.bookmarks); n > 0 {
			next := clamp(m.bmCur+d, 0, n-1)
			for next >= 0 && next < n && m.bookmarks[next].IsHeader {
				next += d
				if next < 0 || next >= n {
					return
				}
			}
			m.bmCur = next
		}
	}
}

// midScrollMax is the largest useful feed offset: the point where the last
// line sits at the bottom of the viewport. Scrolling past that would leave
// the pane blank with no way to tell how far you had gone.
func (m Model) midScrollMax() int {
	visible := m.paneHeight(paneMiddle) - paneVChrome - 2
	if visible < 1 {
		visible = 1
	}
	if max := len(m.middleLines(m.paneWidth(paneMiddle))) - visible; max > 0 {
		return max
	}
	return 0
}

func (m Model) activate() (tea.Model, tea.Cmd) {
	switch m.focus {
	case paneLeft:
		if m.leftCur >= 0 && m.leftCur < len(m.recipes) && !m.running {
			cmd := (&m).runRecipe(m.recipes[m.leftCur].Name)
			return m, cmd
		}
	case paneRight:
		if m.bmCur < len(m.bookmarks) {
			b := m.bookmarks[m.bmCur]
			if !b.IsHeader && b.URL != "" {
				openURL(b.URL)
			}
		}
	}
	return m, nil
}

func openURL(url string) {
	bin := "xdg-open"
	if runtime.GOOS == "darwin" {
		bin = "open"
	}
	_ = exec.Command(bin, url).Start()
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ── layout ────────────────────────────────────────────────────────────

// paneChrome is the horizontal cost of paneBox's rounded border. lipgloss
// Width() sizes the content box inside the border, so a pane occupies two
// more columns than we ask for; every width below is an outer width, and
// the render funcs subtract this before handing it to lipgloss.
const paneChrome = 2

// paneVChrome is the vertical equivalent: the box's top and bottom border
// rows, which Height() likewise does not count.
const paneVChrome = 2

const (
	minSideW = 22 // narrower than this and service names are unreadable
	minMidW  = 32

	// minStackH is the shortest a stacked pane can usefully be: two border
	// rows, a title, a spacer, and two content rows. One content row is
	// not enough — overflowing content spends it on the "N more" marker,
	// leaving a pane that shows nothing but its own truncation.
	minStackH = 6
)

type paneLayout struct {
	pane   pane
	width  int // outer, including paneChrome
	height int // outer, including paneVChrome
}

// layout places the visible panes in the terminal. Above minMidW+minSideW
// the panes sit side by side; below it there is no width to split, so they
// stack vertically at full width instead — a phone-sized terminal gets the
// whole dashboard scrolled down the screen rather than the feed alone.
func (m Model) layout() []paneLayout {
	side := m.width / 5
	if side < minSideW {
		side = minSideW
	}
	h := m.bodyHeight()
	switch {
	case m.width >= 2*side+minMidW:
		return []paneLayout{
			{paneLeft, side, h},
			{paneMiddle, m.width - 2*side, h},
			{paneRight, side, h},
		}
	case m.width >= side+minMidW:
		return []paneLayout{{paneLeft, side, h}, {paneMiddle, m.width - side, h}}
	default:
		return m.stackedLayout(h)
	}
}

// stackedLayout splits the body height down the column. Every pane gets
// minStackH and the focused one absorbs the remainder, so tab doubles as
// "expand this pane" on a display too narrow to show them side by side.
// When even the minimums do not fit, panes drop in the same order the
// horizontal layout drops them: right first, then left.
func (m Model) stackedLayout(h int) []paneLayout {
	order := []pane{paneLeft, paneMiddle, paneRight}
	for len(order) > 1 && h < len(order)*minStackH {
		if order[len(order)-1] == paneRight {
			order = order[:len(order)-1]
		} else {
			order = order[1:]
		}
	}

	focus := m.focus
	if !containsPane(order, focus) {
		focus = paneMiddle
	}

	out := make([]paneLayout, 0, len(order))
	spare := h - len(order)*minStackH
	for _, p := range order {
		ph := minStackH
		if spare > 0 && p == focus {
			ph += spare
		}
		out = append(out, paneLayout{p, m.width, ph})
	}
	// A body shorter than the minimums still has to total exactly h, or
	// the frame overshoots the terminal and scrolls the header away.
	if spare < 0 {
		out[len(out)-1].height += spare
	}
	return out
}

func containsPane(ps []pane, p pane) bool {
	for _, x := range ps {
		if x == p {
			return true
		}
	}
	return false
}

// bodyHeight is the room left for panes once the header, footer and (when
// shown) the runner have taken their rows.
func (m Model) bodyHeight() int {
	runnerRows := 0
	if m.running || m.runOutput.Len() > 0 {
		runnerRows = runnerHeight
	}
	h := m.height - 2 - runnerRows
	if h < paneVChrome+1 {
		h = paneVChrome + 1
	}
	return h
}

const runnerHeight = 8

// nextPane advances focus by d, skipping panes the current width hides.
func (m Model) nextPane(d int) pane {
	p := m.focus
	for i := 0; i < int(numPanes); i++ {
		p = (p + pane(d) + numPanes) % numPanes
		if m.paneVisible(p) {
			return p
		}
	}
	return m.focus
}

// window clips lines to h rows starting at offset, replacing the first and
// last visible rows with hidden-count markers when content is cut off. The
// returned slice is always exactly h rows so panes keep a stable height.
func window(lines []string, offset, h int) []string {
	if h <= 0 {
		return nil
	}
	if max := len(lines) - h; offset > max {
		offset = max
	}
	if offset < 0 {
		offset = 0
	}
	end := offset + h
	if end > len(lines) {
		end = len(lines)
	}
	out := append([]string(nil), lines[offset:end]...)
	if offset > 0 && len(out) > 0 {
		out[0] = subtle.Render(fmt.Sprintf("↑ %d more", offset))
	}
	if end < len(lines) && len(out) > 0 {
		out[len(out)-1] = subtle.Render(fmt.Sprintf("↓ %d more", len(lines)-end+1))
	}
	for len(out) < h {
		out = append(out, "")
	}
	return out
}

// scrollTo returns the offset that brings line cur into a viewport of h
// rows, preferring the smallest movement from the current offset. Used by
// the cursor panes, which scroll to follow the selection rather than being
// scrolled directly.
func scrollTo(cur, offset, h, total int) int {
	if h <= 0 || total <= h {
		return 0
	}
	if cur < offset+1 {
		offset = cur - 1 // keep a row of context above where possible
	}
	if cur > offset+h-2 {
		offset = cur - h + 2
	}
	return clamp(offset, 0, total-h)
}

func (m Model) paneVisible(p pane) bool {
	return m.paneHeight(p) > 0
}

// paneHeight reports the outer height the layout gave a pane, or 0 when it
// is hidden at the current size.
func (m Model) paneHeight(p pane) int {
	for _, l := range m.layout() {
		if l.pane == p {
			return l.height
		}
	}
	return 0
}

// paneWidth is paneHeight's horizontal twin, minus the border, so callers
// get the width content actually has to work with. Zero when the pane is
// not in the current layout (dropped on a narrow terminal).
func (m Model) paneWidth(p pane) int {
	for _, l := range m.layout() {
		if l.pane == p {
			return l.width - paneChrome
		}
	}
	return 0
}

// stacked reports whether the terminal is too narrow to put panes side by
// side, so they run down the screen instead.
func (m Model) stacked() bool {
	side := m.width / 5
	if side < minSideW {
		side = minSideW
	}
	return m.width < side+minMidW
}

// ── view ──────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading…"
	}

	header := titleBar.Render(" glancectl ") + "  " +
		subtle.Render(fmt.Sprintf("config pages: %d", len(m.opts.Config.Pages)))

	runnerRows := 0
	if m.running || m.runOutput.Len() > 0 {
		runnerRows = runnerHeight
	}

	// The frame must total exactly m.height: one header row, one footer
	// row, the runner if shown, and the panes take the rest. A single row
	// of overshoot scrolls the terminal and pushes the header off-screen.
	panes := m.layout()
	var cols []string
	for _, p := range panes {
		switch p.pane {
		case paneLeft:
			cols = append(cols, m.renderLeft(p.width, p.height))
		case paneMiddle:
			cols = append(cols, m.renderMiddle(p.width, p.height))
		case paneRight:
			cols = append(cols, m.renderRight(p.width, p.height))
		}
	}
	body := lipgloss.JoinHorizontal(lipgloss.Top, cols...)
	if m.stacked() {
		body = lipgloss.JoinVertical(lipgloss.Left, cols...)
	}

	parts := []string{header, body}
	if runnerRows > 0 {
		parts = append(parts, m.renderRunner(m.width, runnerRows))
	}
	parts = append(parts, m.renderFooter())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// leftLines builds the pane's scrollable content and reports which line
// the cursor sits on, so the viewport can follow the selection.
func (m Model) leftLines(w int) ([]string, int) {
	var lines []string
	cursor := 0

	if m.alertIdx >= 0 {
		c := BuildCard(m.midWidgets[m.alertIdx], m.midData[m.alertIdx], w)
		lines = append(lines, groupSt.Render(c.Title))
		lines = append(lines, c.Lines...)
		lines = append(lines, "")
	}

	lines = append(lines, groupSt.Render("Actions"))
	lastGroup := ""
	for i, r := range m.recipes {
		if r.Group != lastGroup {
			if i > 0 {
				lines = append(lines, "")
			}
			if r.Group != "" {
				lines = append(lines, subtle.Render("["+r.Group+"]"))
			}
			lastGroup = r.Group
		}
		row := "  " + truncate(r.Name, w-6)
		if m.leftCur == i {
			cursor = len(lines)
			if m.focus == paneLeft {
				row = selected.Render(row)
			}
		}
		lines = append(lines, row)
	}
	return lines, cursor
}

func (m Model) renderLeft(w, h int) string {
	lines, cursor := m.leftLines(w)
	inner := h - paneVChrome
	body := window(lines, scrollTo(cursor, 0, inner-2, len(lines)), inner-2)
	return m.paneBoxed(paneLeft, "Alerts / Actions", body, w, inner)
}

// middleLines builds the feed's cards as one flat, scrollable list. w is
// the pane's content width, which column-laying cards size against.
func (m Model) middleLines(w int) []string {
	var lines []string
	for i, wd := range m.midWidgets {
		if i == m.alertIdx { // rendered in the left pane instead
			continue
		}
		if len(lines) > 0 {
			lines = append(lines, "")
		}
		c := BuildCard(wd, m.midData[i], w)
		lines = append(lines, accent.Bold(true).Render(c.Title))
		lines = append(lines, c.Lines...)
	}
	return lines
}

func (m Model) renderMiddle(w, h int) string {
	inner := h - paneVChrome
	body := window(m.middleLines(w-paneChrome), m.midOffset, inner-2)
	return m.paneBoxed(paneMiddle, "Feed", body, w, inner)
}

func (m Model) rightLines(w int) ([]string, int) {
	var lines []string
	cursor := 0
	for i, b := range m.bookmarks {
		if b.IsHeader {
			lines = append(lines, groupSt.Render(b.Title))
			continue
		}
		row := "  " + truncate(b.Title, w-6)
		if m.bmCur == i {
			cursor = len(lines)
			if m.focus == paneRight {
				row = selected.Render(row)
			}
		}
		lines = append(lines, row)
	}
	return lines, cursor
}

func (m Model) renderRight(w, h int) string {
	lines, cursor := m.rightLines(w)
	inner := h - paneVChrome
	body := window(lines, scrollTo(cursor, 0, inner-2, len(lines)), inner-2)
	return m.paneBoxed(paneRight, "Bookmarks", body, w, inner)
}

// paneBoxed frames a pane: focus-aware title, blank spacer, then the
// already-windowed body. Height is the inner content height, so the box
// renders exactly h+paneVChrome rows.
func (m Model) paneBoxed(p pane, title string, body []string, w, inner int) string {
	header := paneTitle.Render(title)
	if m.focus == p {
		header = paneTitleFocused.Render(title)
	}
	lines := append([]string{header, ""}, body...)
	return paneOf(m.focus == p).Width(w - paneChrome).Height(inner).Render(strings.Join(lines, "\n"))
}

func (m Model) renderRunner(w, h int) string {
	header := accent.Bold(true).Render(m.runTitle)
	if m.running {
		header += " " + warn.Render("(running…)")
	} else if m.statusLine != "" {
		header += " " + subtle.Render("("+m.statusLine+")")
	}
	body := lastLines(m.runOutput.String(), h-3)
	content := strings.Join([]string{header, "", body}, "\n")
	return paneBox.Width(w - 2).Height(h - 1).Render(content)
}

func (m Model) renderFooter() string {
	bits := []string{
		fmt.Sprintf("alerts: %s", colorByCount(m.alertCount).Render(fmt.Sprintf("%d", m.alertCount))),
		fmt.Sprintf("updates: %s", colorByCount(m.updateCount).Render(fmt.Sprintf("%d", m.updateCount))),
		"",
		"tab pane",
		"↑/↓ nav",
		"enter act",
		"y copy",
		"r refresh",
		"esc close",
		"q quit",
	}
	help := strings.Join(bits, "  ")
	// The status line reports the result of the last action (a yank, a
	// finished recipe); it earns the footer when there is one to show.
	if m.statusLine != "" {
		help = accent.Render(m.statusLine) + "  " + subtle.Render("· esc to clear")
	}
	return footer.Width(m.width).Render(ansi.Truncate(help, m.width-2, "…"))
}

func paneOf(focused bool) lipgloss.Style {
	if focused {
		return paneBoxFocused
	}
	return paneBox
}

func colorByCount(n int) lipgloss.Style {
	if n == 0 {
		return good
	}
	return warn
}

func truncate(s string, w int) string {
	if w <= 1 {
		return ""
	}
	if len(s) <= w {
		return s
	}
	if w <= 3 {
		return s[:w]
	}
	return s[:w-1] + "…"
}

func lastLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n")
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}
