package sources

import (
	"context"
	"fmt"
	"time"
)

// ServiceStatus is one row from the service-status shim. The three
// booleans map to the three status lights Glance's template renders:
// running, backup fresh, and up to date (the inverse of UpdateAvailable).
type ServiceStatus struct {
	Name            string
	Running         bool
	BackupFresh     bool
	BackupAgeHours  float64
	UpdateAvailable bool
	Current         string
	Latest          string
}

// OK reports whether all three lights pass — the "everything green" case
// the feed renders without any detail suffix.
func (s ServiceStatus) OK() bool {
	return s.Running && s.BackupFresh && !s.UpdateAvailable
}

// ServiceStatusReport wraps the service list with the SLA the shim used,
// so the renderer can show "36h > 24h" without a second source of truth.
type ServiceStatusReport struct {
	BackupSLAHours float64
	Services       []ServiceStatus
}

// FetchServiceStatus reads the service-status shim's services.json. The
// payload is hand-rolled rather than a standard API, so every field is
// read defensively: a missing or retyped key degrades to the zero value
// instead of failing the whole card.
func FetchServiceStatus(ctx context.Context, url string, headers map[string]string, timeout time.Duration) (ServiceStatusReport, error) {
	out := ServiceStatusReport{}
	v, err := FetchJSON(ctx, url, headers, timeout)
	if err != nil {
		return out, err
	}
	root, ok := v.(map[string]any)
	if !ok {
		return out, fmt.Errorf("%s: unexpected root", url)
	}
	out.BackupSLAHours = floatOf(root["backup_sla_hours"])
	rows, _ := root["services"].([]any)
	for _, r := range rows {
		obj, ok := r.(map[string]any)
		if !ok {
			continue
		}
		s := ServiceStatus{}
		s.Name, _ = obj["name"].(string)
		s.Running, _ = obj["running"].(bool)
		if b, ok := obj["backedup"].(map[string]any); ok {
			s.BackupFresh, _ = b["fresh"].(bool)
			s.BackupAgeHours = floatOf(b["age_hours"])
		}
		if u, ok := obj["updates"].(map[string]any); ok {
			s.UpdateAvailable, _ = u["available"].(bool)
			s.Current, _ = u["current"].(string)
			s.Latest, _ = u["latest"].(string)
		}
		out.Services = append(out.Services, s)
	}
	return out, nil
}

func floatOf(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	}
	return 0
}
