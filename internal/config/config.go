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

// DefaultPort is the TCP port the daemon listens on when config omits one.
const DefaultPort = 2270

// defaultNotifyEvents lists the status changes that raise a notification when
// config omits notify.events.
var defaultNotifyEvents = []string{"awaiting_input", "idle"}

// cacheTTL bounds how often a long-running picker re-reads the file.
const cacheTTL = time.Second

// Config holds the settings one agent-sesh run reads.
type Config struct {
	Version int          `json:"version"`
	Agents  []string     `json:"agents"`
	Notify  NotifyConfig `json:"notify"`
	Daemon  DaemonConfig `json:"daemon"`
}

// NotifyConfig controls event notifications. Enabled stays off until a config
// turns it on, so an existing config.json gains no new side effect.
type NotifyConfig struct {
	Enabled bool `json:"enabled"`
	// Backend is auto, osc, notify-send, osascript, command, or log.
	Backend string `json:"backend,omitempty"`
	// Command is the shell command for the command backend.
	Command string `json:"command,omitempty"`
	// TTY pins the terminal device for the osc backend.
	TTY string `json:"tty,omitempty"`
	// Events lists the statuses that notify. Empty uses defaultNotifyEvents.
	Events []string `json:"events,omitempty"`
}

// DaemonConfig holds daemon settings.
type DaemonConfig struct {
	// Port is the TCP port the daemon listens on.
	Port int `json:"port,omitempty"`
}

// Port returns the configured daemon port, or DefaultPort.
func (c Config) Port() int {
	if c.Daemon.Port > 0 {
		return c.Daemon.Port
	}
	return DefaultPort
}

// EventList returns the notify events, or defaultNotifyEvents when unset.
func (c Config) EventList() []string {
	if events := normalize(c.Notify.Events); len(events) > 0 {
		return events
	}
	return defaultNotifyEvents
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
	cfg.Notify = file.Notify
	cfg.Daemon = file.Daemon
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
