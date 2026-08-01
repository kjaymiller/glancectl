package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/kjaymiller/glancectl/internal/sources"
)

// uptimeCols is the width of the binary uptime bar. 32 buckets over 24h
// is 45 minutes per column — coarse enough to fit beside a name and an
// uptime figure in the feed pane, fine enough that a single failed check
// still colours a column.
const uptimeCols = 32

// sparkCols caps the width of a Prometheus sparkline. Series are
// downsampled to this; shorter series draw at their own length.
const sparkCols = 40

// sparkRunes is the eight-level block ramp used for value sparklines.
var sparkRunes = []rune("▁▂▃▄▅▆▇█")

// Bucket ranks, ordered by how much they should dominate a bucket that
// holds more than one kind of beat. Higher wins.
const (
	bucketNone = iota
	bucketUp
	bucketMaint
	bucketPending
	bucketDown
)

// uptimeBuckets reduces beats to one rank per time bucket across window,
// ending at now. Each bucket takes the worst status that fell in it, so a
// single failure inside a 45-minute bucket is not averaged away — the
// point of the graph is to make outages visible, not to be fair to them.
// Buckets with no heartbeat stay bucketNone rather than counting as up,
// which keeps a monitor that only started reporting an hour ago from
// claiming a full green day. Beats outside the window are dropped, not
// clamped into an edge bucket.
func uptimeBuckets(beats []sources.KumaBeat, window time.Duration, now time.Time) []int {
	out := make([]int, uptimeCols)
	if window <= 0 {
		return out
	}
	start := now.Add(-window)
	per := window / uptimeCols
	for _, b := range beats {
		if b.At.Before(start) || b.At.After(now) {
			continue
		}
		i := int(b.At.Sub(start) / per)
		if i < 0 || i >= uptimeCols {
			continue
		}
		var rank int
		switch b.Status {
		case sources.KumaUp:
			rank = bucketUp
		case sources.KumaMaintenance:
			rank = bucketMaint
		case sources.KumaPending:
			rank = bucketPending
		default:
			rank = bucketDown
		}
		if rank > out[i] {
			out[i] = rank
		}
	}
	return out
}

// uptimeBar renders the bucket ranks as a binary bar. Height carries the
// signal and colour only reinforces it: a full block is up, a low block
// is an outage. Relying on red-vs-green alone would make the graph
// unreadable on a mono terminal, in a pipe, or to a colourblind reader —
// and "was it down" is exactly the thing this card exists to answer.
func uptimeBar(beats []sources.KumaBeat, window time.Duration, now time.Time) string {
	var sb strings.Builder
	for _, b := range uptimeBuckets(beats, window, now) {
		switch b {
		case bucketUp:
			sb.WriteString(good.Render("█"))
		case bucketMaint:
			sb.WriteString(accent.Render("▒"))
		case bucketPending:
			sb.WriteString(warn.Render("▄"))
		case bucketDown:
			sb.WriteString(bad.Render("▁"))
		default:
			sb.WriteString(subtle.Render("·"))
		}
	}
	return sb.String()
}

// sparkline renders values as a block-ramp graph. A flat series draws on
// the bottom rung rather than dividing by a zero range.
func sparkline(points []sources.PromPoint, lo, hi float64) string {
	if len(points) == 0 {
		return ""
	}
	points = downsample(points, sparkCols)
	span := hi - lo
	var sb strings.Builder
	for _, p := range points {
		idx := 0
		if span > 0 {
			idx = int((p.V - lo) / span * float64(len(sparkRunes)-1))
		}
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkRunes) {
			idx = len(sparkRunes) - 1
		}
		sb.WriteRune(sparkRunes[idx])
	}
	return accent.Render(sb.String())
}

// downsample reduces points to at most n by averaging each bucket. Mean,
// not max: unlike the uptime bar this is a magnitude graph, where a
// single scrape spike should not redefine the whole column.
func downsample(points []sources.PromPoint, n int) []sources.PromPoint {
	if n <= 0 || len(points) <= n {
		return points
	}
	out := make([]sources.PromPoint, 0, n)
	for i := 0; i < n; i++ {
		start := i * len(points) / n
		end := (i + 1) * len(points) / n
		if end <= start {
			end = start + 1
		}
		if end > len(points) {
			end = len(points)
		}
		sum := 0.0
		for _, p := range points[start:end] {
			sum += p.V
		}
		out = append(out, sources.PromPoint{
			T: points[end-1].T,
			V: sum / float64(end-start),
		})
	}
	return out
}

// formatValue renders a metric for display. The format hint comes from
// the widget; without one the value is printed at a sensible precision
// for its magnitude, so both 0.049 (probe seconds) and 16753221632 stay
// readable without per-metric configuration.
func formatValue(v float64, format, unit string) string {
	switch format {
	case "percent":
		return fmt.Sprintf("%.1f%%", v)
	case "bytes":
		return humanBytes(v)
	case "duration":
		return humanDuration(v)
	}
	var s string
	switch av := abs(v); {
	case av >= 1000:
		s = fmt.Sprintf("%.0f", v)
	case av >= 10:
		s = fmt.Sprintf("%.1f", v)
	default:
		s = fmt.Sprintf("%.3g", v)
	}
	return s + unit
}

// humanBytes renders a byte count in binary units.
func humanBytes(v float64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	i := 0
	for abs(v) >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", v, units[i])
	}
	return fmt.Sprintf("%.1f %s", v, units[i])
}

// humanDuration renders a duration given in seconds.
func humanDuration(v float64) string {
	switch av := abs(v); {
	case av < 1:
		return fmt.Sprintf("%.0fms", v*1000)
	case av < 60:
		return fmt.Sprintf("%.2fs", v)
	default:
		return time.Duration(v * float64(time.Second)).Round(time.Second).String()
	}
}

func abs(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// shortDur renders a lookback window the way the card labels it ("24h").
func shortDur(d time.Duration) string {
	switch {
	// A day is still labelled "24h" — only past that does "2d" read better.
	case d > 24*time.Hour && d%(24*time.Hour) == 0:
		return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
	case d >= time.Hour:
		return fmt.Sprintf("%dh", int(d/time.Hour))
	case d >= time.Minute:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	default:
		return d.String()
	}
}
