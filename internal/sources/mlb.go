package sources

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

type Game struct {
	When     time.Time
	Status   string
	Home     bool
	Opponent string
	Self     int
	Other    int
	GamePk   int
}

// FetchSchedule pulls the MLB statsapi schedule and filters to games
// involving teamId. parameters comes straight from the Glance widget
// (sportId, teamId, season, hydrate).
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
	var out []Game
	for _, d := range dates {
		dm, _ := d.(map[string]any)
		games, _ := dm["games"].([]any)
		for _, g := range games {
			gm, _ := g.(map[string]any)
			out = append(out, parseGame(gm, teamID))
		}
	}
	return out, nil
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
