package ui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

// copyText puts s on the system clipboard and returns a short description
// of how it got there, for the status line.
//
// An external helper is tried first because it survives the alt-screen and
// works when the terminal has OSC 52 disabled (many do, since it lets a
// remote host write the local clipboard). OSC 52 is the fallback since it
// is the only option that works over SSH with no helper installed. If both
// are unavailable the text is spilled to a file, which is still better than
// losing an error the user cannot select with the mouse.
// The bool reports whether a real helper handled it; OSC 52 is best-effort
// and gives no completion signal, so callers pair it with a spill file.
func copyText(s string) (string, bool, error) {
	for _, c := range [][]string{
		{"wl-copy"},
		{"xclip", "-selection", "clipboard"},
		{"xsel", "--clipboard", "--input"},
		{"pbcopy"},
	} {
		path, err := exec.LookPath(c[0])
		if err != nil {
			continue
		}
		cmd := exec.Command(path, c[1:]...)
		cmd.Stdin = strings.NewReader(s)
		if err := cmd.Run(); err != nil {
			return "", false, fmt.Errorf("%s: %w", c[0], err)
		}
		return c[0], true, nil
	}

	// No helper. OSC 52 writes the clipboard through the terminal itself.
	termenv.Copy(s)
	return "OSC 52", false, nil
}

// spillFile writes s somewhere the user can cat it, for when no clipboard
// path worked at all.
func spillFile(s string) (string, error) {
	f, err := os.CreateTemp(os.TempDir(), "glancectl-*.txt")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.WriteString(s); err != nil {
		return "", err
	}
	return filepath.Clean(f.Name()), nil
}

// paneText renders the focused pane as plain, unwrapped text.
//
// It deliberately rebuilds from the model rather than stripping ANSI off
// the rendered box: the box is wrapped and padded to fit the terminal, so
// a long error would come out hard-wrapped mid-word. This also lets each
// pane include detail the compact view omits — URLs, HTTP status codes —
// which is the whole point when copying a failure out for a bug report.
func (m Model) paneText(p pane) string {
	var b strings.Builder
	switch p {
	case paneLeft:
		if m.alertIdx >= 0 {
			w := m.midWidgets[m.alertIdx]
			c := BuildCard(w, m.midData[m.alertIdx], m.paneWidth(paneLeft))
			b.WriteString(c.Title + "\n")
			if w.URL != "" {
				fmt.Fprintf(&b, "  source: %s\n", w.URL)
			}
			for _, l := range c.Lines {
				fmt.Fprintf(&b, "  %s\n", ansi.Strip(l))
			}
		}
		if len(m.recipes) > 0 {
			b.WriteString("\nActions\n")
			for _, r := range m.recipes {
				if r.Group != "" {
					fmt.Fprintf(&b, "  [%s] %s\n", r.Group, r.Name)
				} else {
					fmt.Fprintf(&b, "  %s\n", r.Name)
				}
			}
		}

	case paneMiddle:
		for i, w := range m.midWidgets {
			if i == m.alertIdx { // lives in the left pane
				continue
			}
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			c := BuildCard(w, m.midData[i], m.paneWidth(paneMiddle))
			b.WriteString(c.Title + "\n")
			if w.URL != "" {
				fmt.Fprintf(&b, "  source: %s\n", w.URL)
			}
			for _, l := range c.Lines {
				fmt.Fprintf(&b, "  %s\n", ansi.Strip(l))
			}
		}

	case paneRight:
		for _, bm := range m.bookmarks {
			if bm.IsHeader {
				fmt.Fprintf(&b, "\n%s\n", bm.Title)
				continue
			}
			fmt.Fprintf(&b, "  %s\t%s\n", bm.Title, bm.URL)
		}
	}
	return strings.TrimSpace(b.String()) + "\n"
}

// yank copies the focused pane and returns the status line to show.
func (m Model) yank() string {
	text := m.paneText(m.focus)
	lines := strings.Count(strings.TrimRight(text, "\n"), "\n") + 1
	via, confirmed, err := copyText(text)
	if err == nil && confirmed {
		return fmt.Sprintf("copied %s (%d lines) via %s", paneName(m.focus), lines, via)
	}
	// Either the helper failed, or we fell back to OSC 52 and cannot tell
	// whether the terminal honored it. Leave a file either way so the text
	// is never simply lost.
	path, ferr := spillFile(text)
	switch {
	case err != nil && ferr != nil:
		return "copy failed: " + err.Error()
	case err != nil:
		return fmt.Sprintf("clipboard failed (%v) — wrote %s", err, path)
	case ferr != nil:
		return fmt.Sprintf("copied %s (%d lines) via %s", paneName(m.focus), lines, via)
	default:
		return fmt.Sprintf("copied %s via %s (unconfirmed) — also wrote %s", paneName(m.focus), via, path)
	}
}

func paneName(p pane) string {
	switch p {
	case paneLeft:
		return "Services/Actions"
	case paneMiddle:
		return "Feed"
	default:
		return "Bookmarks"
	}
}
