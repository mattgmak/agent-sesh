package daemon

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mattgmak/agent-sesh/internal/notify"
	"github.com/mattgmak/agent-sesh/internal/registry"
)

func fixture(t *testing.T, sessions ...registry.Session) string {
	t.Helper()
	data, err := json.Marshal(registry.File{Version: 1, Sessions: sessions})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sessions.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func writeFixture(t *testing.T, path string, sessions ...registry.Session) {
	t.Helper()
	data, err := json.Marshal(registry.File{Version: 1, Sessions: sessions})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func session(id, target string, status registry.Status) registry.Session {
	return registry.Session{
		ID:         id,
		TmuxTarget: target,
		Agent:      "pi",
		CWD:        "/repo/" + id,
		Branch:     "main",
		Status:     status,
	}
}

func newServer(t *testing.T, sessions ...registry.Session) (*Server, *bytes.Buffer) {
	t.Helper()
	t.Setenv("AGENT_SESH_AGENTS", "pi")
	var log bytes.Buffer
	server, err := New(Options{
		Addr:     "127.0.0.1:2270",
		Registry: fixture(t, sessions...),
		LogTo:    &log,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server, &log
}

func get(t *testing.T, h http.Handler, method, target string, body string) (int, string) {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, reader)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestNewLoadsRegistry(t *testing.T) {
	server, log := newServer(t,
		session("aaaa1111", "main:1.1", registry.StatusIdle),
		session("bbbb2222", "main:1.2", registry.StatusWorking),
	)
	if got := len(server.Sessions()); got != 2 {
		t.Fatalf("sessions = %d, want 2", got)
	}
	if log.Len() != 0 {
		t.Fatalf("unexpected log output: %q", log.String())
	}
}

func TestHandlerRoutes(t *testing.T) {
	server, _ := newServer(t, session("aaaa1111", "main:1.1", registry.StatusIdle))
	h := server.Handler()

	code, body := get(t, h, http.MethodGet, "/v1/health", "")
	if code != http.StatusOK {
		t.Fatalf("health code = %d, body = %s", code, body)
	}
	var health map[string]any
	if err := json.Unmarshal([]byte(body), &health); err != nil {
		t.Fatalf("health is not JSON: %v\n%s", err, body)
	}
	if health["status"] != "ok" {
		t.Fatalf("health = %v", health)
	}
	if health["sessions"].(float64) != 1 {
		t.Fatalf("health sessions = %v", health["sessions"])
	}

	code, body = get(t, h, http.MethodGet, "/v1/sessions", "")
	if code != http.StatusOK {
		t.Fatalf("sessions code = %d", code)
	}
	var sessions []registry.Session
	if err := json.Unmarshal([]byte(body), &sessions); err != nil {
		t.Fatalf("sessions is not JSON: %v\n%s", err, body)
	}
	if len(sessions) != 1 || sessions[0].ID != "aaaa1111" {
		t.Fatalf("sessions = %+v", sessions)
	}

	code, body = get(t, h, http.MethodGet, "/v1/preview", "")
	if code != http.StatusBadRequest {
		t.Fatalf("preview without target = %d %s", code, body)
	}

	code, body = get(t, h, http.MethodPost, "/v1/sessions", "")
	if code != http.StatusMethodNotAllowed {
		t.Fatalf("POST sessions = %d %s", code, body)
	}

	code, _ = get(t, h, http.MethodGet, "/v1/missing", "")
	if code != http.StatusNotFound {
		t.Fatalf("unknown path = %d", code)
	}

	code, body = get(t, h, http.MethodGet, "/", "")
	if code != http.StatusOK || !strings.Contains(body, "/v1/events") {
		t.Fatalf("index = %d %s", code, body)
	}
}

func TestRefreshDetectsStatusChange(t *testing.T) {
	t.Setenv("AGENT_SESH_AGENTS", "pi")
	var log bytes.Buffer
	path := fixture(t, session("aaaa1111", "main:1.1", registry.StatusWorking))
	var notices bytes.Buffer
	server, err := New(Options{
		Addr:     "127.0.0.1:2270",
		Registry: path,
		NotifyOn: true,
		Notify:   notify.Options{Backend: notify.BackendLog, Stdout: &notices},
		LogTo:    &log,
	})
	if err != nil {
		t.Fatal(err)
	}
	if frame := server.hub.current(); frame == "" {
		t.Fatal("New published no frame")
	}

	writeFixture(t, path, session("aaaa1111", "main:1.1", registry.StatusIdle))
	stamp := time.Now().Add(time.Second)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	changed, err := server.Refresh()
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Fatal("Refresh reported no change after the status moved")
	}
	frame := server.hub.current()
	if !strings.Contains(frame, `"changed":["aaaa1111"]`) {
		t.Fatalf("frame missing the changed id: %s", frame)
	}
	if !strings.Contains(frame, `"idle"`) {
		t.Fatalf("frame missing the new status: %s", frame)
	}
	if got := notices.String(); !strings.Contains(got, "notify: ") || !strings.Contains(got, "idle") {
		t.Fatalf("no notification for a listed status: %q", got)
	}

	if changed, err := server.Refresh(); err != nil || changed {
		t.Fatalf("second Refresh = %v, %v; want false, nil", changed, err)
	}
}

func TestRefreshIgnoresUnlistedStatus(t *testing.T) {
	t.Setenv("AGENT_SESH_AGENTS", "pi")
	path := fixture(t, session("aaaa1111", "main:1.1", registry.StatusWorking))
	var notices bytes.Buffer
	server, err := New(Options{
		Addr:     "127.0.0.1:2270",
		Registry: path,
		NotifyOn: true,
		Notify:   notify.Options{Backend: notify.BackendLog, Stdout: &notices},
		LogTo:    io.Discard,
	})
	if err != nil {
		t.Fatal(err)
	}
	writeFixture(t, path, session("aaaa1111", "main:1.1", registry.StatusToolCall))
	stamp := time.Now().Add(time.Second)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if _, err := server.Refresh(); err != nil {
		t.Fatal(err)
	}
	if notices.Len() != 0 {
		t.Fatalf("tool_call is not on the default list, got %q", notices.String())
	}
}

func TestHubCoalesces(t *testing.T) {
	h := newHub()
	signal, unsubscribe := h.subscribe()
	h.publish("first")
	h.publish("second")
	<-signal
	if got := h.current(); got != "second" {
		t.Fatalf("current = %q", got)
	}
	unsubscribe()
	h.mu.Lock()
	subs := len(h.subs)
	h.mu.Unlock()
	if subs != 0 {
		t.Fatalf("unsubscribe left %d subscribers", subs)
	}
}

func TestPIDFileRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AGENT_SESH_STATE_DIR", dir)

	path, err := PIDPath("127.0.0.1:2270")
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "daemon-127.0.0.1_2270.pid") {
		t.Fatalf("PIDPath = %s", path)
	}
	if _, ok, err := ReadPID("127.0.0.1:2270"); ok || err != nil {
		t.Fatalf("empty read = %v, %v", ok, err)
	}
	if err := WritePID("127.0.0.1:2270", os.Getpid()); err != nil {
		t.Fatal(err)
	}
	pid, ok, err := ReadPID("127.0.0.1:2270")
	if err != nil || !ok || pid != os.Getpid() {
		t.Fatalf("read = %d, %v, %v", pid, ok, err)
	}
	if !Alive(pid) {
		t.Fatalf("Alive(%d) = false", pid)
	}
	if err := RemovePID("127.0.0.1:2270"); err != nil {
		t.Fatal(err)
	}
	if err := RemovePID("127.0.0.1:2270"); err != nil {
		t.Fatalf("second remove = %v", err)
	}
}

func TestStopWithoutDaemon(t *testing.T) {
	t.Setenv("AGENT_SESH_STATE_DIR", t.TempDir())
	err := Stop(Options{Addr: "127.0.0.1:2270"})
	if err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("Stop error = %v", err)
	}
}
