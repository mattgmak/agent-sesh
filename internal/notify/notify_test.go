package notify

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withStubs(t *testing.T, newRun func(string, []string, []string) (string, error), newLook func(string) (string, error)) {
	t.Helper()
	oldRun, oldLook, oldExists := run, lookPath, exists
	t.Cleanup(func() { run, lookPath, exists = oldRun, oldLook, oldExists })
	if newRun != nil {
		run = newRun
	}
	if newLook != nil {
		lookPath = newLook
	}
	exists = func(string) bool { return false }
}

func TestResolveBackends(t *testing.T) {
	tests := []struct {
		name     string
		explicit string
		env      Env
		want     string
		wantErr  bool
	}{
		{
			name: "mac osascript",
			env:  Env{GOOS: "darwin", OSAScript: true},
			want: BackendOSAScript,
		},
		{
			name: "linux notify-send",
			env:  Env{GOOS: "linux", NotifySend: true, DBUSSet: true},
			want: BackendNotifySend,
		},
		{
			name: "notify-send without bus",
			env:  Env{GOOS: "linux", NotifySend: true},
			want: BackendLog,
		},
		{
			name: "tmux over ssh",
			env:  Env{GOOS: "linux", InTmux: true, HasTTY: true},
			want: BackendOSC,
		},
		{
			name: "headless falls back to log",
			env:  Env{GOOS: "linux"},
			want: BackendLog,
		},
		{
			name:     "explicit command without command",
			explicit: BackendCommand,
			env:      Env{GOOS: "linux"},
			wantErr:  true,
		},
		{
			name:     "explicit wins over auto",
			explicit: "LOG",
			env:      Env{GOOS: "darwin", OSAScript: true},
			want:     BackendLog,
		},
		{
			name:     "unknown backend",
			explicit: "pager",
			env:      Env{GOOS: "linux"},
			wantErr:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.explicit, tt.env)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestOSCSequence(t *testing.T) {
	got := OSCSequence("pi ab12cd34", "tool_call Read")
	want := "\x1b]777;notify;pi ab12cd34;tool_call Read\x07"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := OSCSequence("a;b\x07c\x1bd", "x"); got != "\x1b]777;notify;a,b c d;x\x07" {
		t.Fatalf("escaping broke the sequence: %q", got)
	}
	long := OSCSequence(strings.Repeat("z", 600), "b")
	if !strings.HasPrefix(long, "\x1b]777;notify;") || len([]rune(long)) > 520 {
		t.Fatalf("title not truncated: %d runes", len([]rune(long)))
	}
}

func TestRenderEvent(t *testing.T) {
	title, body := Render(Event{Title: "pinned", Body: "body"})
	if title != "pinned" || body != "body" {
		t.Fatalf("explicit fields changed: %q %q", title, body)
	}

	title, body = Render(Event{Event: "awaiting_input", Agent: "pi-local", SessionID: "12345678-9abc-def0"})
	if title != "pi-local 12345678" {
		t.Fatalf("title = %q", title)
	}
	if body != "awaiting_input" {
		t.Fatalf("body = %q", body)
	}

	title, body = Render(Event{Agent: "pi", Branch: "main"})
	if title != "pi" {
		t.Fatalf("title = %q", title)
	}
	if body != "main" {
		t.Fatalf("body = %q", body)
	}

	title, _ = Render(Event{})
	if title != "agent-sesh" {
		t.Fatalf("empty event title = %q", title)
	}
}

func TestSendLogBackend(t *testing.T) {
	withStubs(t, nil, nil)
	var buf bytes.Buffer
	err := Send(Event{Agent: "pi", SessionID: "abcd1234", Body: "done"}, Options{Backend: BackendLog, Stdout: &buf})
	if err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "notify: pi abcd1234 | done\n" {
		t.Fatalf("log output = %q", got)
	}
}

func TestSendCommandBackend(t *testing.T) {
	var gotName string
	var gotArgs []string
	var gotEnv []string
	withStubs(t, func(name string, args []string, env []string) (string, error) {
		gotName, gotArgs, gotEnv = name, args, env
		return "", nil
	}, nil)

	err := Send(Event{Event: "idle", Agent: "pi", SessionID: "abcd1234", Body: "done"}, Options{
		Backend: BackendCommand,
		Command: "cat >> $LOG",
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotName != "sh" || len(gotArgs) != 2 || gotArgs[1] != "cat >> $LOG" {
		t.Fatalf("command argv = %q %q", gotName, gotArgs)
	}
	joined := strings.Join(gotEnv, "\n")
	for _, want := range []string{"AGENT_SESH_EVENT=idle", "AGENT_SESH_SESSION_ID=abcd1234", "AGENT_SESH_AGENT=pi", "AGENT_SESH_BODY=done"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in %q", want, joined)
		}
	}
}

func TestSendOSCWritesTTY(t *testing.T) {
	withStubs(t, nil, nil)
	path := filepath.Join(t.TempDir(), "tty")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := Send(Event{Title: "pi x", Body: "working", Event: "tool_call"}, Options{Backend: BackendOSC, TTY: path})
	if err != nil {
		t.Fatal(err)
	}
	// Render appends the event to the body, so the sequence holds "working tool_call".
	want := OSCSequence("pi x", "working tool_call")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("tty content = %q, want %q", got, want)
	}
}
