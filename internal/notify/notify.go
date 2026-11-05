// Package notify delivers agent-sesh events to the user.
//
// The osc backend writes an OSC 777 escape to a terminal device, so a client
// attached over SSH gets a native notification from its own terminal. The
// command backend hands the event to a shell command with AGENT_SESH_*
// environment variables, which covers ntfy, Pushover, or a log pipe.
package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/mattgmak/agent-sesh/internal/tmux"
)

// commandTimeout bounds one notification command.
const commandTimeout = 10 * time.Second

// Backends the resolver can pick.
const (
	BackendAuto       = "auto"
	BackendOSC        = "osc"
	BackendNotifySend = "notify-send"
	BackendOSAScript  = "osascript"
	BackendCommand    = "command"
	BackendLog        = "log"
)

var knownBackends = map[string]struct{}{
	BackendAuto:       {},
	BackendOSC:        {},
	BackendNotifySend: {},
	BackendOSAScript:  {},
	BackendCommand:    {},
	BackendLog:        {},
}

// Event is one notification payload. Empty text fields are filled from the
// session fields by Render.
type Event struct {
	Event     string `json:"event"`
	SessionID string `json:"session_id"`
	Target    string `json:"target"`
	Agent     string `json:"agent"`
	Project   string `json:"project"`
	Branch    string `json:"branch"`
	Title     string `json:"title"`
	Subtitle  string `json:"subtitle"`
	Body      string `json:"body"`
}

// Options controls delivery.
type Options struct {
	// Backend forces a channel. Empty means auto.
	Backend string
	// TTY pins the terminal device for the osc backend.
	TTY string
	// Command is the shell command for the command backend.
	Command string
	// Stdout receives log output and the osc fallback. Defaults to os.Stderr.
	Stdout io.Writer
}

// Env is the machine facts the auto resolver needs. It is a parameter so the
// resolver stays testable without a real macOS or Linux host.
type Env struct {
	GOOS        string
	OSAScript   bool
	NotifySend  bool
	DBUSSet     bool
	DisplaySet  bool
	InTmux      bool
	HasTTY      bool
	HasCommand  bool
	HasStdoutOK bool
}

// run executes one command and returns its combined output. Tests replace it.
var run = func(name string, args []string, env []string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// exists reports whether path is an executable file.
var exists = func(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// lookPath resolves a binary on PATH. Tests replace it.
var lookPath = func(name string) (string, error) { return exec.LookPath(name) }

// Resolve picks the backend for an event. An explicit backend wins; auto picks
// osascript on macOS, notify-send on a Linux desktop, osc inside tmux, then log.
func Resolve(explicit string, env Env) (string, error) {
	backend := strings.ToLower(strings.TrimSpace(explicit))
	if backend == "" {
		backend = BackendAuto
	}
	if _, ok := knownBackends[backend]; !ok {
		return "", fmt.Errorf("unknown notify backend %q", explicit)
	}
	if backend != BackendAuto {
		if backend == BackendCommand && !env.HasCommand {
			return "", fmt.Errorf("command backend needs a command")
		}
		return backend, nil
	}
	switch {
	case env.GOOS == "darwin" && env.OSAScript:
		return BackendOSAScript, nil
	case env.NotifySend && (env.DBUSSet || env.DisplaySet):
		return BackendNotifySend, nil
	case env.InTmux && env.HasTTY:
		return BackendOSC, nil
	default:
		return BackendLog, nil
	}
}

// escapeOSC strips the characters that would end the escape early.
func escapeOSC(s string) string {
	s = strings.ReplaceAll(s, "\x07", " ")
	s = strings.ReplaceAll(s, "\x1b", " ")
	s = strings.ReplaceAll(s, ";", ",")
	if r := []rune(s); len(r) > 500 {
		return string(r[:500])
	}
	return s
}

// OSCSequence builds the OSC 777 notify escape that terminal emulators such as
// kitty, Ghostty, WezTerm, and iTerm2 render as a native notification.
func OSCSequence(title, body string) string {
	return "\x1b]777;notify;" + escapeOSC(title) + ";" + escapeOSC(body) + "\x07"
}

// Render fills the title and body from the session fields when they are empty.
func Render(e Event) (title, body string) {
	title = strings.TrimSpace(e.Title)
	body = strings.TrimSpace(e.Body)
	subtitle := strings.TrimSpace(e.Subtitle)
	if title == "" {
		switch {
		case e.Agent != "" && e.SessionID != "":
			title = e.Agent + " " + shortID(e.SessionID)
		case e.Agent != "":
			title = e.Agent
		case e.SessionID != "":
			title = shortID(e.SessionID)
		default:
			title = "agent-sesh"
		}
	}
	if e.Event != "" {
		body = strings.TrimSpace(body + " " + e.Event)
	}
	if body == "" {
		switch {
		case subtitle != "":
			body = subtitle
		case e.Branch != "":
			body = e.Branch
		case e.Project != "":
			body = e.Project
		default:
			body = "no detail"
		}
	}
	return title, body
}

func shortID(id string) string {
	id = strings.TrimSpace(id)
	if len(id) <= 8 {
		return id
	}
	return id[:8]
}

// eventEnv exports the event as AGENT_SESH_* variables for the command backend.
func eventEnv(e Event, title, body string) []string {
	return []string{
		"AGENT_SESH_EVENT=" + e.Event,
		"AGENT_SESH_SESSION_ID=" + e.SessionID,
		"AGENT_SESH_TARGET=" + e.Target,
		"AGENT_SESH_AGENT=" + e.Agent,
		"AGENT_SESH_PROJECT=" + e.Project,
		"AGENT_SESH_BRANCH=" + e.Branch,
		"AGENT_SESH_TITLE=" + title,
		"AGENT_SESH_SUBTITLE=" + e.Subtitle,
		"AGENT_SESH_BODY=" + body,
	}
}

// Send delivers one event through the chosen backend.
func Send(e Event, opts Options) error {
	env := envFor(opts)
	backend, err := Resolve(opts.Backend, env)
	if err != nil {
		return err
	}
	title, body := Render(e)
	switch backend {
	case BackendLog:
		return writeLog(opts, e, title, body)
	case BackendOSC:
		return sendOSC(e, opts, title, body)
	case BackendCommand:
		out, err := run("sh", []string{"-c", opts.Command}, eventEnv(e, title, body))
		if err != nil {
			return fmt.Errorf("notify command: %w: %s", err, strings.TrimSpace(out))
		}
		return nil
	case BackendNotifySend:
		bin, err := lookPath("notify-send")
		if err != nil {
			return err
		}
		if _, err := run(bin, []string{title, body}, nil); err != nil {
			return fmt.Errorf("notify-send: %w", err)
		}
		return nil
	case BackendOSAScript:
		bin, err := lookPath("osascript")
		if err != nil {
			return err
		}
		script := fmt.Sprintf("display notification %q with title %q", body, title)
		if _, err := run(bin, []string{"-e", script}, nil); err != nil {
			return fmt.Errorf("osascript: %w", err)
		}
		return nil
	}
	return fmt.Errorf("unhandled backend %q", backend)
}

// SendEventJSON decodes a marshalled Event and sends it. The daemon uses it for
// SSE-triggered notifications.
func SendEventJSON(raw []byte, opts Options) error {
	var e Event
	if err := json.Unmarshal(raw, &e); err != nil {
		return fmt.Errorf("decode notify event: %w", err)
	}
	return Send(e, opts)
}

func writeLog(opts Options, e Event, title, body string) error {
	w := opts.Stdout
	if w == nil {
		w = os.Stderr
	}
	line := fmt.Sprintf("notify: %s | %s", title, body)
	if e.Target != "" {
		line += " (" + e.Target + ")"
	}
	_, err := io.WriteString(w, line+"\n")
	return err
}

// sendOSC writes the escape to a terminal device. It falls back to opts.Stdout
// when no client tty is reachable.
func sendOSC(e Event, opts Options, title, body string) error {
	tty := strings.TrimSpace(opts.TTY)
	if tty == "" {
		found, err := tmux.ClientTTY(e.Target)
		if err == nil {
			tty = found
		}
	}
	seq := OSCSequence(title, body)
	if tty != "" {
		if err := writeTTY(tty, seq); err == nil {
			return nil
		}
	}
	return writeLog(opts, e, title, body)
}

func writeTTY(tty, s string) error {
	f, err := os.OpenFile(tty, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	_, err = io.WriteString(f, s)
	return err
}

// envFor collects the machine facts for the resolver.
func envFor(opts Options) Env {
	notifySend, _ := lookPath("notify-send")
	env := Env{
		GOOS:       runtime.GOOS,
		NotifySend: notifySend != "",
		DBUSSet:    os.Getenv("DBUS_SESSION_BUS_ADDRESS") != "",
		DisplaySet: os.Getenv("DISPLAY") != "",
		InTmux:     os.Getenv("TMUX") != "",
		HasCommand: strings.TrimSpace(opts.Command) != "",
		HasTTY:     strings.TrimSpace(opts.TTY) != "",
	}
	if path := "/usr/bin/osascript"; exists(path) {
		env.OSAScript = true
	}
	if !env.InTmux || env.HasTTY {
		return env
	}
	if _, err := tmux.ClientTTY(""); err == nil {
		env.HasTTY = true
	}
	return env
}
