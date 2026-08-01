package sources

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// kumaServer serves both status-page endpoints, so the fetcher's two-call
// shape is exercised the way it runs against a real Kuma.
func kumaServer(t *testing.T, heartbeat, config string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "/heartbeat/") {
			_, _ = w.Write([]byte(heartbeat))
			return
		}
		if config == "" {
			http.Error(w, `{"status":"fail"}`, http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(config))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// beatsFixture builds a heartbeat payload with timestamps relative to now,
// since the fetcher drops anything older than the 24h window.
func beatsFixture(t *testing.T) string {
	t.Helper()
	now := time.Now()
	// Kuma emits zoneless UTC; formatting in local time here would hide a
	// timezone bug in the parser rather than catch it.
	at := func(d time.Duration) string {
		return now.Add(-d).UTC().Format("2006-01-02 15:04:05.000")
	}
	return fmt.Sprintf(`{
	  "heartbeatList": {
	    "7": [
	      {"status": 1, "time": %q},
	      {"status": 0, "time": %q},
	      {"status": 1, "time": %q},
	      {"status": 1, "time": %q}
	    ],
	    "3": [
	      {"status": 1, "time": %q}
	    ]
	  },
	  "uptimeList": {"7": 0.99, "7_24": 0.9975, "3_24": 1}
	}`, at(3*time.Hour), at(2*time.Hour), at(time.Hour), at(30*time.Hour), at(time.Hour))
}

const kumaConfigFixture = `{
  "config": {"slug": "homelab", "title": "Homelab"},
  "publicGroupList": [
    {"name": "Core", "monitorList": [{"id": 7, "name": "whoami"}, {"id": 3, "name": "grafana"}]}
  ]
}`

func TestFetchKumaUptime(t *testing.T) {
	srv := kumaServer(t, beatsFixture(t), kumaConfigFixture)
	rep, err := FetchKumaUptime(context.Background(), srv.URL+"/api/status-page/heartbeat/homelab", nil, 2*time.Second)
	if err != nil {
		t.Fatalf("FetchKumaUptime: %v", err)
	}
	if len(rep.Monitors) != 2 {
		t.Fatalf("want 2 monitors, got %d", len(rep.Monitors))
	}

	// Status page order wins over map iteration order: 7 before 3.
	if rep.Monitors[0].Name != "whoami" || rep.Monitors[1].Name != "grafana" {
		t.Errorf("order/names = %q, %q; want whoami, grafana",
			rep.Monitors[0].Name, rep.Monitors[1].Name)
	}

	m := rep.Monitors[0]
	// The 30h-old beat is outside the window and must be dropped.
	if len(m.Beats) != 3 {
		t.Fatalf("want 3 in-window beats, got %d (%+v)", len(m.Beats), m.Beats)
	}
	for i := 1; i < len(m.Beats); i++ {
		if m.Beats[i].At.Before(m.Beats[i-1].At) {
			t.Errorf("beats not ascending: %+v", m.Beats)
		}
	}
	if !m.HasUptime || m.Uptime24h != 0.9975 {
		t.Errorf("Uptime24h = %v (has=%v), want 0.9975", m.Uptime24h, m.HasUptime)
	}
	if m.Beats[0].Status != KumaUp || !m.Beats[0].Up() {
		t.Errorf("first beat = %+v, want up", m.Beats[0])
	}
	if m.Beats[1].Up() {
		t.Errorf("second beat should be down: %+v", m.Beats[1])
	}
}

// Without a reachable config endpoint the graph still renders, labelled
// by monitor id and ordered numerically so it does not reshuffle.
func TestFetchKumaUptimeWithoutNames(t *testing.T) {
	srv := kumaServer(t, beatsFixture(t), "")
	rep, err := FetchKumaUptime(context.Background(), srv.URL+"/api/status-page/heartbeat/homelab", nil, 2*time.Second)
	if err != nil {
		t.Fatalf("FetchKumaUptime: %v", err)
	}
	if len(rep.Monitors) != 2 {
		t.Fatalf("want 2 monitors, got %d", len(rep.Monitors))
	}
	if rep.Monitors[0].ID != "3" || rep.Monitors[1].ID != "7" {
		t.Errorf("want numeric id order 3,7; got %q,%q", rep.Monitors[0].ID, rep.Monitors[1].ID)
	}
	if rep.Monitors[0].Name != "monitor 3" {
		t.Errorf("Name = %q, want the id fallback", rep.Monitors[0].Name)
	}
}

// Kuma answers with an empty skeleton rather than 404 when the slug has
// no public status page. That must read as an error, not as "all green".
func TestFetchKumaUptimeNoStatusPage(t *testing.T) {
	srv := kumaServer(t, `{"heartbeatList":{},"uptimeList":{}}`, "")
	rep, err := FetchKumaUptime(context.Background(), srv.URL+"/api/status-page/heartbeat/homelab", nil, 2*time.Second)
	if err == nil {
		t.Fatalf("want error for empty heartbeat list, got %+v", rep)
	}
	if !strings.Contains(err.Error(), "homelab") {
		t.Errorf("error should name the slug: %v", err)
	}
}

func TestKumaSplit(t *testing.T) {
	cases := []struct{ in, base, slug string }{
		{"https://kuma.example.com/api/status-page/heartbeat/homelab", "https://kuma.example.com", "homelab"},
		{"https://kuma.example.com/api/status-page/homelab", "https://kuma.example.com", "homelab"},
		{"https://kuma.example.com/status/homelab", "https://kuma.example.com", "homelab"},
		{"https://kuma.example.com/status/homelab/", "https://kuma.example.com", "homelab"},
		{"https://kuma.example.com", "https://kuma.example.com", "default"},
	}
	for _, c := range cases {
		base, slug := kumaSplit(c.in)
		if base != c.base || slug != c.slug {
			t.Errorf("kumaSplit(%q) = %q,%q; want %q,%q", c.in, base, slug, c.base, c.slug)
		}
	}
	want := "https://kuma.example.com/api/status-page/heartbeat/homelab"
	if got := kumaHeartbeatURL("https://kuma.example.com/status/homelab"); got != want {
		t.Errorf("kumaHeartbeatURL = %q, want %q", got, want)
	}
	wantCfg := "https://kuma.example.com/api/status-page/homelab"
	if got := kumaConfigURL("https://kuma.example.com/api/status-page/heartbeat/homelab"); got != wantCfg {
		t.Errorf("kumaConfigURL = %q, want %q", got, wantCfg)
	}
}

// Zoneless Kuma timestamps are UTC. Parsing them as local time shifts
// every beat by the UTC offset, which west of Greenwich puts them in the
// future — the renderer then drops them all and draws an empty bar next
// to a healthy uptime percentage. Regression test for exactly that.
func TestKumaTimeZonelessIsUTC(t *testing.T) {
	got, ok := kumaTime("2026-08-01 17:04:35.252")
	if !ok {
		t.Fatal("failed to parse a zoneless Kuma timestamp")
	}
	want := time.Date(2026, 8, 1, 17, 4, 35, 252000000, time.UTC)
	if !got.Equal(want) {
		t.Errorf("kumaTime = %v, want %v (UTC, not local)", got.UTC(), want)
	}
}

// A beat taken moments ago must land in the past no matter the host's
// timezone — the check the original bug failed.
func TestKumaTimeRecentBeatIsNotInTheFuture(t *testing.T) {
	now := time.Now()
	s := now.Add(-time.Minute).UTC().Format("2006-01-02 15:04:05.000")
	got, ok := kumaTime(s)
	if !ok {
		t.Fatalf("failed to parse %q", s)
	}
	if got.After(now) {
		t.Errorf("beat from a minute ago parsed as %v, which is after now (%v)", got, now)
	}
}

func TestKumaTime(t *testing.T) {
	for _, s := range []string{
		"2026-08-01 12:00:00.000",
		"2026-08-01 12:00:00",
		"2026-08-01T12:00:00Z",
		"2026-08-01T12:00:00.123Z",
	} {
		if _, ok := kumaTime(s); !ok {
			t.Errorf("kumaTime(%q) failed to parse", s)
		}
	}
	if _, ok := kumaTime("not a time"); ok {
		t.Error("kumaTime should reject garbage")
	}
	if _, ok := kumaTime(nil); ok {
		t.Error("kumaTime should reject a non-string")
	}
}
