package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/kjaymiller/glancectl/internal/sources"
)

// plain strips ANSI styling so assertions can talk about glyphs. lipgloss
// suppresses colour without a TTY anyway, so bucket *ranks* are asserted
// against uptimeBuckets and only glyph/width against the rendered bar.
func plain(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}

func countRank(buckets []int, rank int) int {
	n := 0
	for _, b := range buckets {
		if b == rank {
			n++
		}
	}
	return n
}

func TestUptimeBarBucketsAndWidth(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	window := 24 * time.Hour

	// One up beat per bucket.
	var beats []sources.KumaBeat
	per := window / uptimeCols
	for i := 0; i < uptimeCols; i++ {
		at := now.Add(-window).Add(time.Duration(i)*per + per/2)
		beats = append(beats, sources.KumaBeat{At: at, Status: sources.KumaUp})
	}

	buckets := uptimeBuckets(beats, window, now)
	if got := countRank(buckets, bucketUp); got != uptimeCols {
		t.Errorf("want every bucket up, got %d of %d (%v)", got, uptimeCols, buckets)
	}

	bar := plain(uptimeBar(beats, window, now))
	if n := len([]rune(bar)); n != uptimeCols {
		t.Fatalf("bar width = %d runes, want %d (%q)", n, uptimeCols, bar)
	}
	if bar != strings.Repeat("█", uptimeCols) {
		t.Errorf("all buckets should be filled blocks: %q", bar)
	}
}

// Up and down must differ by glyph, not only by colour: the bar has to
// stay readable on a mono terminal and to a colourblind reader.
func TestUptimeBarDistinguishesWithoutColour(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	window := 24 * time.Hour
	per := window / uptimeCols

	glyph := func(status int) rune {
		bar := plain(uptimeBar([]sources.KumaBeat{
			{At: now.Add(-per / 2), Status: status},
		}, window, now))
		return []rune(bar)[uptimeCols-1]
	}

	seen := map[rune]int{}
	for _, status := range []int{sources.KumaUp, sources.KumaDown, sources.KumaPending, sources.KumaMaintenance} {
		g := glyph(status)
		if prev, dup := seen[g]; dup {
			t.Errorf("status %d and %d share glyph %q", prev, status, g)
		}
		seen[g] = status
	}
	if g := glyph(sources.KumaUp); g != '█' {
		t.Errorf("up glyph = %q, want a full block", g)
	}
	if g := glyph(sources.KumaDown); g == '█' {
		t.Errorf("down glyph = %q, must not be a full block", g)
	}
}

// A bucket holding both an up and a down beat must rank as down: the
// graph exists to surface outages, not to average them away.
func TestUptimeBarWorstWins(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	window := 24 * time.Hour
	per := window / uptimeCols
	at := now.Add(-per / 2) // both land in the final bucket

	buckets := uptimeBuckets([]sources.KumaBeat{
		{At: at, Status: sources.KumaUp},
		{At: at.Add(time.Second), Status: sources.KumaDown},
	}, window, now)

	if got := buckets[uptimeCols-1]; got != bucketDown {
		t.Errorf("mixed bucket rank = %d, want bucketDown (%d)", got, bucketDown)
	}
	// Order the ranks so that a future status can't silently outrank down.
	if bucketDown <= bucketUp || bucketDown <= bucketMaint || bucketDown <= bucketPending {
		t.Error("bucketDown must outrank every other status")
	}
}

// Maintenance is not an outage, and must not rank as one.
func TestUptimeBarMaintenanceIsNotDown(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	window := 24 * time.Hour
	buckets := uptimeBuckets([]sources.KumaBeat{
		{At: now.Add(-time.Minute), Status: sources.KumaMaintenance},
	}, window, now)
	if got := buckets[uptimeCols-1]; got != bucketMaint {
		t.Errorf("maintenance rank = %d, want bucketMaint (%d)", got, bucketMaint)
	}
}

// Buckets with no heartbeat are gaps, not implied uptime.
func TestUptimeBarGapsAreNotUp(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	window := 24 * time.Hour
	beats := []sources.KumaBeat{{At: now.Add(-time.Minute), Status: sources.KumaUp}}

	buckets := uptimeBuckets(beats, window, now)
	if got := countRank(buckets, bucketNone); got != uptimeCols-1 {
		t.Errorf("want %d empty buckets for a monitor with one beat, got %d", uptimeCols-1, got)
	}

	bar := plain(uptimeBar(beats, window, now))
	if n := len([]rune(bar)); n != uptimeCols {
		t.Fatalf("bar width = %d, want %d", n, uptimeCols)
	}
	if dots := strings.Count(bar, "·"); dots != uptimeCols-1 {
		t.Errorf("want %d gap dots, got %d (%q)", uptimeCols-1, dots, bar)
	}
}

// Beats outside the window are ignored rather than clamped into an edge
// bucket, which would invent an outage (or an uptime) that never was.
func TestUptimeBarIgnoresOutOfWindow(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	window := 24 * time.Hour
	beats := []sources.KumaBeat{
		{At: now.Add(-48 * time.Hour), Status: sources.KumaDown},
		{At: now.Add(time.Hour), Status: sources.KumaDown},
	}
	if got := countRank(uptimeBuckets(beats, window, now), bucketNone); got != uptimeCols {
		t.Errorf("out-of-window beats should leave every bucket empty, got %d filled", uptimeCols-got)
	}
	if got := plain(uptimeBar(beats, window, now)); got != strings.Repeat("·", uptimeCols) {
		t.Errorf("bar = %q, want all gaps", got)
	}
}

func TestSparklineWidthAndShape(t *testing.T) {
	var pts []sources.PromPoint
	for i := 0; i < 8; i++ {
		pts = append(pts, sources.PromPoint{V: float64(i)})
	}
	got := plain(sparkline(pts, 0, 7))
	if got != "▁▂▃▄▅▆▇█" {
		t.Errorf("ramp = %q, want the full block ramp", got)
	}
}

// A flat series must not divide by a zero range.
func TestSparklineFlatSeries(t *testing.T) {
	pts := []sources.PromPoint{{V: 5}, {V: 5}, {V: 5}}
	got := plain(sparkline(pts, 5, 5))
	if got != "▁▁▁" {
		t.Errorf("flat series = %q, want the bottom rung", got)
	}
	if sparkline(nil, 0, 1) != "" {
		t.Error("empty series should render empty")
	}
}

func TestSparklineDownsamples(t *testing.T) {
	var pts []sources.PromPoint
	for i := 0; i < 500; i++ {
		pts = append(pts, sources.PromPoint{V: float64(i)})
	}
	got := plain(sparkline(pts, 0, 499))
	if n := len([]rune(got)); n != sparkCols {
		t.Errorf("width = %d, want %d", n, sparkCols)
	}
}

func TestDownsampleKeepsShortSeries(t *testing.T) {
	pts := []sources.PromPoint{{V: 1}, {V: 2}}
	if got := downsample(pts, 40); len(got) != 2 {
		t.Errorf("short series should pass through, got %d", len(got))
	}
}

func TestFormatValue(t *testing.T) {
	cases := []struct {
		v      float64
		format string
		unit   string
		want   string
	}{
		{97.5, "percent", "", "97.5%"},
		{16753221632, "bytes", "", "15.6 GiB"},
		{512, "bytes", "", "512 B"},
		{0.049417489, "duration", "", "49ms"},
		{2.5, "duration", "", "2.50s"},
		{125, "duration", "", "2m5s"},
		{1.26, "", "", "1.26"},
		{45.67, "", "%", "45.7%"},
		{813731459072, "", "", "813731459072"},
	}
	for _, c := range cases {
		if got := formatValue(c.v, c.format, c.unit); got != c.want {
			t.Errorf("formatValue(%v, %q, %q) = %q, want %q", c.v, c.format, c.unit, got, c.want)
		}
	}
}

func TestShortDur(t *testing.T) {
	cases := map[time.Duration]string{
		24 * time.Hour:   "24h",
		48 * time.Hour:   "2d",
		6 * time.Hour:    "6h",
		90 * time.Minute: "1h",
		30 * time.Minute: "30m",
		45 * time.Second: "45s",
	}
	for in, want := range cases {
		if got := shortDur(in); got != want {
			t.Errorf("shortDur(%v) = %q, want %q", in, got, want)
		}
	}
}
