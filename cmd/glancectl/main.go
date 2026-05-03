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
		cfgPath  string
		envPath  string
		workdir  string
		refresh  time.Duration
		showVer  bool
	)
	flag.StringVar(&cfgPath, "config", "configs/glance/glance.yml", "path to glance.yml")
	flag.StringVar(&envPath, "env", "compose/glance/.env", "path to .env for ${VAR} substitution")
	flag.StringVar(&workdir, "workdir", ".", "working directory for `just` commands")
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

	cfg, err := glanceconf.Load(abs(cfgPath), abs(envPath))
	if err != nil {
		fmt.Fprintln(os.Stderr, "load config:", err)
		os.Exit(1)
	}

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
