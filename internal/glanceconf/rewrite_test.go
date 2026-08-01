package glanceconf

import "testing"

func TestParseRewrites(t *testing.T) {
	r, err := ParseRewrites("update-shim:5000=https://update.example.dev,\n alertmanager = alerts.example.dev \n")
	if err != nil {
		t.Fatal(err)
	}
	if got := r["update-shim:5000"]; got != "https://update.example.dev" {
		t.Errorf("update-shim rule = %q", got)
	}
	if got := r["alertmanager"]; got != "alerts.example.dev" {
		t.Errorf("alertmanager rule = %q", got)
	}
}

// A rule copy-pasted straight off a widget's url: line should still work.
func TestParseRewritesStripsScheme(t *testing.T) {
	r, err := ParseRewrites("http://update-shim:5000=https://update.example.dev")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := r["update-shim:5000"]; !ok {
		t.Errorf("scheme not stripped from key: %v", r)
	}
}

func TestParseRewritesRejectsMalformed(t *testing.T) {
	for _, spec := range []string{"no-equals-sign", "=missing-key", "missing-value="} {
		if _, err := ParseRewrites(spec); err == nil {
			t.Errorf("ParseRewrites(%q) should have failed", spec)
		}
	}
	if r, err := ParseRewrites(""); err != nil || len(r) != 0 {
		t.Errorf("empty spec should yield empty ruleset, got %v %v", r, err)
	}
}

func TestRewriterApply(t *testing.T) {
	r, err := ParseRewrites(
		"update-shim:5000=https://update.example.dev," +
			"alertmanager=https://alerts.example.dev," +
			"svc:5000=other:6000," +
			"prefixed:80=https://gw.example.dev/api")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ in, want string }{
		// Scheme and authority both replaced; path and query preserved.
		{"http://update-shim:5000/api/containers", "https://update.example.dev/api/containers"},
		{"http://alertmanager:9093/api/v2/alerts?active=true", "https://alerts.example.dev/api/v2/alerts?active=true"},
		// Bare authority keeps the original scheme.
		{"http://svc:5000/x", "http://other:6000/x"},
		// A path on the replacement is prefixed.
		{"http://prefixed:80/services.json", "https://gw.example.dev/api/services.json"},
		// No rule, unparseable, and empty all pass through untouched.
		{"https://grafana.example.dev/api/health", "https://grafana.example.dev/api/health"},
		{"://nonsense", "://nonsense"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := r.Apply(tc.in); got != tc.want {
			t.Errorf("Apply(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// A host-only rule must not fire when a port-specific rule exists for the
// same host, otherwise the more specific intent is silently lost.
func TestRewriterPrefersPortSpecificRule(t *testing.T) {
	r, err := ParseRewrites("host:5000=https://specific.example.dev,host=https://general.example.dev")
	if err != nil {
		t.Fatal(err)
	}
	if got := r.Apply("http://host:5000/x"); got != "https://specific.example.dev/x" {
		t.Errorf("port-specific rule not preferred: %q", got)
	}
	if got := r.Apply("http://host:9999/x"); got != "https://general.example.dev/x" {
		t.Errorf("host-only rule did not catch other port: %q", got)
	}
}

func TestApplyRewritesCoversConfig(t *testing.T) {
	cfg := &Config{Pages: []Page{{Columns: []Column{{Widgets: []Widget{
		{Type: "custom-api", URL: "http://update-shim:5000/api/containers"},
		{Type: "monitor", Sites: []Site{{URL: "http://update-shim:5000/healthz"}}},
		{Type: "bookmarks", Groups: []BookmarkGroup{{Links: []Site{{URL: "http://update-shim:5000/ui"}}}}},
	}}}}}}
	r, err := ParseRewrites("update-shim:5000=https://update.example.dev")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ApplyRewrites(r)

	w := cfg.Pages[0].Columns[0].Widgets
	if got := w[0].URL; got != "https://update.example.dev/api/containers" {
		t.Errorf("widget URL = %q", got)
	}
	if got := w[1].Sites[0].URL; got != "https://update.example.dev/healthz" {
		t.Errorf("site URL = %q", got)
	}
	if got := w[2].Groups[0].Links[0].URL; got != "https://update.example.dev/ui" {
		t.Errorf("bookmark URL = %q", got)
	}
}

func TestApplyRewritesNoRulesIsNoop(t *testing.T) {
	cfg := &Config{Pages: []Page{{Columns: []Column{{Widgets: []Widget{
		{URL: "http://update-shim:5000/api"},
	}}}}}}
	cfg.ApplyRewrites(nil)
	if got := cfg.Pages[0].Columns[0].Widgets[0].URL; got != "http://update-shim:5000/api" {
		t.Errorf("URL changed with no rules: %q", got)
	}
}
