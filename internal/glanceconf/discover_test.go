package glanceconf

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, body string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// chdir moves into an isolated dir with no HOME/XDG config so discovery
// only sees what the test creates.
func isolate(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	t.Setenv("HOME", filepath.Join(dir, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "home", ".config"))
	t.Setenv(ConfigEnvVar, "")
	t.Setenv(EnvFileEnvVar, "")
	return dir
}

func TestDiscoverConfigXDG(t *testing.T) {
	dir := isolate(t)
	want := write(t, filepath.Join(dir, "home", ".config", "glancectl", "glance.yml"), "pages: []\n")

	got, err := DiscoverConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestDiscoverConfigEnvVarWins(t *testing.T) {
	dir := isolate(t)
	write(t, filepath.Join(dir, "home", ".config", "glancectl", "glance.yml"), "pages: []\n")
	write(t, filepath.Join(dir, "configs", "glance", "glance.yml"), "pages: []\n")
	want := write(t, filepath.Join(dir, "custom", "my.yml"), "pages: []\n")
	t.Setenv(ConfigEnvVar, want)

	got, err := DiscoverConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestDiscoverConfigEnvVarDirectory(t *testing.T) {
	dir := isolate(t)
	want := write(t, filepath.Join(dir, "custom", "glance.yaml"), "pages: []\n")
	t.Setenv(ConfigEnvVar, filepath.Join(dir, "custom"))

	got, err := DiscoverConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestDiscoverConfigProjectLocalBeatsHome(t *testing.T) {
	dir := isolate(t)
	write(t, filepath.Join(dir, "home", ".config", "glancectl", "glance.yml"), "pages: []\n")
	want := write(t, filepath.Join(dir, "configs", "glance", "glance.yml"), "pages: []\n")

	got, err := DiscoverConfig("")
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestDiscoverConfigNotFound(t *testing.T) {
	isolate(t)
	if _, err := DiscoverConfig(""); err == nil {
		t.Fatal("expected error when no config exists")
	}
}

func TestDiscoverConfigExplicitMissing(t *testing.T) {
	dir := isolate(t)
	write(t, filepath.Join(dir, "configs", "glance", "glance.yml"), "pages: []\n")
	if _, err := DiscoverConfig(filepath.Join(dir, "nope.yml")); err == nil {
		t.Fatal("explicit --config must not silently fall back")
	}
}

func TestDiscoverEnvFileBesideConfig(t *testing.T) {
	dir := isolate(t)
	cfg := write(t, filepath.Join(dir, "configs", "glance", "glance.yml"), "pages: []\n")
	want := write(t, filepath.Join(dir, "configs", "glance", ".env"), "K=V\n")

	if got := DiscoverEnvFile("", cfg); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func TestDiscoverEnvFileNoneFound(t *testing.T) {
	dir := isolate(t)
	cfg := write(t, filepath.Join(dir, "configs", "glance", "glance.yml"), "pages: []\n")
	if got := DiscoverEnvFile("", cfg); got != "" {
		t.Errorf("expected empty, got %s", got)
	}
}
