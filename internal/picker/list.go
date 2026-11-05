package picker

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/mattgmak/agent-sesh/internal/registry"
	"github.com/mattgmak/agent-sesh/internal/tmux"
)

// Lookup finds one session by id, tmux target, title, or pi session name.
func Lookup(key string) (registry.Session, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return registry.Session{}, false, fmt.Errorf("empty session key")
	}
	path, err := registry.DefaultPath()
	if err != nil {
		return registry.Session{}, false, err
	}
	sessions, err := registry.Load(path)
	if err != nil {
		return registry.Session{}, false, err
	}
	for _, session := range sessions {
		if session.ID == key || session.TmuxTarget == key || session.Title == key || session.PiSessionName == key {
			return session, true, nil
		}
	}
	return registry.Session{}, false, nil
}

// ResolveTarget maps a session id, pi session name, or title onto its tmux
// target. An unknown key that still parses as a tmux target passes through.
func ResolveTarget(key string) (string, error) {
	key = strings.TrimSpace(key)
	session, ok, err := Lookup(key)
	if err != nil {
		return "", err
	}
	if ok {
		return session.TmuxTarget, nil
	}
	if kind, _ := tmux.ParseTarget(key); kind != tmux.TargetUnknown {
		return key, nil
	}
	return "", fmt.Errorf("session %q not found", key)
}

// WriteSessionsJSON renders registry rows as an indented JSON array.
func WriteSessionsJSON(w io.Writer, sessions []registry.Session) error {
	if sessions == nil {
		sessions = []registry.Session{}
	}
	data, err := json.MarshalIndent(sessions, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}
