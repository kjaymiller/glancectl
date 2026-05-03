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

	"github.com/kjaymiller/glancectl/internal/glanceconf"
	"github.com/kjaymiller/glancectl/internal/sources"
)

type pane int

const (
	paneServices pane = iota
	paneActions
	paneBookmarks
	numPanes
)

type Options struct {
	Config       *glanceconf.Config
	Workdir      string        // where to run `just`
	RefreshEvery time.Duration // services + counts refresh
	HTTPTimeout  time.Duration // per-request HTTP timeout
}

type Model struct {
	opts Options

	width, height int
	focus         pane

	// services pane
	sites   []sources.Site
	health  []sources.HealthResult
	svcCur  int

	// actions pane
	recipes []sources.Recipe
	actCur  int

	// bookmarks pane
	bookmarks []bookmarkEntry
	bmCur     int

	// counts (footer)
	alertCount  int
	updateCount int

	// runner output
	running   bool
	runTitle  string
	runOutput strings.Builder

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
	m := Model{opts: opts}

	for _, s := range opts.Config.Sites() {
		m.sites = append(m.sites, sources.Site{Title: s.Title, URL: s.URL})
	}
	for _, g := range opts.Config.Bookmarks() {
		m.bookmarks = append(m.bookmarks, bookmarkEntry{IsHeader: true, Title: g.Title})
		for _, l := range g.Links {
			m.bookmarks = append(m.bookmarks, bookmarkEntry{Title: l.Title, URL: l.URL})
		}
	}
	// Skip past the first header so the cursor starts on a real entry.
	if len(m.bookmarks) > 0 && m.bookmarks[0].IsHeader {
		m.bmCur = 1
	}
	return m
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.refreshHealthCmd(),
		m.refreshCountsCmd(),
		m.refreshRecipesCmd(),
		m.tickCmd(),
	)
}

// ── messages ──────────────────────────────────────────────────────────

type tickMsg time.Time
type healthMsg []sources.HealthResult
type countsMsg struct {
	alerts, updates int
}
type recipesMsg []sources.Recipe

// ── commands ──────────────────────────────────────────────────────────

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(m.opts.RefreshEvery, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func (m Model) refreshHealthCmd() tea.Cmd {
	sites := append([]sources.Site(nil), m.sites...)
	timeout := m.opts.HTTPTimeout
	return func() tea.Msg {
		return healthMsg(sources.CheckAll(context.Background(), sites, timeout))
	}
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

type runResultMsg struct {
	output []byte
	err    error
}

// runRecipe runs `just <name>` and returns the captured output + status.
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
		return m, nil

	case tickMsg:
		return m, tea.Batch(m.refreshHealthCmd(), m.refreshCountsCmd(), m.tickCmd())

	case healthMsg:
		m.health = []sources.HealthResult(msg)
		return m, nil

	case countsMsg:
		m.alertCount = msg.alerts
		m.updateCount = msg.updates
		return m, nil

	case recipesMsg:
		m.recipes = []sources.Recipe(msg)
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
		m.focus = (m.focus + 1) % numPanes
		return m, nil
	case "shift+tab":
		m.focus = (m.focus + numPanes - 1) % numPanes
		return m, nil
	case "r":
		return m, tea.Batch(m.refreshHealthCmd(), m.refreshCountsCmd(), m.refreshRecipesCmd())
	case "esc":
		m.runOutput.Reset()
		m.runTitle = ""
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
	case paneServices:
		if n := len(m.sites); n > 0 {
			m.svcCur = clamp(m.svcCur+d, 0, n-1)
		}
	case paneActions:
		if n := len(m.recipes); n > 0 {
			m.actCur = clamp(m.actCur+d, 0, n-1)
		}
	case paneBookmarks:
		if n := len(m.bookmarks); n > 0 {
			next := clamp(m.bmCur+d, 0, n-1)
			// Skip over header rows.
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

func (m Model) activate() (tea.Model, tea.Cmd) {
	switch m.focus {
	case paneServices:
		if m.svcCur < len(m.sites) {
			openURL(m.sites[m.svcCur].URL)
		}
	case paneActions:
		if m.actCur < len(m.recipes) && !m.running {
			cmd := (&m).runRecipe(m.recipes[m.actCur].Name)
			return m, cmd
		}
	case paneBookmarks:
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

// ── view ──────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 || m.height == 0 {
		return "loading…"
	}

	// Header
	header := titleBar.Render(" glancectl ") + "  " +
		subtle.Render(fmt.Sprintf("config pages: %d", len(m.opts.Config.Pages)))

	// Body: 3 columns. Reserve rows for header(1)+spacer(1)+runner+footer(1).
	runnerRows := 0
	if m.running || m.runOutput.Len() > 0 {
		runnerRows = 8
	}
	bodyHeight := m.height - 3 - runnerRows
	if bodyHeight < 6 {
		bodyHeight = 6
	}

	colW := (m.width - 2) / 3
	if colW < 18 {
		colW = 18
	}

	left := m.renderServices(colW, bodyHeight)
	mid := m.renderActions(colW, bodyHeight)
	right := m.renderBookmarks(m.width-2*colW, bodyHeight)

	body := lipgloss.JoinHorizontal(lipgloss.Top, left, mid, right)

	parts := []string{header, body}
	if runnerRows > 0 {
		parts = append(parts, m.renderRunner(m.width, runnerRows))
	}
	parts = append(parts, m.renderFooter())
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

func (m Model) renderServices(w, h int) string {
	title := "Services"
	header := paneTitle.Render(title)
	if m.focus == paneServices {
		header = paneTitleFocused.Render(title)
	}
	var lines []string
	lines = append(lines, header, "")
	for i, s := range m.sites {
		mark := subtle.Render("·")
		if i < len(m.health) {
			h := m.health[i]
			switch {
			case h.Err != nil:
				mark = bad.Render("✗")
			case h.Status >= 200 && h.Status < 400:
				mark = good.Render("✓")
			case h.Status >= 400:
				mark = warn.Render(fmt.Sprintf("%d", h.Status))
			}
		}
		row := fmt.Sprintf("%s %s", mark, truncate(s.Title, w-6))
		if i == m.svcCur && m.focus == paneServices {
			row = selected.Render(row)
		}
		lines = append(lines, row)
	}
	return paneOf(m.focus == paneServices).Width(w).Height(h).Render(strings.Join(lines, "\n"))
}

func (m Model) renderActions(w, h int) string {
	title := "Actions"
	header := paneTitle.Render(title)
	if m.focus == paneActions {
		header = paneTitleFocused.Render(title)
	}
	var lines []string
	lines = append(lines, header, "")
	lastGroup := ""
	for i, r := range m.recipes {
		if r.Group != lastGroup {
			if i > 0 {
				lines = append(lines, "")
			}
			if r.Group != "" {
				lines = append(lines, groupSt.Render("["+r.Group+"]"))
			}
			lastGroup = r.Group
		}
		row := "  " + truncate(r.Name, w-6)
		if i == m.actCur && m.focus == paneActions {
			row = selected.Render(row)
		}
		lines = append(lines, row)
	}
	return paneOf(m.focus == paneActions).Width(w).Height(h).Render(strings.Join(lines, "\n"))
}

func (m Model) renderBookmarks(w, h int) string {
	title := "Bookmarks"
	header := paneTitle.Render(title)
	if m.focus == paneBookmarks {
		header = paneTitleFocused.Render(title)
	}
	var lines []string
	lines = append(lines, header, "")
	for i, b := range m.bookmarks {
		if b.IsHeader {
			lines = append(lines, groupSt.Render(b.Title))
			continue
		}
		row := "  " + truncate(b.Title, w-6)
		if i == m.bmCur && m.focus == paneBookmarks {
			row = selected.Render(row)
		}
		lines = append(lines, row)
	}
	return paneOf(m.focus == paneBookmarks).Width(w).Height(h).Render(strings.Join(lines, "\n"))
}

func (m Model) renderRunner(w, h int) string {
	header := accent.Bold(true).Render(m.runTitle)
	if m.running {
		header += " " + warn.Render("(running…)")
	} else if m.statusLine != "" {
		header += " " + subtle.Render("("+m.statusLine+")")
	}
	body := m.runOutput.String()
	body = lastLines(body, h-3)
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
		"r refresh",
		"esc close",
		"q quit",
	}
	return footer.Width(m.width).Render(strings.Join(bits, "  "))
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
