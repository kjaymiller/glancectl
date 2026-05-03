package sources

import (
	"os"
	"testing"
)

func TestListRecipesHomelab(t *testing.T) {
	wd := os.Getenv("GLANCECTL_TEST_WORKDIR")
	if wd == "" {
		home, _ := os.UserHomeDir()
		wd = home + "/homelab"
	}
	if _, err := os.Stat(wd + "/justfile"); err != nil {
		t.Skip("no justfile available")
	}
	r, err := ListRecipes(wd)
	if err != nil {
		t.Fatalf("ListRecipes: %v", err)
	}
	if len(r) == 0 {
		t.Fatal("no recipes parsed")
	}
	t.Logf("parsed %d recipes; first: [%s] %s — %s", len(r), r[0].Group, r[0].Name, r[0].Doc)
	groups := map[string]bool{}
	for _, x := range r {
		groups[x.Group] = true
	}
	if !groups["backup"] {
		t.Errorf("expected a 'backup' group, got groups: %v", groups)
	}
}
