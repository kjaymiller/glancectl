package sources

import (
	"bufio"
	"os/exec"
	"strings"
)

type Recipe struct {
	Group string
	Name  string
	Doc   string
}

// ListRecipes runs `just --list --unsorted` in workdir and parses its
// output into Recipe entries. Group/comment headers in the listing
// (lines starting with "[group" or "#") are tracked and applied to
// subsequent recipes.
func ListRecipes(workdir string) ([]Recipe, error) {
	cmd := exec.Command("just", "--list", "--unsorted")
	cmd.Dir = workdir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var recipes []Recipe
	group := ""
	scan := bufio.NewScanner(strings.NewReader(string(out)))
	for scan.Scan() {
		line := scan.Text()
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "Available recipes:") {
			continue
		}
		// Group header lines look like "[backup]" indented.
		if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") {
			group = strings.TrimSuffix(strings.TrimPrefix(trim, "["), "]")
			continue
		}
		// Recipe lines are indented; format: `name args # doc`
		if !strings.HasPrefix(line, "    ") {
			continue
		}
		body := strings.TrimSpace(line)
		name := body
		doc := ""
		if i := strings.Index(body, "#"); i >= 0 {
			name = strings.TrimSpace(body[:i])
			doc = strings.TrimSpace(body[i+1:])
		}
		// Strip arg placeholders so the recipe name is just the first token.
		if i := strings.IndexByte(name, ' '); i >= 0 {
			name = name[:i]
		}
		recipes = append(recipes, Recipe{Group: group, Name: name, Doc: doc})
	}
	return recipes, scan.Err()
}
