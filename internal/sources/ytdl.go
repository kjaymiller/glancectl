package sources

import (
	"context"
	"time"
)

type YtdlChannelStats struct {
	Channels   int
	Files      int
	LastUnix   int64
}

// FetchYtdlChannels reads ytdl-sub-api /channels and rolls up totals.
func FetchYtdlChannels(ctx context.Context, url string, headers map[string]string, timeout time.Duration) (YtdlChannelStats, error) {
	out := YtdlChannelStats{}
	v, err := FetchJSON(ctx, url, headers, timeout)
	if err != nil {
		return out, err
	}
	root, _ := v.(map[string]any)
	rows, _ := root["channels"].([]any)
	out.Channels = len(rows)
	for _, r := range rows {
		obj, _ := r.(map[string]any)
		dl, _ := obj["downloads"].(map[string]any)
		out.Files += intOf(dl["file_count"])
		if m := int64(intOf(dl["latest_mtime"])); m > out.LastUnix {
			out.LastUnix = m
		}
	}
	return out, nil
}

type YtdlRun struct {
	When   time.Time
	Failed bool
}

// FetchYtdlRuns reads ytdl-sub-api /runs?limit=N.
func FetchYtdlRuns(ctx context.Context, url string, headers map[string]string, timeout time.Duration) ([]YtdlRun, error) {
	v, err := FetchJSON(ctx, url, headers, timeout)
	if err != nil {
		return nil, err
	}
	root, _ := v.(map[string]any)
	rows, _ := root["runs"].([]any)
	var out []YtdlRun
	for _, r := range rows {
		obj, _ := r.(map[string]any)
		exec, _ := obj["Execution"].(map[string]any)
		run := YtdlRun{}
		if d, ok := exec["Date"].(string); ok {
			if t, err := time.Parse(time.RFC3339, d); err == nil {
				run.When = t.Local()
			}
		}
		if f, ok := exec["Failed"].(bool); ok {
			run.Failed = f
		}
		out = append(out, run)
	}
	return out, nil
}
