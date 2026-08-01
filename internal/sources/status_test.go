package sources

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func serveJSON(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const statusFixture = `{
  "backup_sla_hours": 24,
  "services": [
    {
      "name": "grafana",
      "running": true,
      "backedup": {"fresh": true, "age_hours": 3.5},
      "updates": {"available": false, "current": "10.1.0", "latest": "10.1.0"}
    },
    {
      "name": "loki",
      "running": false,
      "backedup": {"fresh": false, "age_hours": 36},
      "updates": {"available": true, "current": "10.1.0", "latest": "10.2.0"}
    }
  ]
}`

func TestFetchServiceStatus(t *testing.T) {
	srv := serveJSON(t, statusFixture)
	rep, err := FetchServiceStatus(context.Background(), srv.URL, nil, 2*time.Second)
	if err != nil {
		t.Fatalf("FetchServiceStatus: %v", err)
	}
	if rep.BackupSLAHours != 24 {
		t.Errorf("BackupSLAHours = %v, want 24", rep.BackupSLAHours)
	}
	if len(rep.Services) != 2 {
		t.Fatalf("want 2 services, got %d", len(rep.Services))
	}

	ok := rep.Services[0]
	want := ServiceStatus{
		Name: "grafana", Running: true, BackupFresh: true, BackupAgeHours: 3.5,
		UpdateAvailable: false, Current: "10.1.0", Latest: "10.1.0",
	}
	if ok != want {
		t.Errorf("all-green service = %+v, want %+v", ok, want)
	}
	if !ok.OK() {
		t.Error("grafana should report OK")
	}

	bad := rep.Services[1]
	wantBad := ServiceStatus{
		Name: "loki", Running: false, BackupFresh: false, BackupAgeHours: 36,
		UpdateAvailable: true, Current: "10.1.0", Latest: "10.2.0",
	}
	if bad != wantBad {
		t.Errorf("all-failing service = %+v, want %+v", bad, wantBad)
	}
	if bad.OK() {
		t.Error("loki should not report OK")
	}
}

// Missing/retyped keys should degrade to zero values, not panic.
func TestFetchServiceStatusPartial(t *testing.T) {
	srv := serveJSON(t, `{"services": [{"name": "bare"}, "not-an-object", {}]}`)
	rep, err := FetchServiceStatus(context.Background(), srv.URL, nil, 2*time.Second)
	if err != nil {
		t.Fatalf("FetchServiceStatus: %v", err)
	}
	if len(rep.Services) != 2 {
		t.Fatalf("want 2 object services, got %d", len(rep.Services))
	}
	if rep.Services[0].Name != "bare" || rep.Services[0].Running || rep.Services[0].BackupAgeHours != 0 {
		t.Errorf("partial service = %+v", rep.Services[0])
	}
}

func TestFetchServiceStatusMalformed(t *testing.T) {
	cases := map[string]string{
		"empty":      ``,
		"not-json":   `<html>nope</html>`,
		"wrong-root": `[1, 2, 3]`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			srv := serveJSON(t, body)
			rep, err := FetchServiceStatus(context.Background(), srv.URL, nil, 2*time.Second)
			if err == nil {
				t.Fatalf("want error for %s payload, got %+v", name, rep)
			}
			if len(rep.Services) != 0 {
				t.Errorf("want empty result on error, got %+v", rep.Services)
			}
		})
	}
}

func TestFetchServiceStatusHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := FetchServiceStatus(context.Background(), srv.URL, nil, 2*time.Second); err == nil {
		t.Fatal("want error on HTTP 500")
	}
}
