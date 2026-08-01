package glanceconf

import (
	"fmt"
	"net/url"
	"strings"
)

// RewriteEnvVar holds rewrite rules when no --rewrite flag is given.
const RewriteEnvVar = "GLANCECTL_REWRITE"

// Rewriter maps a URL authority to its replacement. Glance runs inside the
// compose network and addresses services by their Docker-network names,
// which do not resolve on the host; a rewrite lets glancectl reach the same
// services from outside without forking the shared config.
//
// Keys are matched most-specific-first: an entry with a port only matches
// that port, an entry without one matches any port on that host.
type Rewriter map[string]string

// ParseRewrites reads rules in `host[:port]=replacement` form, separated by
// commas or newlines. The replacement may be a bare authority or a full URL;
// a scheme in the replacement overrides the original, and a path on it is
// prefixed to the original path.
//
//	update-shim:5000=https://update.example.dev
//	alertmanager=https://alerts.example.dev
func ParseRewrites(spec string) (Rewriter, error) {
	out := Rewriter{}
	for _, field := range strings.FieldsFunc(spec, func(r rune) bool {
		return r == ',' || r == '\n'
	}) {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		key, val, ok := strings.Cut(field, "=")
		key, val = strings.TrimSpace(key), strings.TrimSpace(val)
		if !ok || key == "" || val == "" {
			return nil, fmt.Errorf("rewrite rule %q: want host[:port]=replacement", field)
		}
		// Tolerate a scheme on the key so a rule can be copy-pasted from a
		// widget's url: line.
		if _, rest, found := strings.Cut(key, "://"); found {
			key = rest
		}
		key = strings.TrimSuffix(key, "/")
		out[key] = val
	}
	return out, nil
}

// Apply rewrites raw's authority if a rule matches, otherwise returns it
// unchanged. Anything unparseable is passed through untouched: a rewrite
// should never be the reason a URL stops working.
func (r Rewriter) Apply(raw string) string {
	if len(r) == 0 || raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return raw
	}
	repl, ok := r[u.Host]
	if !ok {
		repl, ok = r[u.Hostname()]
	}
	if !ok {
		return raw
	}

	// A bare authority carries no scheme; give it one so url.Parse reads it
	// as a host rather than a path.
	if !strings.Contains(repl, "://") {
		repl = u.Scheme + "://" + repl
	}
	ru, err := url.Parse(repl)
	if err != nil || ru.Host == "" {
		return raw
	}

	u.Scheme = ru.Scheme
	u.Host = ru.Host
	if p := strings.TrimSuffix(ru.Path, "/"); p != "" {
		u.Path = p + u.Path
	}
	return u.String()
}

// ApplyRewrites rewrites every URL in the config: widget sources, monitor
// sites, and bookmarks alike, so what the UI shows and what it fetches
// cannot drift apart.
func (c *Config) ApplyRewrites(r Rewriter) {
	if len(r) == 0 {
		return
	}
	for pi := range c.Pages {
		for ci := range c.Pages[pi].Columns {
			col := &c.Pages[pi].Columns[ci]
			for wi := range col.Widgets {
				w := &col.Widgets[wi]
				w.URL = r.Apply(w.URL)
				for si := range w.Sites {
					w.Sites[si].URL = r.Apply(w.Sites[si].URL)
				}
				for gi := range w.Groups {
					for li := range w.Groups[gi].Links {
						w.Groups[gi].Links[li].URL = r.Apply(w.Groups[gi].Links[li].URL)
					}
				}
			}
		}
	}
}
