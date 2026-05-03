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
// matched or the fetch failed (Err carries the reason).
func BuildCard(w glanceconf.Widget, data any) Card {
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
		if strings.HasPrefix(g.Status, "Final") {
			score = fmt.Sprintf("%s %d-%d", g.Status, g.Self, g.Other)
		} else if g.When.After(time.Now()) {
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

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
