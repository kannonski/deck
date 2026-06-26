package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/charmbracelet/lipgloss"
)

// colConf is one configured column. An open task lands in the first column whose
// `tag` it carries; if none match it falls to the `pool` column (skipping
// `hide_priority`). Resolved-today tasks go to the `resolved_today` column.
type colConf struct {
	Title         string `toml:"title"`
	Accent        string `toml:"accent"`
	Tag           string `toml:"tag"`
	Pool          bool   `toml:"pool"`
	HidePriority  string `toml:"hide_priority"`
	ResolvedToday bool   `toml:"resolved_today"`
}

type themeConf struct {
	ID     string            `toml:"id"`     // task id colour
	Dim    string            `toml:"dim"`    // meta / hints
	Select string            `toml:"select"` // cursor / highlights
	Active string            `toml:"active"` // ▶ active marker
	Area   map[string]string `toml:"area"`   // dstask project → colour ("default" = fallback)
}

// Config is deck's full configuration. Resolution order: defaults → DECK_* env → the
// TOML file (the file supersedes env).
type Config struct {
	Hooks struct {
		Open   string `toml:"open"`   // enter
		Agent  string `toml:"agent"`  // :
		Enrich string `toml:"enrich"` // e
		Ingest string `toml:"ingest"` // I
	} `toml:"hooks"`
	Cards struct {
		Dir string `toml:"dir"`
	} `toml:"cards"`
	Focus struct {
		Minutes int `toml:"minutes"`
	} `toml:"focus"`
	UI struct {
		DetailFraction float64 `toml:"detail_fraction"` // detail pane height ≈ this × screen
		Mouse          bool    `toml:"mouse"`           // wheel-scroll, click-select, drag-to-column (off by default: capture disables native text selection — Shift to select)
	} `toml:"ui"`
	Theme   themeConf `toml:"theme"`
	Columns []colConf `toml:"columns"`
}

// cfg is the process-wide config, populated once in main() before the TUI starts.
var cfg = defaultConfig()

func defaultConfig() Config {
	var c Config
	c.Focus.Minutes = 25
	c.UI.DetailFraction = 0.40
	c.Theme = themeConf{
		ID: "117", Dim: "240", Select: "212", Active: "120",
		Area: map[string]string{"customer": "211", "team": "117", "work": "180", "personal": "150", "default": "245"},
	}
	c.Columns = []colConf{
		{Title: "★ TODAY", Accent: "183", Tag: "now"},
		{Title: "NEXT", Accent: "117", Pool: true, HidePriority: "P3"},
		{Title: "WAITING", Accent: "245", Tag: "waiting"},
		{Title: "✓ DONE", Accent: "120", ResolvedToday: true},
	}
	return c
}

func configPath() string {
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		return filepath.Join(x, "deck", "config.toml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "deck", "config.toml")
}

func expandTilde(s string) string {
	if strings.HasPrefix(s, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return home + s[1:]
		}
	}
	return s
}

// loadConfig resolves the config: defaults, then DECK_* env, then the TOML file (wins).
func loadConfig() Config {
	c := defaultConfig()
	for env, dst := range map[string]*string{
		"DECK_OPEN_CMD":   &c.Hooks.Open,
		"DECK_AGENT_CMD":  &c.Hooks.Agent,
		"DECK_ENRICH_CMD": &c.Hooks.Enrich,
		"DECK_INGEST_CMD": &c.Hooks.Ingest,
		"DECK_CARD_DIR":   &c.Cards.Dir,
	} {
		if v := os.Getenv(env); v != "" {
			*dst = v
		}
	}
	if v := os.Getenv("DECK_MOUSE"); v == "1" || v == "true" {
		c.UI.Mouse = true
	}
	if b, err := os.ReadFile(configPath()); err == nil {
		if err := toml.Unmarshal(b, &c); err != nil {
			fmt.Fprintf(os.Stderr, "deck: %s: %v\n", configPath(), err)
		}
	}
	c.Cards.Dir = expandTilde(c.Cards.Dir)
	c.Hooks.Open = expandTilde(c.Hooks.Open)
	c.Hooks.Agent = expandTilde(c.Hooks.Agent)
	c.Hooks.Enrich = expandTilde(c.Hooks.Enrich)
	c.Hooks.Ingest = expandTilde(c.Hooks.Ingest)
	if c.Focus.Minutes <= 0 {
		c.Focus.Minutes = 25
	}
	if c.UI.DetailFraction <= 0 {
		c.UI.DetailFraction = 0.40
	}
	if len(c.Columns) == 0 {
		c.Columns = defaultConfig().Columns
	}
	if c.Theme.Area == nil {
		c.Theme.Area = map[string]string{}
	}
	if c.Theme.Area["default"] == "" {
		c.Theme.Area["default"] = "245"
	}
	return c
}

// applyTheme sets the render styles from cfg.Theme (call after loadConfig).
func applyTheme() {
	idStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(cfg.Theme.ID))
	dimStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(cfg.Theme.Dim))
	helpStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(cfg.Theme.Dim))
	selStyle = lipgloss.NewStyle().Foreground(lipgloss.Color(cfg.Theme.Select)).Bold(true)
}

// columnTags returns the tag of every tag-based column (e.g. ["now","waiting"]).
func columnTags() []string {
	var t []string
	for _, c := range cfg.Columns {
		if c.Tag != "" {
			t = append(t, c.Tag)
		}
	}
	return t
}
