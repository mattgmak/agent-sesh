package picker

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/mattgmak/agent-sesh/internal/registry"
	"github.com/mattgmak/agent-sesh/internal/tmux"
)

// ListOptions controls the shape of the non-interactive output.
type ListOptions struct {
	// JSON emits registry rows as a JSON array instead of fzf items.
	JSON bool
}

// List writes the fzf input: one NUL-separated item per session (fzf --read0
// supports multiline items since 0.53), so each entry keeps its wrapped meta
// rows and prompt line while remaining a single selectable candidate.
func List(w io.Writer, opts ListOptions) error {
	path, err := registry.DefaultPath()
	if err != nil {
		return err
	}
	sessions, err := registry.Load(path)
	if err != nil {
		return err
	}
	sessions = sanitizeAndPersist(path, sessions)
	if opts.JSON {
		return WriteSessionsJSON(w, sessions)
	}
	_, err = io.WriteString(w, formatFzfInput(sessions))
	return err
}

// Preview writes tmux capture-pane output for a session id or tmux target.
func Preview(w io.Writer, key string) error {
	target, err := ResolveTarget(key)
	if err != nil {
		return err
	}
	// fzf runs one process per preview call, so a process-local cache cannot
	// hit here. Capture every time and let the caller see live pane text.
	content, err := tmux.CapturePane(target, 0)
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, content)
	return err
}

// RunFzf launches an sesh-style fzf picker with multiline ANSI preview.
func RunFzf() error {
	if _, err := initProfile(); err != nil {
		return fmt.Errorf("init profile: %w", err)
	}
	if _, err := startCPUProfile(); err != nil {
		return fmt.Errorf("init cpu profile: %w", err)
	}
	defer func() {
		if path := stopCPUProfile(); path != "" {
			fmt.Fprintf(os.Stderr, "agent-sesh: cpu profile %s\n", path)
		}
		if path := closeProfile(); path != "" {
			fmt.Fprintf(os.Stderr, "agent-sesh: profile log %s\n", path)
		}
	}()

	path, err := registry.DefaultPath()
	if err != nil {
		return err
	}
	sessions, err := registry.Load(path)
	if err != nil {
		return err
	}
	sessions = sanitizeAndPersist(path, sessions)
	if len(sessions) == 0 {
		return nil
	}

	self, err := os.Executable()
	if err != nil {
		return err
	}

	preview := fmt.Sprintf("%s preview {1}", self)
	cmd := exec.Command(
		"fzf",
		"--no-sort",
		"--ansi",
		"--read0",
		"--highlight-line",
		"--border", "rounded",
		"--border-label", " agent-sesh ",
		"--input-border", "rounded",
		"--preview-border", "rounded",
		"--prompt", "> ",
		"--header", "  Enter attach  ^x kill pane  ^X kill session  ^t new window",
		"--bind", "tab:down,btab:up",
		"--preview-window", "right:50%",
		"--preview", preview,
	)
	// AGENT_SESH_FZF_WINDOW=inline fills the terminal instead of opening an
	// fzf popup. The e2e harness sets it: a popup is a child window with a
	// name the harness cannot predict, and an inline picker draws in the pane
	// it runs in.
	if os.Getenv("AGENT_SESH_FZF_WINDOW") != "inline" {
		cmd.Args = append([]string{"fzf", "--tmux", "90%,90%"}, cmd.Args[1:]...)
	}
	cmd.Stdin = strings.NewReader(formatFzfInput(sessions))
	cmd.Env = os.Environ()
	cmd.Stderr = os.Stderr

	out, err := cmd.Output()
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 130 {
			return nil
		}
		return err
	}

	line := strings.TrimSpace(string(out))
	if line == "" {
		return nil
	}
	fields := strings.SplitN(line, "\t", 2)
	key := fields[0]
	for _, session := range sessions {
		if session.ID != key && session.TmuxTarget != key {
			continue
		}
		return tmux.SwitchClient(session.TmuxTarget)
	}
	return fmt.Errorf("session %q not found", key)
}

func formatFzfInput(sessions []registry.Session) string {
	var b strings.Builder
	for i, session := range sessions {
		if i > 0 {
			b.WriteByte('\x00')
		}
		b.WriteString(formatFzfLine(session))
	}
	return b.String()
}

func formatFzfLine(session registry.Session) string {
	// The key prefixes the item so fzf's {1} (first whitespace-delimited field)
	// and our \t split recover the session — the tmux target is short and
	// display-friendly, the ID is the fallback.
	key := session.TmuxTarget
	if key == "" {
		key = session.ID
	}
	label := formatSessionLine(session, 120)
	// Tabs inside the label would break the \t split.
	label = strings.ReplaceAll(label, "\t", " ")
	return key + "\t" + label
}
