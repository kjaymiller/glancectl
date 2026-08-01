package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/kjaymiller/glancectl/internal/sources"

	"github.com/kjaymiller/glancectl/internal/glanceconf"
)

func testConfig() *glanceconf.Config {
	return &glanceconf.Config{Pages: []glanceconf.Page{{
		Name: "P",
		Columns: []glanceconf.Column{
			{Size: "small", Widgets: []glanceconf.Widget{
				{Type: "monitor", Sites: []glanceconf.Site{{Title: "Grafana", URL: "http://x"}}},
			}},
			{Size: "full", Widgets: []glanceconf.Widget{
				{Type: "custom-api", Title: "Systems", URL: "http://x"},
			}},
			{Size: "small", Widgets: []glanceconf.Widget{
				{Type: "bookmarks", Groups: []glanceconf.BookmarkGroup{
					{Title: "Ops", Links: []glanceconf.Site{{Title: "Kuma", URL: "http://x"}}},
				}},
			}},
		},
	}}}
}

// The frame must never be wider than the terminal: an over-wide row wraps
// and garbles every pane, which reads as "only the left pane rendered".
func TestViewNeverExceedsTerminalWidth(t *testing.T) {
	for _, w := range []int{40, 50, 60, 70, 80, 90, 100, 120, 160, 200} {
		m := New(Options{Config: testConfig()})
		m.width, m.height = w, 40
		for i, line := range strings.Split(m.View(), "\n") {
			if got := lipgloss.Width(line); got > w {
				t.Errorf("width=%d: line %d is %d cols (overflow %d)", w, i, got, got-w)
				break
			}
		}
	}
}

// Wide terminals share the width; a phone-width one keeps every pane but
// stacks them down the screen instead of dropping any.
func TestLayoutDropsPanesWhenNarrow(t *testing.T) {
	cases := []struct {
		width  int
		want   []pane
		stacks bool
	}{
		{200, []pane{paneLeft, paneMiddle, paneRight}, false},
		{60, []pane{paneLeft, paneMiddle}, false},
		{40, []pane{paneLeft, paneMiddle, paneRight}, true},
	}
	for _, tc := range cases {
		m := New(Options{Config: testConfig()})
		m.width, m.height = tc.width, 40
		var got []pane
		sumW, sumH := 0, 0
		for _, l := range m.layout() {
			got = append(got, l.pane)
			sumW += l.width
			sumH += l.height
		}
		if len(got) != len(tc.want) {
			t.Errorf("width=%d: got %d panes, want %d", tc.width, len(got), len(tc.want))
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("width=%d: pane %d = %v, want %v", tc.width, i, got[i], tc.want[i])
			}
		}
		if m.stacked() != tc.stacks {
			t.Errorf("width=%d: stacked = %v, want %v", tc.width, m.stacked(), tc.stacks)
		}
		if tc.stacks {
			// Stacked panes each take the full width and split the height.
			if sumH != m.bodyHeight() {
				t.Errorf("width=%d: pane heights sum to %d, want %d", tc.width, sumH, m.bodyHeight())
			}
			for _, l := range m.layout() {
				if l.width != tc.width {
					t.Errorf("width=%d: stacked pane %v is %d cols", tc.width, l.pane, l.width)
				}
			}
		} else if sumW != tc.width {
			t.Errorf("width=%d: pane widths sum to %d", tc.width, sumW)
		}
	}
}

// Stacking gives the focused pane the leftover rows, so tab doubles as
// "expand this one" when the panes are too narrow to sit side by side.
func TestStackedFocusGetsExtraRows(t *testing.T) {
	m := New(Options{Config: testConfig()})
	m.width, m.height = 40, 40
	m.focus = paneRight

	for _, l := range m.layout() {
		if l.pane == paneRight && l.height <= minStackH {
			t.Errorf("focused pane got %d rows, want more than the %d minimum", l.height, minStackH)
		}
		if l.pane != paneRight && l.height != minStackH {
			t.Errorf("unfocused pane %v got %d rows, want %d", l.pane, l.height, minStackH)
		}
	}
}

// Tab must not park focus on a pane the current size hides.
func TestNextPaneSkipsHidden(t *testing.T) {
	m := New(Options{Config: testConfig()})
	m.width, m.height = 40, 9 // stacked, too short for more than the feed
	m.focus = paneMiddle
	if got := m.nextPane(+1); got != paneMiddle {
		t.Errorf("nextPane = %v, want paneMiddle (only visible pane)", got)
	}

	m.width, m.height = 60, 40 // left + middle
	m.focus = paneMiddle
	if got := m.nextPane(+1); got != paneLeft {
		t.Errorf("nextPane = %v, want paneLeft (right is hidden)", got)
	}
}

// A frame taller than the terminal scrolls it, pushing the header off the
// top — the user sees the bottom of the frame and cannot get back.
func TestViewNeverExceedsTerminalHeight(t *testing.T) {
	for _, w := range []int{40, 50, 120} { // 40/50 stack, 120 sits side by side
		for _, h := range []int{10, 14, 20, 24, 30, 40, 60} {
			m := New(Options{Config: testConfig()})
			m.width, m.height = w, h
			if got := len(strings.Split(m.View(), "\n")); got != h {
				t.Errorf("width=%d height=%d: rendered %d rows (excess %d)", w, h, got, got-h)
			}
		}
	}
}

// Long content must clip to the pane rather than growing the box.
func TestViewClipsOverflowingContent(t *testing.T) {
	m := New(Options{Config: testConfig()})
	m.width, m.height = 120, 12
	for i := 0; i < 200; i++ {
		m.recipes = append(m.recipes, sources.Recipe{Name: fmt.Sprintf("task-%d", i)})
	}
	if got := len(strings.Split(m.View(), "\n")); got != 12 {
		t.Errorf("200 actions rendered %d rows, want 12", got)
	}
}

func TestWindowClampsAndMarks(t *testing.T) {
	lines := []string{"a", "b", "c", "d", "e", "f"}

	got := window(lines, 0, 3)
	if len(got) != 3 {
		t.Fatalf("got %d rows, want 3", len(got))
	}
	if !strings.Contains(ansi.Strip(got[2]), "more") {
		t.Errorf("last row should mark hidden content, got %q", got[2])
	}

	// Past the end clamps to the final window rather than blanking out.
	got = window(lines, 999, 3)
	if !strings.Contains(ansi.Strip(got[0]), "more") {
		t.Errorf("clamped window should mark content above, got %q", got[0])
	}
	if plain := ansi.Strip(got[2]); plain != "f" {
		t.Errorf("clamped window should end at last line, got %q", plain)
	}

	// Short content pads to the requested height so panes stay stable.
	if got := window([]string{"a"}, 0, 4); len(got) != 4 {
		t.Errorf("short content produced %d rows, want 4", len(got))
	}
}

func TestScrollToKeepsCursorVisible(t *testing.T) {
	const h, total = 5, 40
	for _, cur := range []int{0, 3, 20, 39} {
		off := scrollTo(cur, 0, h, total)
		if cur < off || cur >= off+h {
			t.Errorf("cursor %d not visible in window [%d,%d)", cur, off, off+h)
		}
		if off < 0 || off > total-h {
			t.Errorf("offset %d out of range for total=%d h=%d", off, total, h)
		}
	}
	if got := scrollTo(3, 0, 10, 4); got != 0 {
		t.Errorf("content shorter than viewport should not scroll, got %d", got)
	}
}

// The cursor pane scrolls to follow the selection, so an action far down
// the list is still on screen when selected.
func TestLeftPaneFollowsCursor(t *testing.T) {
	m := New(Options{Config: testConfig()})
	m.width, m.height = 120, 14
	for i := 0; i < 60; i++ {
		m.recipes = append(m.recipes, sources.Recipe{Name: fmt.Sprintf("task-%d", i)})
	}
	m.focus = paneLeft
	m.leftCur = 55
	if !strings.Contains(ansi.Strip(m.View()), "task-54") {
		t.Error("selected action scrolled out of view")
	}
}

func servicesReport(n int) sources.ServiceStatusReport {
	rep := sources.ServiceStatusReport{BackupSLAHours: 30}
	for i := 0; i < n; i++ {
		rep.Services = append(rep.Services, sources.ServiceStatus{
			Name:        fmt.Sprintf("svc%d", i),
			Running:     true,
			BackupFresh: true,
		})
	}
	return rep
}

func TestRenderServicesGridsFourAcross(t *testing.T) {
	lines := renderServices(servicesReport(9), 80)
	if len(lines) != 3 {
		t.Fatalf("9 services at width 80 → %d rows, want 3", len(lines))
	}
	for i, want := range []int{4, 4, 1} {
		if got := strings.Count(ansi.Strip(lines[i]), "●●●"); got != want {
			t.Errorf("row %d has %d services, want %d", i, got, want)
		}
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w > 80 {
			t.Errorf("row %d is %d wide, want <= 80", i, w)
		}
	}
}

func TestRenderServicesNarrowPaneDropsColumns(t *testing.T) {
	lines := renderServices(servicesReport(4), 20)
	if len(lines) != 4 {
		t.Fatalf("width 20 → %d rows, want one service per row", len(lines))
	}
}

func TestRenderServicesDetailsFollowGrid(t *testing.T) {
	rep := servicesReport(4)
	rep.Services[2].Running = false
	lines := renderServices(rep, 80)
	if len(lines) != 2 {
		t.Fatalf("got %d lines, want grid row + 1 detail", len(lines))
	}
	if !strings.Contains(ansi.Strip(lines[1]), "svc2  down") {
		t.Errorf("detail line = %q", ansi.Strip(lines[1]))
	}
}
