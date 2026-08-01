package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kjaymiller/glancectl/internal/glanceconf"
)

func yankModel() Model {
	m := New(Options{Config: testConfig()})
	m.width, m.height = 120, 40
	m.midWidgets = []glanceconf.Widget{
		{Type: "custom-api", Title: "Active Alerts", URL: "https://alerts.example/api/v2/alerts"},
		{Type: "custom-api", Title: "Systems", URL: "http://service-status:5000/services.json"},
	}
	m.midData = []any{
		fmt.Errorf("dial tcp: connection refused"),
		fmt.Errorf("500 Internal Server Error"),
	}
	m.alertIdx = 0
	return m
}

// The whole point of the yank is getting an unselectable error out of the
// alt-screen, so the failure detail must survive into the copied text.
func TestPaneTextCarriesErrors(t *testing.T) {
	m := yankModel()

	left := m.paneText(paneLeft)
	for _, want := range []string{"Active Alerts", "https://alerts.example/api/v2/alerts", "connection refused"} {
		if !strings.Contains(left, want) {
			t.Errorf("left pane text missing %q:\n%s", want, left)
		}
	}

	mid := m.paneText(paneMiddle)
	for _, want := range []string{"Systems", "http://service-status:5000/services.json", "500 Internal Server Error"} {
		if !strings.Contains(mid, want) {
			t.Errorf("middle pane text missing %q:\n%s", want, mid)
		}
	}
}

// Pasted text must be plain: no escape sequences, and no hard wrapping
// from the rendered box (which would split a long URL mid-word).
func TestPaneTextIsPlain(t *testing.T) {
	m := yankModel()
	for _, p := range []pane{paneLeft, paneMiddle, paneRight} {
		got := m.paneText(p)
		if got != ansi.Strip(got) {
			t.Errorf("pane %v text contains ANSI escapes: %q", p, got)
		}
	}
	// The source URL is longer than a pane is wide; it must stay on one line.
	for _, line := range strings.Split(m.paneText(paneMiddle), "\n") {
		if strings.Contains(line, "service-status:5000/services.json") {
			return
		}
	}
	t.Error("middle pane text wrapped the source URL")
}

func TestPaneTextBookmarksIncludeURLs(t *testing.T) {
	m := yankModel()
	got := m.paneText(paneRight)
	if !strings.Contains(got, "Kuma") || !strings.Contains(got, "http://x") {
		t.Errorf("bookmark text missing title or URL:\n%s", got)
	}
}
