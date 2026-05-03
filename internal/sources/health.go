// Package sources provides concurrent fetchers for the data behind each
// glancectl pane: HTTP health checks, Alertmanager, update-shim, etc.
package sources

import (
	"context"
	"net/http"
	"sync"
	"time"
)

type HealthResult struct {
	Title  string
	URL    string
	Status int   // 0 = error
	Err    error
	RT     time.Duration
}

// CheckAll probes every URL concurrently with a per-request timeout.
// Returns results in the same order as the input.
func CheckAll(ctx context.Context, sites []Site, timeout time.Duration) []HealthResult {
	out := make([]HealthResult, len(sites))
	var wg sync.WaitGroup
	for i, s := range sites {
		wg.Add(1)
		go func(i int, s Site) {
			defer wg.Done()
			out[i] = check(ctx, s, timeout)
		}(i, s)
	}
	wg.Wait()
	return out
}

type Site struct {
	Title string
	URL   string
}

func check(ctx context.Context, s Site, timeout time.Duration) HealthResult {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, s.URL, nil)
	if err != nil {
		return HealthResult{Title: s.Title, URL: s.URL, Err: err}
	}
	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	rt := time.Since(start)
	if err != nil {
		return HealthResult{Title: s.Title, URL: s.URL, Err: err, RT: rt}
	}
	defer resp.Body.Close()
	return HealthResult{Title: s.Title, URL: s.URL, Status: resp.StatusCode, RT: rt}
}
