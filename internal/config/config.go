// Package config reads the agent-sesh settings file.
//
//	AGENT_SESH_CONFIG  file path, overrides every default
//	AGENT_SESH_AGENTS  comma-separated agent names, overrides the file
//
// Without AGENT_SESH_CONFIG, agent-sesh reads
// $XDG_CONFIG_HOME/agent-sesh/config.json and falls back to
// ~/.config/agent-sesh/config.json. The Home Manager module in flake.nix writes
// that file from programs.agent-sesh.agents.
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DefaultAgent is the agent name agent-sesh assumes when nothing is configured.
const DefaultAgent = "pi"

// cacheTTL bounds how often a long-running picker re-reads the file.
const cacheTTL = time.Second

// Config holds the settings one agent-sesh run reads.
type Config struct {
	Version int      `json:"version"`
	Agents  []string `json:"agents"`
}

var (
	cacheMu    sync.Mutex
	cached     []string
	cachedAt   time.Time
	cachedFrom string
)

// Path returns the config file to read, or "" when no file exists.
func Path() string {
	if p := strings.TrimSpace(os.Getenv("AGENT_SESH_CONFIG")); p != "" {
		return p
	}

	var paths []string
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		paths = append(paths, filepath.Join(xdg, "agent-sesh", "config.json"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, ".config", "agent-sesh", "config.json"))
	}
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p
		}
	}
	return ""
}

// Load reads the config file. A missing or unreadable file gives the defaults.
func Load() Config {
	if raw := strings.TrimSpace(os.Getenv("AGENT_SESH_AGENTS")); raw != "" {
		return Config{Agents: normalize(strings.Split(raw, ","))}
	}

	cfg := Config{Agents: []string{DefaultAgent}}
	data, err := os.ReadFile(Path())
	if err != nil {
		return cfg
	}
	var file Config
	if err := json.Unmarshal(data, &file); err != nil {
		return cfg
	}
	if agents := normalize(file.Agents); len(agents) > 0 {
		cfg.Agents = agents
	}
	return cfg
}

// Agents returns the configured agent names. The answer is cached for one second,
// so a ps scan reads the file once instead of once per process line.
func Agents() []string {
	path := Path()

	cacheMu.Lock()
	defer cacheMu.Unlock()
	if cachedFrom == path && time.Since(cachedAt) < cacheTTL {
		return cached
	}
	cached = Load().Agents
	cachedFrom = path
	cachedAt = time.Now()
	return cached
}

// IsAgent reports whether name is a configured agent name.
func IsAgent(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		name = DefaultAgent
	}
	for _, agent := range Agents() {
		if agent == name {
			return true
		}
	}
	return false
}

// normalize trims names, drops empty entries, and keeps the first of each repeat.
func normalize(names []string) []string {
	out := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	return out
}
