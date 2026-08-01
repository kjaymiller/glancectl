package sources

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Kuma heartbeat status codes, as stored by Uptime Kuma.
const (
	KumaDown        = 0
	KumaUp          = 1
	KumaPending     = 2
	KumaMaintenance = 3
)

// KumaBeat is one heartbeat: when it was taken and what it found.
type KumaBeat struct {
	At     time.Time
	Status int
}

// Up reports whether the beat counts as available. Maintenance is not a
// failure of the service, so it is kept out of the down column — the
// renderer gives it its own glyph.
func (b KumaBeat) Up() bool { return b.Status == KumaUp }

// KumaMonitor is one row of the uptime graph.
type KumaMonitor struct {
	ID        string
	Name      string
	Beats     []KumaBeat // ascending by time
	Uptime24h float64    // 0..1
	HasUptime bool
}

// KumaReport is the whole status page: one monitor per row, over Window.
type KumaReport struct {
	Window   time.Duration
	Monitors []KumaMonitor
}

// KumaWindow is the span the binary graph covers.
const KumaWindow = 24 * time.Hour

// FetchKumaUptime reads a Uptime Kuma *public status page* and returns
// each monitor's recent heartbeats. It hits two endpoints:
//
//	/api/status-page/heartbeat/<slug>  — beats + 24h uptime, keyed by monitor id
//	/api/status-page/<slug>            — the group list, which carries the names
//
// The names call is best-effort: if it fails the monitors still render,
// labelled by id. Note that Kuma answers the heartbeat endpoint with an
// empty skeleton rather than a 404 when the slug has no public status
// page, so an empty report is reported as an error to distinguish "no
// status page" from "everything is fine".
func FetchKumaUptime(ctx context.Context, statusURL string, headers map[string]string, timeout time.Duration) (KumaReport, error) {
	out := KumaReport{Window: KumaWindow}

	v, err := FetchJSON(ctx, kumaHeartbeatURL(statusURL), headers, timeout)
	if err != nil {
		return out, err
	}
	root, ok := v.(map[string]any)
	if !ok {
		return out, fmt.Errorf("kuma: unexpected root")
	}
	beats, _ := root["heartbeatList"].(map[string]any)
	uptime, _ := root["uptimeList"].(map[string]any)
	if len(beats) == 0 {
		return out, fmt.Errorf("kuma: status page has no monitors (is %q public?)", kumaSlug(statusURL))
	}

	names := kumaMonitorNames(ctx, statusURL, headers, timeout)

	cutoff := time.Now().Add(-KumaWindow)
	for id, raw := range beats {
		m := KumaMonitor{ID: id, Name: names.name[id]}
		if m.Name == "" {
			m.Name = "monitor " + id
		}
		list, _ := raw.([]any)
		for _, item := range list {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			at, ok := kumaTime(obj["time"])
			if !ok || at.Before(cutoff) {
				continue
			}
			m.Beats = append(m.Beats, KumaBeat{At: at, Status: intOf(obj["status"])})
		}
		sort.Slice(m.Beats, func(i, j int) bool { return m.Beats[i].At.Before(m.Beats[j].At) })
		if u, ok := uptime[id+"_24"]; ok {
			m.Uptime24h = floatOf(u)
			m.HasUptime = true
		}
		out.Monitors = append(out.Monitors, m)
	}

	// heartbeatList is a JSON object, so range order is random. Sort by the
	// status page's own group order where we have it, and by numeric id
	// otherwise, so the card does not reshuffle on every refresh.
	sort.SliceStable(out.Monitors, func(i, j int) bool {
		oi, iok := names.order[out.Monitors[i].ID]
		oj, jok := names.order[out.Monitors[j].ID]
		if iok && jok {
			return oi < oj
		}
		if iok != jok {
			return iok
		}
		ni, ei := strconv.Atoi(out.Monitors[i].ID)
		nj, ej := strconv.Atoi(out.Monitors[j].ID)
		if ei == nil && ej == nil {
			return ni < nj
		}
		return out.Monitors[i].ID < out.Monitors[j].ID
	})
	return out, nil
}

// kumaNames maps monitor id to display name, remembering the order the
// status page listed them in.
type kumaNames struct {
	name  map[string]string
	order map[string]int
}

// kumaMonitorNames fetches the status page config for its group list.
// Failure is not fatal: an empty table means every monitor falls back to
// its id, which is still a usable graph.
func kumaMonitorNames(ctx context.Context, statusURL string, headers map[string]string, timeout time.Duration) kumaNames {
	names := kumaNames{name: map[string]string{}, order: map[string]int{}}

	v, err := FetchJSON(ctx, kumaConfigURL(statusURL), headers, timeout)
	if err != nil {
		return names
	}
	root, ok := v.(map[string]any)
	if !ok {
		return names
	}
	groups, _ := root["publicGroupList"].([]any)
	i := 0
	for _, g := range groups {
		gm, _ := g.(map[string]any)
		monitors, _ := gm["monitorList"].([]any)
		for _, mo := range monitors {
			om, ok := mo.(map[string]any)
			if !ok {
				continue
			}
			id := fmt.Sprint(intOf(om["id"]))
			name, _ := om["name"].(string)
			if name != "" {
				names.name[id] = name
			}
			names.order[id] = i
			i++
		}
	}
	return names
}

// kumaTime parses the timestamps Kuma emits: either ISO with a zone, or a
// zoneless datetime like "2026-08-01 17:04:35.252".
//
// The zoneless form is UTC. Kuma stores heartbeats in UTC and the status
// page API serialises them without a zone, regardless of the server's
// display timezone. Reading them as local time instead puts every beat off
// by the UTC offset — west of Greenwich that lands them in the future,
// where the renderer discards them as out-of-window and draws an empty bar
// beside a perfectly healthy 100% uptime figure.
func kumaTime(v any) (time.Time, bool) {
	s, _ := v.(string)
	if s == "" {
		return time.Time{}, false
	}
	for _, l := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	for _, l := range []string{"2006-01-02 15:04:05.999", "2006-01-02 15:04:05"} {
		if t, err := time.ParseInLocation(l, s, time.UTC); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// kumaHeartbeatURL accepts either the heartbeat endpoint itself or the
// status page URL a user would paste from their browser, and returns the
// heartbeat endpoint.
func kumaHeartbeatURL(u string) string {
	base, slug := kumaSplit(u)
	return base + "/api/status-page/heartbeat/" + slug
}

func kumaConfigURL(u string) string {
	base, slug := kumaSplit(u)
	return base + "/api/status-page/" + slug
}

func kumaSlug(u string) string {
	_, slug := kumaSplit(u)
	return slug
}

// kumaSplit pulls the origin and the status page slug out of any of the
// shapes a user might configure: the heartbeat API path, the status page
// API path, or the /status/<slug> page itself.
func kumaSplit(u string) (base, slug string) {
	u = strings.TrimRight(u, "/")
	for _, prefix := range []string{"/api/status-page/heartbeat/", "/api/status-page/", "/status/"} {
		if i := strings.LastIndex(u, prefix); i >= 0 {
			return u[:i], u[i+len(prefix):]
		}
	}
	return u, "default"
}
