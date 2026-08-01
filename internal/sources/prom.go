package sources

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// PromPoint is one sample from a query_range matrix.
type PromPoint struct {
	T time.Time
	V float64
}

// PromSeries is a single Prometheus metric over a time window, plus the
// display hints the widget carried. Kuma answers "was it up"; this answers
// everything else — load, memory, probe latency — so the card is built
// around a value with a shape rather than a status light.
type PromSeries struct {
	Label  string // display name, defaults to the raw query
	Unit   string // appended to the current value ("%", "s", …)
	Format string // "", "percent", "bytes", "duration"
	Range  time.Duration
	Points []PromPoint
}

// Current returns the most recent sample. Prometheus returns values in
// ascending time order, so that is the last one.
func (s PromSeries) Current() (float64, bool) {
	if len(s.Points) == 0 {
		return 0, false
	}
	return s.Points[len(s.Points)-1].V, true
}

// MinMax returns the range covered by the samples, used to scale the
// sparkline. Returns ok=false for an empty series.
func (s PromSeries) MinMax() (lo, hi float64, ok bool) {
	if len(s.Points) == 0 {
		return 0, 0, false
	}
	lo, hi = s.Points[0].V, s.Points[0].V
	for _, p := range s.Points[1:] {
		if p.V < lo {
			lo = p.V
		}
		if p.V > hi {
			hi = p.V
		}
	}
	return lo, hi, true
}

// promDefaultRange / promDefaultStep are used when the widget does not
// pin them. A day at 15m resolution is 96 points — plenty to downsample
// into a sparkline without asking Prometheus for more than we can draw.
const (
	promDefaultRange = 24 * time.Hour
	promDefaultStep  = 15 * time.Minute
)

// FetchPromRange runs a range query and returns the first series. The
// widget's `parameters` carry the config, which keeps the YAML valid for
// a real Glance instance reading the same file:
//
//	query  — PromQL (required)
//	range  — lookback window, Go duration (default 24h)
//	step   — sample interval, Go duration (default 15m)
//	label  — display name (default: the query itself)
//	unit   — suffix for the current value
//	format — percent | bytes | duration
//
// A query that matches several series is reduced to the first one
// Prometheus returns; use an aggregation in the query to control that.
func FetchPromRange(ctx context.Context, baseURL string, parameters map[string]string, timeout time.Duration) (PromSeries, error) {
	out := PromSeries{
		Label:  parameters["label"],
		Unit:   parameters["unit"],
		Format: parameters["format"],
	}
	query := parameters["query"]
	if query == "" {
		return out, fmt.Errorf("query missing from parameters")
	}
	if out.Label == "" {
		out.Label = query
	}

	lookback := promDuration(parameters["range"], promDefaultRange)
	step := promDuration(parameters["step"], promDefaultStep)
	out.Range = lookback

	end := time.Now()
	start := end.Add(-lookback)
	q := url.Values{}
	q.Set("query", query)
	q.Set("start", strconv.FormatInt(start.Unix(), 10))
	q.Set("end", strconv.FormatInt(end.Unix(), 10))
	q.Set("step", strconv.FormatInt(int64(step.Seconds()), 10))

	v, err := FetchJSON(ctx, promEndpoint(baseURL, "query_range")+"?"+q.Encode(), nil, timeout)
	if err != nil {
		return out, err
	}
	pts, err := parsePromMatrix(v)
	if err != nil {
		return out, err
	}
	out.Points = pts
	return out, nil
}

// parsePromMatrix reads the `data.result[0].values` of a query_range
// response. Prometheus encodes sample values as strings, and emits NaN
// and ±Inf as "NaN"/"+Inf" — those are dropped rather than plotted, so a
// gap in the data reads as a gap instead of a spike to the axis.
func parsePromMatrix(v any) ([]PromPoint, error) {
	root, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("prometheus: unexpected root")
	}
	if s, _ := root["status"].(string); s != "" && s != "success" {
		msg, _ := root["error"].(string)
		if msg == "" {
			msg = s
		}
		return nil, fmt.Errorf("prometheus: %s", msg)
	}
	data, _ := root["data"].(map[string]any)
	result, _ := data["result"].([]any)
	if len(result) == 0 {
		return nil, nil
	}
	first, _ := result[0].(map[string]any)
	values, _ := first["values"].([]any)
	var out []PromPoint
	for _, raw := range values {
		pair, _ := raw.([]any)
		if len(pair) != 2 {
			continue
		}
		ts := floatOf(pair[0])
		s, _ := pair[1].(string)
		f, err := strconv.ParseFloat(s, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			continue
		}
		out = append(out, PromPoint{T: time.Unix(int64(ts), 0), V: f})
	}
	return out, nil
}

// promEndpoint normalises whatever the widget's url points at into the
// wanted API path. Glance configs tend to name a concrete endpoint
// (…/api/v1/query) because that is what the template fetches, so accept
// either that or a bare host.
func promEndpoint(base, path string) string {
	base = strings.TrimRight(base, "/")
	for _, suffix := range []string{"/api/v1/query_range", "/api/v1/query"} {
		if strings.HasSuffix(base, suffix) {
			base = strings.TrimSuffix(base, suffix)
			break
		}
	}
	return base + "/api/v1/" + path
}

func promDuration(s string, def time.Duration) time.Duration {
	if s == "" {
		return def
	}
	d, err := time.ParseDuration(s)
	if err != nil || d <= 0 {
		return def
	}
	return d
}
