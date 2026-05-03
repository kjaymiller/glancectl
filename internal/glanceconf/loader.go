// Package glanceconf parses the subset of Glance's YAML config that
// glancectl can render in a TUI: monitor sites, bookmarks, and
// custom-api widgets (URL + headers, no HTML templates).
package glanceconf

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Pages []Page
}

type Page struct {
	Name    string
	Columns []Column
}

type Column struct {
	Size    string
	Widgets []Widget
}

// Widget is the lowest-common-denominator shape we extract from each
// supported widget type. Type-specific fields are populated when present.
type Widget struct {
	Type       string
	Title      string
	URL        string
	Headers    map[string]string
	Parameters map[string]string // custom-api query params

	// monitor
	Sites []Site

	// bookmarks
	Groups []BookmarkGroup

	// weather
	Location   string
	Units      string
	HourFormat string
}

type Site struct {
	Title string
	URL   string
}

type BookmarkGroup struct {
	Title string
	Links []Site
}

// Load reads the root glance.yml, resolves $include references relative
// to the file, and substitutes ${VAR} from env (after applying envFile,
// if non-empty and readable, in dotenv-lite form: KEY=VALUE per line).
func Load(path, envFile string) (*Config, error) {
	if envFile != "" {
		_ = applyEnvFile(envFile)
	}

	root, err := readNode(path)
	if err != nil {
		return nil, err
	}
	if err := resolveIncludes(root, filepath.Dir(path)); err != nil {
		return nil, err
	}

	var raw rawConfig
	if err := root.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decode %s: %w", path, err)
	}

	cfg := &Config{}
	for _, p := range raw.Pages {
		page := Page{Name: p.Name}
		for _, c := range p.Columns {
			col := Column{Size: c.Size}
			col.Widgets = flattenWidgets(c.Widgets)
			page.Columns = append(page.Columns, col)
		}
		cfg.Pages = append(cfg.Pages, page)
	}
	return cfg, nil
}

type rawConfig struct {
	Pages []struct {
		Name    string `yaml:"name"`
		Columns []struct {
			Size    string     `yaml:"size"`
			Widgets []rawWidget `yaml:"widgets"`
		} `yaml:"columns"`
	} `yaml:"pages"`
}

type rawWidget struct {
	Type       string            `yaml:"type"`
	Title      string            `yaml:"title"`
	URL        string            `yaml:"url"`
	Headers    map[string]string `yaml:"headers"`
	Parameters map[string]string `yaml:"parameters"`
	Location   string            `yaml:"location"`
	Units      string            `yaml:"units"`
	HourFormat string            `yaml:"hour-format"`

	Sites []struct {
		Title string `yaml:"title"`
		URL   string `yaml:"url"`
	} `yaml:"sites"`

	Groups []struct {
		Title string `yaml:"title"`
		Links []struct {
			Title string `yaml:"title"`
			URL   string `yaml:"url"`
		} `yaml:"links"`
	} `yaml:"groups"`

	// split-column nests more widgets
	Widgets []rawWidget `yaml:"widgets"`
}

func flattenWidgets(in []rawWidget) []Widget {
	var out []Widget
	for _, w := range in {
		if w.Type == "split-column" {
			out = append(out, flattenWidgets(w.Widgets)...)
			continue
		}
		out = append(out, convert(w))
	}
	return out
}

func convert(w rawWidget) Widget {
	out := Widget{
		Type:       w.Type,
		Title:      expand(w.Title),
		URL:        expand(w.URL),
		Headers:    map[string]string{},
		Parameters: map[string]string{},
		Location:   expand(w.Location),
		Units:      w.Units,
		HourFormat: w.HourFormat,
	}
	for k, v := range w.Headers {
		out.Headers[k] = expand(v)
	}
	for k, v := range w.Parameters {
		out.Parameters[k] = expand(v)
	}
	for _, s := range w.Sites {
		out.Sites = append(out.Sites, Site{Title: expand(s.Title), URL: expand(s.URL)})
	}
	for _, g := range w.Groups {
		bg := BookmarkGroup{Title: expand(g.Title)}
		for _, l := range g.Links {
			bg.Links = append(bg.Links, Site{Title: expand(l.Title), URL: expand(l.URL)})
		}
		out.Groups = append(out.Groups, bg)
	}
	return out
}

var envRe = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

func expand(s string) string {
	return envRe.ReplaceAllStringFunc(s, func(m string) string {
		name := envRe.FindStringSubmatch(m)[1]
		if v, ok := os.LookupEnv(name); ok {
			return v
		}
		return ""
	})
}

func readNode(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var n yaml.Node
	if err := yaml.Unmarshal(data, &n); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &n, nil
}

// resolveIncludes walks the YAML tree and replaces every mapping shaped
// like `{ $include: "<path>" }` with the parsed contents of that file.
// Glance's include files are themselves widget lists or single widgets.
func resolveIncludes(n *yaml.Node, baseDir string) error {
	if n == nil {
		return nil
	}
	for i := 0; i < len(n.Content); i++ {
		child := n.Content[i]
		if isIncludeMapping(child) {
			incPath := includePath(child)
			if !filepath.IsAbs(incPath) {
				incPath = filepath.Join(baseDir, incPath)
			}
			incNode, err := readNode(incPath)
			if err != nil {
				return err
			}
			if err := resolveIncludes(incNode, filepath.Dir(incPath)); err != nil {
				return err
			}
			doc := unwrapDoc(incNode)
			// If the include is a list, splice its items into the parent
			// (we're inside a sequence). Otherwise, replace in-place.
			if doc.Kind == yaml.SequenceNode && n.Kind == yaml.SequenceNode {
				n.Content = append(n.Content[:i], append(append([]*yaml.Node{}, doc.Content...), n.Content[i+1:]...)...)
				i += len(doc.Content) - 1
			} else {
				n.Content[i] = doc
			}
			continue
		}
		if err := resolveIncludes(child, baseDir); err != nil {
			return err
		}
	}
	return nil
}

func unwrapDoc(n *yaml.Node) *yaml.Node {
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		return n.Content[0]
	}
	return n
}

func isIncludeMapping(n *yaml.Node) bool {
	if n == nil || n.Kind != yaml.MappingNode || len(n.Content) != 2 {
		return false
	}
	return n.Content[0].Value == "$include"
}

func includePath(n *yaml.Node) string {
	return n.Content[1].Value
}

// Sites returns every monitor site across all pages. Useful for the
// services pane.
func (c *Config) Sites() []Site {
	var out []Site
	for _, p := range c.Pages {
		for _, col := range p.Columns {
			for _, w := range col.Widgets {
				if w.Type == "monitor" {
					out = append(out, w.Sites...)
				}
			}
		}
	}
	return out
}

// Bookmarks returns every bookmark group across all pages.
func (c *Config) Bookmarks() []BookmarkGroup {
	var out []BookmarkGroup
	for _, p := range c.Pages {
		for _, col := range p.Columns {
			for _, w := range col.Widgets {
				if w.Type == "bookmarks" {
					out = append(out, w.Groups...)
				}
			}
		}
	}
	return out
}

// MiddleWidgets returns every widget in the widest column of the first
// page (the "feature" column in Glance), in document order. Falls back
// to all non-monitor/non-bookmarks widgets if no column is `size: full`.
func (c *Config) MiddleWidgets() []Widget {
	if len(c.Pages) == 0 {
		return nil
	}
	page := c.Pages[0]
	for _, col := range page.Columns {
		if col.Size == "full" {
			return col.Widgets
		}
	}
	var out []Widget
	for _, col := range page.Columns {
		for _, w := range col.Widgets {
			if w.Type != "monitor" && w.Type != "bookmarks" {
				out = append(out, w)
			}
		}
	}
	return out
}

// FindCustomAPI returns the first custom-api widget whose title matches
// (case-insensitive substring). Used to pick out alerts / updates / etc.
func (c *Config) FindCustomAPI(titleSubstr string) *Widget {
	titleSubstr = lower(titleSubstr)
	for _, p := range c.Pages {
		for _, col := range p.Columns {
			for i, w := range col.Widgets {
				if w.Type == "custom-api" && contains(lower(w.Title), titleSubstr) {
					return &col.Widgets[i]
				}
			}
		}
	}
	return nil
}

func lower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func contains(s, substr string) bool {
	if substr == "" {
		return true
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
