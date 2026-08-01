package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/kjaymiller/glancectl/internal/glanceconf"
	"github.com/kjaymiller/glancectl/internal/sources"
)

// Card is one block in the middle "feed" pane: a title plus N rendered
// lines of body. Lines may already contain lipgloss styling.
type Card struct {
	Title string
	Lines []string
	Err   error
}

// BuildCard dispatches by widget type/title and renders the card body.
// Returned cards always have a Title; Lines may be empty when nothing
// matched or the fetch failed (Err carries the reason). width is the
// content width of the pane the card will be rendered into; cards that
// lay content out in columns size themselves against it.
func BuildCard(w glanceconf.Widget, data any, width int) Card {
	c := Card{Title: w.Title}
	switch {
	case w.Type == "weather":
		c.Title = "Weather — " + w.Location
		if data == nil {
			c.Lines = []string{subtle.Render("…")}
			return c
		}
		if e, ok := data.(error); ok {
			c.Err = e
			c.Lines = []string{bad.Render(e.Error())}
			return c
		}
		wx := data.(sources.Weather)
		c.Lines = []string{
			fmt.Sprintf("%s%s  %s", accent.Render(fmt.Sprintf("%.0f", wx.Temperature)), wx.Unit, wx.Description),
			subtle.Render(wx.Place),
		}

	case w.Type == "custom-api" && contains(strings.ToLower(w.Title), "kuma"):
		if data == nil {
			c.Lines = []string{subtle.Render("…")}
			return c
		}
		if e, ok := data.(error); ok {
			c.Err = e
			c.Lines = []string{bad.Render(e.Error())}
			return c
		}
		rep := data.(sources.KumaReport)
		c.Lines = renderKuma(rep, time.Now())

	case w.Type == "custom-api" && contains(strings.ToLower(w.Title), "prometheus"):
		if data == nil {
			c.Lines = []string{subtle.Render("…")}
			return c
		}
		if e, ok := data.(error); ok {
			c.Err = e
			c.Lines = []string{bad.Render(e.Error())}
			return c
		}
		c.Lines = renderPromSeries(data.(sources.PromSeries))

	// Kept ahead of the other custom-api cases for the same reason as the
	// fetch dispatch in model.go: "System Updates" must not fall into the
	// "update" case.
	case w.Type == "custom-api" && contains(strings.ToLower(w.Title), "system"):
		if data == nil {
			c.Lines = []string{subtle.Render("…")}
			return c
		}
		if e, ok := data.(error); ok {
			c.Err = e
			c.Lines = []string{bad.Render(e.Error())}
			return c
		}
		rep := data.(sources.ServiceStatusReport)
		c.Lines = renderServices(rep, width)

	case w.Type == "custom-api" && contains(strings.ToLower(w.Title), "brave"):
		if data == nil {
			c.Lines = []string{subtle.Render("…")}
			return c
		}
		if e, ok := data.(error); ok {
			c.Err = e
			c.Lines = []string{bad.Render(e.Error())}
			return c
		}
		games := data.([]sources.Game)
		c.Lines = renderGames(games)

	case w.Type == "custom-api" && contains(strings.ToLower(w.Title), "update"):
		if data == nil {
			c.Lines = []string{subtle.Render("…")}
			return c
		}
		if e, ok := data.(error); ok {
			c.Err = e
			c.Lines = []string{bad.Render(e.Error())}
			return c
		}
		ups := data.([]sources.ContainerUpdate)
		c.Lines = renderUpdates(ups)

	case w.Type == "custom-api" && contains(strings.ToLower(w.Title), "channels"):
		if data == nil {
			c.Lines = []string{subtle.Render("…")}
			return c
		}
		if e, ok := data.(error); ok {
			c.Err = e
			c.Lines = []string{bad.Render(e.Error())}
			return c
		}
		st := data.(sources.YtdlChannelStats)
		c.Lines = []string{
			fmt.Sprintf("%s channels  %s files",
				accent.Render(fmt.Sprintf("%d", st.Channels)),
				accent.Render(fmt.Sprintf("%d", st.Files)),
			),
		}
		if st.LastUnix > 0 {
			t := time.Unix(st.LastUnix, 0).Local()
			c.Lines = append(c.Lines, subtle.Render("last download "+t.Format("Jan 2, 15:04")))
		}

	case w.Type == "custom-api" && contains(strings.ToLower(w.Title), "runs"):
		if data == nil {
			c.Lines = []string{subtle.Render("…")}
			return c
		}
		if e, ok := data.(error); ok {
			c.Err = e
			c.Lines = []string{bad.Render(e.Error())}
			return c
		}
		runs := data.([]sources.YtdlRun)
		if len(runs) == 0 {
			c.Lines = []string{subtle.Render("no runs yet")}
		}
		for _, r := range runs {
			mark := good.Render("✓")
			if r.Failed {
				mark = bad.Render("✗")
			}
			c.Lines = append(c.Lines, fmt.Sprintf("%s %s", mark, r.When.Format("Jan 2, 15:04")))
		}

	case w.Type == "custom-api" && contains(strings.ToLower(w.Title), "alert"):
		if data == nil {
			c.Lines = []string{subtle.Render("…")}
			return c
		}
		if e, ok := data.(error); ok {
			c.Err = e
			c.Lines = []string{bad.Render(e.Error())}
			return c
		}
		n := data.(int)
		if n == 0 {
			c.Lines = []string{good.Render("no active alerts")}
		} else {
			c.Lines = []string{warn.Render(fmt.Sprintf("%d active", n))}
		}

	default:
		c.Lines = []string{subtle.Render("(no renderer for type=" + w.Type + ")")}
	}
	return c
}

// kumaNameW is the column the monitor names are padded to, so the bars
// line up into a readable grid down the card.
const kumaNameW = 14

// renderKuma draws one row per monitor: name, the binary 24h bar, and the
// uptime percentage Kuma itself computed (we show its number rather than
// deriving one from the beats, which only cover as far back as the status
// page keeps them).
func renderKuma(rep sources.KumaReport, now time.Time) []string {
	if len(rep.Monitors) == 0 {
		return []string{subtle.Render("no monitors on this status page")}
	}
	window := rep.Window
	if window <= 0 {
		window = sources.KumaWindow
	}
	out := make([]string, 0, len(rep.Monitors)+1)
	for _, m := range rep.Monitors {
		name := truncate(m.Name, kumaNameW)
		row := fmt.Sprintf("%-*s %s", kumaNameW, name, uptimeBar(m.Beats, window, now))
		if m.HasUptime {
			pct := m.Uptime24h * 100
			st := good
			switch {
			case pct < 99:
				st = bad
			case pct < 99.9:
				st = warn
			}
			row += "  " + st.Render(fmt.Sprintf("%.2f%%", pct))
		}
		out = append(out, row)
	}
	out = append(out, subtle.Render(fmt.Sprintf("last %s · %d monitors · ", shortDur(window), len(rep.Monitors)))+
		good.Render("█")+subtle.Render(" up ")+
		bad.Render("▁")+subtle.Render(" down ")+
		subtle.Render("· no data"))
	return out
}

// renderPromSeries draws the current value big, the sparkline under it,
// and the window's low/high as context for the shape.
func renderPromSeries(s sources.PromSeries) []string {
	if len(s.Points) == 0 {
		return []string{subtle.Render("no data")}
	}
	lo, hi, _ := s.MinMax()
	cur, _ := s.Current()

	head := accent.Bold(true).Render(formatValue(cur, s.Format, s.Unit))
	if s.Label != "" {
		head += "  " + subtle.Render(truncate(s.Label, 48))
	}
	return []string{
		head,
		sparkline(s.Points, lo, hi),
		subtle.Render(fmt.Sprintf("%s · low %s · high %s",
			shortDur(s.Range),
			formatValue(lo, s.Format, s.Unit),
			formatValue(hi, s.Format, s.Unit),
		)),
	}
}

func renderGames(games []sources.Game) []string {
	if len(games) == 0 {
		return []string{subtle.Render("no scheduled games")}
	}
	out := []string{}
	for _, g := range games {
		loc := "vs"
		if !g.Home {
			loc = "@"
		}
		mark := subtle.Render("·")
		switch {
		case strings.HasPrefix(g.Status, "Final") && g.Self > g.Other:
			mark = good.Render("✓")
		case strings.HasPrefix(g.Status, "Final") && g.Self < g.Other:
			mark = bad.Render("✗")
		case g.Status == "In Progress":
			mark = warn.Render("●")
		}
		date := g.When.Format("Mon Jan 2")
		score := g.Status
		switch {
		case strings.HasPrefix(g.Status, "Final"):
			score = fmt.Sprintf("%s %d-%d", g.Status, g.Self, g.Other)
		case strings.HasPrefix(g.Status, "In Progress"), g.Status == "Manager Challenge", g.Status == "Delayed":
			parts := []string{fmt.Sprintf("%d-%d", g.Self, g.Other)}
			if g.Inning > 0 {
				half := g.InningHalf
				if half == "" {
					half = "—"
				}
				parts = append(parts, fmt.Sprintf("%s %s", half, ordinal(g.Inning)))
			}
			if g.Inning > 0 {
				parts = append(parts, fmt.Sprintf("%d out", g.Outs))
			}
			if bases := basesGlyph(g.OnFirst, g.OnSecond, g.OnThird); bases != "" {
				parts = append(parts, bases)
			}
			score = strings.Join(parts, " · ")
		case g.When.After(time.Now()):
			score = g.When.Format("3:04pm")
		}
		out = append(out, fmt.Sprintf("%s %s %s %s — %s", mark, date, loc, g.Opponent, score))
	}
	return out
}

func renderUpdates(ups []sources.ContainerUpdate) []string {
	if len(ups) == 0 {
		return []string{good.Render("everything current")}
	}
	out := []string{}
	for _, u := range ups {
		tag := subtle.Render(fmt.Sprintf("%s → %s", u.OldTag, u.NewTag))
		note := ""
		switch {
		case u.Actionable:
			note = good.Render("[A]")
		case u.Tier == "A" && u.IsMajor:
			note = warn.Render("(major → manual)")
		case u.Tier == "B":
			note = subtle.Render("(B — manual)")
		}
		out = append(out, fmt.Sprintf("%s %s %s", accent.Render(u.Name), tag, note))
	}
	return out
}

// servicesGridCols is how many services sit side by side when the pane is
// wide enough. Narrower panes fall back to fewer columns (see
// servicesColumns) rather than truncating names to nothing.
const servicesGridCols = 4

// servicesMinCellW is the narrowest a grid cell can be and still show the
// three lights, a space, and a few characters of name.
const servicesMinCellW = 14

// renderServices lays the services out as a grid of "●●● name" cells —
// three status lights per service, read left to right, wrapping every
// servicesColumns entries. Anything actually wrong (down, stale backup,
// update available) gets its own detail line under the grid, since that
// text does not fit in a cell.
func renderServices(rep sources.ServiceStatusReport, width int) []string {
	if len(rep.Services) == 0 {
		return []string{subtle.Render("no services reported")}
	}

	cols := servicesColumns(width)
	cellW := width / cols

	var out []string
	var row strings.Builder
	for i, s := range rep.Services {
		// 3 lights + 1 space of gutter; the rest is the name, with one
		// trailing space so adjacent cells never touch.
		name := truncate(s.Name, cellW-5)
		cell := fmt.Sprintf("%s %s", statusGlyph(s), accent.Render(name))
		if pad := cellW - 4 - len(name); pad > 0 && (i+1)%cols != 0 {
			cell += strings.Repeat(" ", pad)
		}
		row.WriteString(cell)
		if (i+1)%cols == 0 {
			out = append(out, row.String())
			row.Reset()
		}
	}
	if row.Len() > 0 {
		// The last cell in a short final row still carries its padding.
		out = append(out, strings.TrimRight(row.String(), " "))
	}

	for _, s := range rep.Services {
		var notes []string
		if !s.Running {
			notes = append(notes, bad.Render("down"))
		}
		if s.UpdateAvailable {
			notes = append(notes, warn.Render(fmt.Sprintf("%s → %s", s.Current, s.Latest)))
		}
		if !s.BackupFresh {
			detail := fmt.Sprintf("backup stale (%.0fh > %.0fh)", s.BackupAgeHours, rep.BackupSLAHours)
			notes = append(notes, bad.Render(detail))
		}
		if len(notes) > 0 {
			out = append(out, fmt.Sprintf("%s  %s", accent.Render(s.Name),
				strings.Join(notes, subtle.Render(" · "))))
		}
	}
	return out
}

// servicesColumns picks the column count that fits: the full grid when
// there is room, dropping to fewer (down to one) on a narrow pane.
func servicesColumns(width int) int {
	for c := servicesGridCols; c > 1; c-- {
		if width/c >= servicesMinCellW {
			return c
		}
	}
	return 1
}

// statusGlyph renders the three lights — running, backed up, up to date —
// as "●●●", green per passing light, amber for an available update (it is
// actionable, not broken), red otherwise.
func statusGlyph(s sources.ServiceStatus) string {
	dot := func(ok bool) string {
		if ok {
			return good.Render("●")
		}
		return bad.Render("●")
	}
	upToDate := good.Render("●")
	if s.UpdateAvailable {
		upToDate = warn.Render("●")
	}
	return dot(s.Running) + dot(s.BackupFresh) + upToDate
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}

// basesGlyph renders the runners-on-base state as a compact 3-char string
// like "1-3" (first & third), "-2-" (just second), or "" if bases empty.
func basesGlyph(first, second, third bool) string {
	if !first && !second && !third {
		return ""
	}
	g := []byte("---")
	if first {
		g[0] = '1'
	}
	if second {
		g[1] = '2'
	}
	if third {
		g[2] = '3'
	}
	return string(g)
}

// ordinal renders 1→"1st", 2→"2nd", etc. — for inning labels.
func ordinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}
