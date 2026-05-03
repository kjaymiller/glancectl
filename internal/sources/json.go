package sources

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// FetchJSON does a GET with the given headers, parses the body as JSON,
// and returns it as an `any`. Used for Alertmanager / update-shim /
// ytdl-sub-api responses where we want to do our own structural reads.
func FetchJSON(ctx context.Context, url string, headers map[string]string, timeout time.Duration) (any, error) {
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(cctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("%s: %w", url, err)
	}
	return out, nil
}

// CountAlerts walks the Alertmanager /api/v2/alerts response shape:
// a JSON array of objects. Returns the total length.
func CountAlerts(v any) int {
	arr, ok := v.([]any)
	if !ok {
		return 0
	}
	return len(arr)
}

// CountActionableUpdates walks the update-shim /api/containers response
// and counts entries where updateAvailable=true.
func CountActionableUpdates(v any) int {
	return len(ActionableUpdates(v))
}

type ContainerUpdate struct {
	Name      string
	OldTag    string
	NewTag    string
	Tier      string
	IsMajor   bool
	Actionable bool
}

// ActionableUpdates returns the list of containers with updateAvailable=true,
// preserving the WUD/update-shim shape we render in the feed.
func ActionableUpdates(v any) []ContainerUpdate {
	arr, ok := v.([]any)
	if !ok {
		return nil
	}
	var out []ContainerUpdate
	for _, item := range arr {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if b, _ := obj["updateAvailable"].(bool); !b {
			continue
		}
		u := ContainerUpdate{}
		u.Name, _ = obj["name"].(string)
		if image, ok := obj["image"].(map[string]any); ok {
			if tag, ok := image["tag"].(map[string]any); ok {
				u.OldTag, _ = tag["value"].(string)
			}
		}
		if result, ok := obj["result"].(map[string]any); ok {
			u.NewTag, _ = result["tag"].(string)
		}
		u.Tier, _ = obj["homelab.tier"].(string)
		u.IsMajor, _ = obj["homelab.is_major_bump"].(bool)
		u.Actionable, _ = obj["homelab.actionable"].(bool)
		out = append(out, u)
	}
	return out
}
