// Package config loads and saves ~/.config/hads/config.json.
package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"hads/internal/paths"
)

const (
	DefaultModel   = "anthropic/claude-sonnet-5.5"
	DefaultBaseURL = "https://openrouter.ai/api/v1"
)

// Agent is the natural-language mode. It is OFF until the user enables it.
type Agent struct {
	Enabled        bool   `json:"enabled"`
	APIKey         string `json:"api_key,omitempty"`
	Model          string `json:"model"`
	BaseURL        string `json:"base_url"`
	AutoFix        bool   `json:"auto_fix"`
	MaxFixAttempts int    `json:"max_fix_attempts"`
}

type Config struct {
	Roots         []string `json:"roots"`
	Exclude       []string `json:"exclude"`
	MaxFiles      int      `json:"max_files"`
	MaxDepth      int      `json:"max_depth"`
	RescanMinutes int      `json:"rescan_minutes"`
	Agent         Agent    `json:"agent"`
}

func Default() *Config {
	return &Config{
		Roots: []string{"~"},
		Exclude: []string{
			// directory names skipped anywhere
			"node_modules", ".git", ".hg", ".svn", ".cache", ".npm", ".pnpm-store", ".yarn",
			".cargo", ".rustup", ".gradle", ".m2", ".venv", "venv", "__pycache__", ".mypy_cache",
			".pytest_cache", ".tox", ".next", ".nuxt", ".turbo", ".parcel-cache", ".terraform",
			"target", "Trash", ".Trash", "Cache", "Caches", "CacheStorage", "Code Cache",
			"GPUCache", "Service Worker", "IndexedDB", "blob_storage", "$RECYCLE.BIN",
			// absolute subtrees
			"~/Library", "~/AppData", "~/.local/share", "~/.local/lib", "~/snap", "~/.var",
			"~/go/pkg", "~/.wine", "~/.android", "~/.vscode", "~/.mozilla",
		},
		MaxFiles:      250000,
		MaxDepth:      14,
		RescanMinutes: 30,
		Agent: Agent{
			Enabled:        false,
			Model:          DefaultModel,
			BaseURL:        DefaultBaseURL,
			AutoFix:        true,
			MaxFixAttempts: 2,
		},
	}
}

func Load() (*Config, error) {
	c := Default()
	b, err := os.ReadFile(paths.ConfigFile())
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, c); err != nil {
		return Default(), err
	}
	d := Default()
	if len(c.Roots) == 0 {
		c.Roots = d.Roots
	}
	if c.MaxFiles <= 0 {
		c.MaxFiles = d.MaxFiles
	}
	if c.MaxDepth <= 0 {
		c.MaxDepth = d.MaxDepth
	}
	if c.RescanMinutes <= 0 {
		c.RescanMinutes = d.RescanMinutes
	}
	if c.Agent.Model == "" {
		c.Agent.Model = d.Agent.Model
	}
	if c.Agent.BaseURL == "" {
		c.Agent.BaseURL = d.Agent.BaseURL
	}
	if c.Agent.MaxFixAttempts <= 0 {
		c.Agent.MaxFixAttempts = d.Agent.MaxFixAttempts
	}
	return c, nil
}

// Save writes atomically with 0600 permissions (it may hold an API key).
func (c *Config) Save() error {
	if err := paths.Ensure(); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := paths.ConfigFile() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, paths.ConfigFile())
}

// Key returns the OpenRouter key, env vars win over the config file.
func (c *Config) Key() string {
	for _, k := range []string{"HADS_OPENROUTER_KEY", "OPENROUTER_API_KEY"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	return strings.TrimSpace(c.Agent.APIKey)
}

// ExcludeSets splits Exclude into dir-name and absolute-path sets.
func (c *Config) ExcludeSets() (names map[string]bool, abs map[string]bool) {
	names, abs = map[string]bool{}, map[string]bool{}
	for _, e := range c.Exclude {
		if strings.HasPrefix(e, "~") || filepath.IsAbs(e) {
			abs[filepath.Clean(paths.Expand(e))] = true
		} else {
			names[e] = true
		}
	}
	return
}
