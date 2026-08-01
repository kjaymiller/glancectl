// glancectl is a TUI dashboard that reads the same Glance configs you
// already use for the web dashboard. It renders a 3-pane view:
// services health, just recipes (actions), and bookmarks.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/kjaymiller/glancectl/internal/glanceconf"
	"github.com/kjaymiller/glancectl/internal/ui"
)

var version = "dev"

func main() {
	var (
		cfgPath string
		envPath string
		workdir string
		rewrite string
		refresh time.Duration
		showVer bool
	)
	flag.StringVar(&cfgPath, "config", "", "path to glance.yml (default: $GLANCECTL_CONFIG_PATH, then ./configs/glance/glance.yml, then ~/.config/glancectl/glance.yml)")
	flag.StringVar(&envPath, "env", "", "path to .env for ${VAR} substitution (default: $GLANCECTL_ENV_PATH, then .env beside the config)")
	flag.StringVar(&workdir, "workdir", ".", "working directory for `just` commands")
	flag.StringVar(&rewrite, "rewrite", "", "comma-separated host[:port]=replacement rules for URLs unreachable from this host (default: $GLANCECTL_REWRITE)")
	flag.DurationVar(&refresh, "refresh", 30*time.Second, "auto-refresh interval")
	flag.BoolVar(&showVer, "version", false, "print version and exit")
	flag.Parse()

	if showVer {
		fmt.Println(version)
		return
	}

	abs := func(p string) string {
		if a, err := filepath.Abs(p); err == nil {
			return a
		}
		return p
	}

	resolved, err := glanceconf.DiscoverConfig(cfgPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	cfg, err := glanceconf.Load(resolved, glanceconf.DiscoverEnvFile(envPath, resolved))
	if err != nil {
		fmt.Fprintln(os.Stderr, "load config:", err)
		os.Exit(1)
	}

	if rewrite == "" {
		rewrite = os.Getenv(glanceconf.RewriteEnvVar)
	}
	rules, err := glanceconf.ParseRewrites(rewrite)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	cfg.ApplyRewrites(rules)

	m := ui.New(ui.Options{
		Config:       cfg,
		Workdir:      abs(workdir),
		RefreshEvery: refresh,
	})

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
