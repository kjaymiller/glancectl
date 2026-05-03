package sources

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

type Game struct {
	When       time.Time
	Status     string
	Home       bool
	Opponent   string
	Self       int
	Other      int
	GamePk     int
	Inning     int    // current inning (in-progress games)
	InningHalf string // "Top" / "Bottom" / "Middle" / "End"
	Outs       int
	OnFirst    bool
	OnSecond   bool
	OnThird    bool
}

// ScheduleWindow is the number of games returned around today.
// 2 past + today/next + 2 after the focal game = 5 total.
const ScheduleWindow = 5

// FetchSchedule pulls the MLB statsapi schedule and returns a window of
// ScheduleWindow games centered on the one closest to "now". parameters
// comes straight from the Glance widget (sportId, teamId, season,
// hydrate).
func FetchSchedule(ctx context.Context, baseURL string, parameters map[string]string, timeout time.Duration) ([]Game, error) {
	teamID := parameters["teamId"]
	if teamID == "" {
		return nil, fmt.Errorf("teamId missing from parameters")
	}
	q := url.Values{}
	for k, v := range parameters {
		q.Set(k, v)
	}
	full := baseURL + "?" + q.Encode()
	v, err := FetchJSON(ctx, full, nil, timeout)
	if err != nil {
		return nil, err
	}
	root, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("schedule: unexpected root")
	}
	dates, _ := root["dates"].([]any)
	var all []Game
	for _, d := range dates {
		dm, _ := d.(map[string]any)
		games, _ := dm["games"].([]any)
		for _, g := range games {
			gm, _ := g.(map[string]any)
			all = append(all, parseGame(gm, teamID))
		}
	}
	return windowAroundNow(all, ScheduleWindow), nil
}

// windowAroundNow picks `size` games centered on the one whose date is
// closest to now. Falls back to the first/last `size` if the schedule
// is shorter or we're past the end. Assumes input is in chronological
// order (statsapi returns it that way).
func windowAroundNow(games []Game, size int) []Game {
	if len(games) <= size {
		return games
	}
	now := time.Now()
	focal := 0
	bestDiff := time.Duration(1<<62 - 1)
	for i, g := range games {
		if g.When.IsZero() {
			continue
		}
		d := g.When.Sub(now)
		if d < 0 {
			d = -d
		}
		if d < bestDiff {
			bestDiff = d
			focal = i
		}
	}
	half := size / 2
	start := focal - half
	end := start + size
	if start < 0 {
		end -= start
		start = 0
	}
	if end > len(games) {
		start -= end - len(games)
		end = len(games)
	}
	if start < 0 {
		start = 0
	}
	return games[start:end]
}

func parseGame(g map[string]any, teamID string) Game {
	out := Game{}
	out.GamePk = intOf(g["gamePk"])
	if s, ok := g["status"].(map[string]any); ok {
		out.Status, _ = s["detailedState"].(string)
	}
	if t, ok := g["gameDate"].(string); ok {
		if parsed, err := time.Parse(time.RFC3339, t); err == nil {
			out.When = parsed.Local()
		}
	}
	if ls, ok := g["linescore"].(map[string]any); ok {
		out.Inning = intOf(ls["currentInning"])
		out.InningHalf, _ = ls["inningState"].(string)
		out.Outs = intOf(ls["outs"])
		if off, ok := ls["offense"].(map[string]any); ok {
			_, out.OnFirst = off["onFirst"].(map[string]any)
			_, out.OnSecond = off["onSecond"].(map[string]any)
			_, out.OnThird = off["onThird"].(map[string]any)
		}
	}
	teams, _ := g["teams"].(map[string]any)
	home, _ := teams["home"].(map[string]any)
	away, _ := teams["away"].(map[string]any)
	homeID := teamIDOf(home)
	awayID := teamIDOf(away)
	homeAbbr := teamAbbrOf(home)
	awayAbbr := teamAbbrOf(away)
	homeScore := intOf(scoreOf(home))
	awayScore := intOf(scoreOf(away))
	if fmt.Sprint(homeID) == teamID {
		out.Home = true
		out.Opponent = awayAbbr
		out.Self = homeScore
		out.Other = awayScore
	} else if fmt.Sprint(awayID) == teamID {
		out.Home = false
		out.Opponent = homeAbbr
		out.Self = awayScore
		out.Other = homeScore
	}
	return out
}

func teamIDOf(side map[string]any) int {
	t, _ := side["team"].(map[string]any)
	return intOf(t["id"])
}

func teamAbbrOf(side map[string]any) string {
	t, _ := side["team"].(map[string]any)
	s, _ := t["abbreviation"].(string)
	return s
}

func scoreOf(side map[string]any) any {
	return side["score"]
}

func intOf(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	}
	return 0
}
