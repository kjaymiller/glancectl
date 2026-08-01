package glanceconf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ConfigEnvVar / EnvFileEnvVar let users point glancectl at a config
// without passing flags every run.
const (
	ConfigEnvVar  = "GLANCECTL_CONFIG_PATH"
	EnvFileEnvVar = "GLANCECTL_ENV_PATH"
)

// configFileNames are the names looked for inside a directory when the
// candidate path is a directory rather than a file.
var configFileNames = []string{"glance.yml", "glance.yaml", "config.yml", "config.yaml"}

// ConfigSearchPaths returns, in precedence order, every location that
// DiscoverConfig will check. Exported so `--help` and error messages can
// show the user exactly where we looked.
func ConfigSearchPaths() []string {
	var paths []string
	if v := os.Getenv(ConfigEnvVar); v != "" {
		paths = append(paths, v)
	}
	// Project-local: the layout glancectl shipped with originally.
	paths = append(paths, filepath.Join("configs", "glance", "glance.yml"), "glance.yml")
	for _, dir := range userConfigDirs() {
		paths = append(paths, dir)
	}
	return paths
}

// userConfigDirs returns the XDG-convention directories, most specific
// first: $XDG_CONFIG_HOME/glancectl, ~/.config/glancectl, then Glance's
// own ~/.config/glance so an existing Glance install works untouched.
func userConfigDirs() []string {
	var dirs []string
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		dirs = append(dirs, filepath.Join(x, "glancectl"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		base := filepath.Join(home, ".config")
		for _, name := range []string{"glancectl", "glance"} {
			d := filepath.Join(base, name)
			if !containsStr(dirs, d) {
				dirs = append(dirs, d)
			}
		}
	}
	return dirs
}

// DiscoverConfig resolves the config path. An explicit path (from
// --config) is used as-is and must exist; otherwise the search paths are
// tried in order and the first readable config wins.
func DiscoverConfig(explicit string) (string, error) {
	if explicit != "" {
		p, err := resolveCandidate(explicit)
		if err != nil {
			return "", err
		}
		return p, nil
	}
	searched := ConfigSearchPaths()
	for _, c := range searched {
		if p, err := resolveCandidate(c); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("no glance config found; set %s, pass --config, or create one of:\n  %s",
		ConfigEnvVar, strings.Join(describe(searched), "\n  "))
}

// resolveCandidate turns a candidate path into an existing config file:
// a file is returned as-is, a directory is scanned for a known config
// file name.
func resolveCandidate(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !st.IsDir() {
		return abs(path), nil
	}
	for _, name := range configFileNames {
		p := filepath.Join(path, name)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return abs(p), nil
		}
	}
	return "", fmt.Errorf("no %s in %s", strings.Join(configFileNames, "/"), path)
}

// DiscoverEnvFile resolves the dotenv file used for ${VAR} substitution.
// Unlike the config it is optional: an empty return means "none found".
// An explicit path is returned even if missing so Load can report it.
func DiscoverEnvFile(explicit, cfgPath string) string {
	if explicit != "" {
		return abs(explicit)
	}
	var candidates []string
	if v := os.Getenv(EnvFileEnvVar); v != "" {
		candidates = append(candidates, v)
	}
	if cfgPath != "" {
		candidates = append(candidates, filepath.Join(filepath.Dir(cfgPath), ".env"))
		// Also look beside the config's real location, so a symlinked
		// config picks up the .env stored next to its source.
		if real, err := filepath.EvalSymlinks(cfgPath); err == nil {
			if d := filepath.Join(filepath.Dir(real), ".env"); !containsStr(candidates, d) {
				candidates = append(candidates, d)
			}
		}
	}
	candidates = append(candidates, filepath.Join("compose", "glance", ".env"))
	for _, dir := range userConfigDirs() {
		candidates = append(candidates, filepath.Join(dir, ".env"))
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			return abs(c)
		}
	}
	return ""
}

// describe annotates directory candidates so the error message reads as
// the file the user actually needs to create.
func describe(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if strings.HasSuffix(p, ".yml") || strings.HasSuffix(p, ".yaml") {
			out = append(out, p)
			continue
		}
		out = append(out, filepath.Join(p, "glance.yml"))
	}
	return out
}

func abs(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
