package glanceconf

import (
	"os"
	"path/filepath"
	"testing"
)

// TestLoadHomelab loads the real homelab glance config when present;
// it's a smoke test that the parser handles $include + ${VAR} +
// split-column without panicking, and that we extract the expected
// widget shapes.
func TestLoadHomelab(t *testing.T) {
	cfg := os.Getenv("GLANCECTL_TEST_CONFIG")
	if cfg == "" {
		// Default to the sibling homelab repo if it exists.
		home, _ := os.UserHomeDir()
		cfg = filepath.Join(home, "homelab", "configs", "glance", "glance.yml")
	}
	if _, err := os.Stat(cfg); err != nil {
		t.Skip("no glance.yml available; set GLANCECTL_TEST_CONFIG to enable")
	}

	envFile := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(cfg))), "compose", "glance", ".env")
	c, err := Load(cfg, envFile)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(c.Pages) == 0 {
		t.Fatal("no pages parsed")
	}
	if len(c.Sites()) == 0 {
		t.Error("expected at least one monitor site")
	}
	if len(c.Bookmarks()) == 0 {
		t.Error("expected at least one bookmark group")
	}
	if w := c.FindCustomAPI("alert"); w == nil || w.URL == "" {
		t.Error("expected an alerts custom-api widget")
	}
	if w := c.FindCustomAPI("update"); w == nil || w.URL == "" {
		t.Error("expected an updates custom-api widget")
	}

	mid := c.MiddleWidgets()
	if len(mid) == 0 {
		t.Fatal("expected widgets in the wide column")
	}
	wantTypes := map[string]bool{"custom-api": false, "weather": false}
	for _, w := range mid {
		if _, ok := wantTypes[w.Type]; ok {
			wantTypes[w.Type] = true
		}
	}
	for typ, seen := range wantTypes {
		if !seen {
			t.Errorf("middle column missing widget type %q", typ)
		}
	}

	// Spot-check braves widget has parameters captured.
	for _, w := range mid {
		if w.Type == "custom-api" && w.Title != "" && contains(lower(w.Title), "brave") {
			if w.Parameters["teamId"] == "" {
				t.Errorf("braves widget missing teamId param; got %v", w.Parameters)
			}
		}
	}
}

// A config reached through a symlink must resolve $include relative to
// the symlink target's directory, not the link's.
func TestLoadIncludesThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.MkdirAll(filepath.Join(real, "widgets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "widgets", "w.yml"),
		[]byte("- type: bookmarks\n  groups:\n    - title: G\n      links:\n        - title: L\n          url: http://x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "glance.yml"),
		[]byte("pages:\n  - name: P\n    columns:\n      - size: full\n        widgets:\n          - $include: widgets/w.yml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "glance.yml")
	if err := os.Symlink(filepath.Join(real, "glance.yml"), link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	cfg, err := Load(link, "")
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Bookmarks(); len(got) != 1 || got[0].Title != "G" {
		t.Errorf("include not resolved through symlink: %+v", got)
	}
}

// writeConfig writes yml to a temp dir and loads it.
func writeConfig(t *testing.T, yml string) *Config {
	t.Helper()
	path := filepath.Join(t.TempDir(), "glance.yml")
	if err := os.WriteFile(path, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path, "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return cfg
}

func widgetTitles(ws []Widget) []string {
	var out []string
	for _, w := range ws {
		out = append(out, w.Title)
	}
	return out
}

func sameTitles(got []Widget, want []string) bool {
	titles := widgetTitles(got)
	if len(titles) != len(want) {
		return false
	}
	for i := range want {
		if titles[i] != want[i] {
			return false
		}
	}
	return true
}

// Widgets in `size: small` columns must still reach the middle pane; the
// full column just comes first. Monitor and bookmarks widgets are
// excluded because they have dedicated panes.
func TestMiddleWidgetsIncludesSmallColumns(t *testing.T) {
	cfg := writeConfig(t, `pages:
  - name: Home
    columns:
      - size: small
        widgets:
          - type: monitor
            title: Services
            sites:
              - title: S
                url: http://s
          - type: custom-api
            title: Left One
            url: http://left/1
      - size: full
        widgets:
          - type: custom-api
            title: Middle One
            url: http://mid/1
          - type: custom-api
            title: Middle Two
            url: http://mid/2
      - size: small
        widgets:
          - type: bookmarks
            title: Links
            groups:
              - title: G
                links:
                  - title: L
                    url: http://l
          - type: custom-api
            title: Right One
            url: http://right/1
`)

	want := []string{"Middle One", "Middle Two", "Left One", "Right One"}
	if got := cfg.MiddleWidgets(); !sameTitles(got, want) {
		t.Errorf("MiddleWidgets() = %v, want %v", widgetTitles(got), want)
	}
}

// With no `size: full` column, every non-monitor/non-bookmarks widget is
// returned in document order.
func TestMiddleWidgetsNoFullColumn(t *testing.T) {
	cfg := writeConfig(t, `pages:
  - name: Home
    columns:
      - size: small
        widgets:
          - type: custom-api
            title: One
            url: http://one
          - type: monitor
            title: Services
            sites:
              - title: S
                url: http://s
      - size: small
        widgets:
          - type: weather
            title: Two
            location: Oakland
`)

	want := []string{"One", "Two"}
	if got := cfg.MiddleWidgets(); !sameTitles(got, want) {
		t.Errorf("MiddleWidgets() = %v, want %v", widgetTitles(got), want)
	}
}

func TestMiddleWidgetsEmptyConfig(t *testing.T) {
	cfg := writeConfig(t, "pages: []\n")
	if got := cfg.MiddleWidgets(); got != nil {
		t.Errorf("MiddleWidgets() = %v, want nil", got)
	}
}
