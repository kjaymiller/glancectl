package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

const promMatrixFixture = `{
  "status": "success",
  "data": {
    "resultType": "matrix",
    "result": [
      {
        "metric": {"__name__": "node_load1", "instance": "node-exporter:9100"},
        "values": [[1785590165, "0.28"], [1785591065, "0.24"], [1785592865, "0.89"], [1785593765, "1.46"]]
      }
    ]
  }
}`

func TestFetchPromRange(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		if r.URL.Path != "/api/v1/query_range" {
			t.Errorf("path = %q, want /api/v1/query_range", r.URL.Path)
		}
		_, _ = w.Write([]byte(promMatrixFixture))
	}))
	defer srv.Close()

	s, err := FetchPromRange(context.Background(), srv.URL, map[string]string{
		"query": "node_load1",
		"range": "6h",
		"step":  "5m",
		"label": "Load",
		"unit":  "",
	}, 2*time.Second)
	if err != nil {
		t.Fatalf("FetchPromRange: %v", err)
	}

	if got.Get("query") != "node_load1" {
		t.Errorf("query param = %q", got.Get("query"))
	}
	if got.Get("step") != "300" {
		t.Errorf("step param = %q, want 300", got.Get("step"))
	}
	if s.Range != 6*time.Hour {
		t.Errorf("Range = %v, want 6h", s.Range)
	}
	if s.Label != "Load" {
		t.Errorf("Label = %q, want Load", s.Label)
	}
	if len(s.Points) != 4 {
		t.Fatalf("want 4 points, got %d", len(s.Points))
	}
	if cur, ok := s.Current(); !ok || cur != 1.46 {
		t.Errorf("Current = %v (ok=%v), want 1.46", cur, ok)
	}
	lo, hi, ok := s.MinMax()
	if !ok || lo != 0.24 || hi != 1.46 {
		t.Errorf("MinMax = %v/%v, want 0.24/1.46", lo, hi)
	}
}

// Defaults apply when range/step/label are absent, and the label falls
// back to the query so the card is never nameless.
func TestFetchPromRangeDefaults(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.URL.Query()
		_, _ = w.Write([]byte(promMatrixFixture))
	}))
	defer srv.Close()

	s, err := FetchPromRange(context.Background(), srv.URL, map[string]string{"query": "up"}, 2*time.Second)
	if err != nil {
		t.Fatalf("FetchPromRange: %v", err)
	}
	if s.Range != promDefaultRange {
		t.Errorf("Range = %v, want %v", s.Range, promDefaultRange)
	}
	if got.Get("step") != "900" {
		t.Errorf("step = %q, want 900", got.Get("step"))
	}
	if s.Label != "up" {
		t.Errorf("Label = %q, want the query", s.Label)
	}
}

func TestFetchPromRangeNeedsQuery(t *testing.T) {
	if _, err := FetchPromRange(context.Background(), "http://x", nil, time.Second); err == nil {
		t.Fatal("want error when query parameter is missing")
	}
}

// Prometheus reports query errors in a 400 body; the HTTP status is what
// FetchJSON surfaces, but a 200 with status:error must also fail.
func TestFetchPromRangeAPIError(t *testing.T) {
	srv := serveJSON(t, `{"status":"error","error":"parse error: unexpected end of input"}`)
	_, err := FetchPromRange(context.Background(), srv.URL, map[string]string{"query": "up("}, 2*time.Second)
	if err == nil {
		t.Fatal("want error for status:error response")
	}
}

// NaN / Inf are dropped rather than plotted at the axis.
func TestParsePromMatrixSkipsNonFinite(t *testing.T) {
	srv := serveJSON(t, `{"status":"success","data":{"result":[{"values":[
		[1,"1.5"],[2,"NaN"],[3,"+Inf"],[4,"2.5"],[5,"garbage"],[6,"bad"]
	]}]}}`)
	s, err := FetchPromRange(context.Background(), srv.URL, map[string]string{"query": "up"}, 2*time.Second)
	if err != nil {
		t.Fatalf("FetchPromRange: %v", err)
	}
	if len(s.Points) != 2 {
		t.Fatalf("want 2 finite points, got %d (%+v)", len(s.Points), s.Points)
	}
	if s.Points[0].V != 1.5 || s.Points[1].V != 2.5 {
		t.Errorf("points = %+v", s.Points)
	}
}

// An empty result set is not an error — the query is valid, nothing
// matched — so the card can say "no data" instead of showing a failure.
func TestFetchPromRangeEmptyResult(t *testing.T) {
	srv := serveJSON(t, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
	s, err := FetchPromRange(context.Background(), srv.URL, map[string]string{"query": "nope"}, 2*time.Second)
	if err != nil {
		t.Fatalf("FetchPromRange: %v", err)
	}
	if len(s.Points) != 0 {
		t.Errorf("want no points, got %+v", s.Points)
	}
	if _, ok := s.Current(); ok {
		t.Error("Current should report !ok for an empty series")
	}
}

func TestPromEndpoint(t *testing.T) {
	cases := map[string]string{
		"https://p.example.com":                    "https://p.example.com/api/v1/query_range",
		"https://p.example.com/":                   "https://p.example.com/api/v1/query_range",
		"https://p.example.com/api/v1/query":       "https://p.example.com/api/v1/query_range",
		"https://p.example.com/api/v1/query_range": "https://p.example.com/api/v1/query_range",
	}
	for in, want := range cases {
		if got := promEndpoint(in, "query_range"); got != want {
			t.Errorf("promEndpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPromDuration(t *testing.T) {
	if got := promDuration("", time.Hour); got != time.Hour {
		t.Errorf("empty = %v, want the default", got)
	}
	if got := promDuration("nonsense", time.Hour); got != time.Hour {
		t.Errorf("unparseable = %v, want the default", got)
	}
	if got := promDuration("-5m", time.Hour); got != time.Hour {
		t.Errorf("negative = %v, want the default", got)
	}
	if got := promDuration("90m", time.Hour); got != 90*time.Minute {
		t.Errorf("90m = %v", got)
	}
}
