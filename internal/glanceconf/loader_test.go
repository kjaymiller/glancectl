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
