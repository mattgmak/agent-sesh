// Package daemon serves the agent-sesh session cache over HTTP and SSE.
//
// One daemon runs per tmux server. It watches sessions.json, keeps the parsed
// rows in memory, and pushes changes to any number of clients over
// /v1/events. The picker, the status line, and remote clients then read one
// cache instead of shelling out to tmux on every refresh.
package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mattgmak/agent-sesh/internal/config"
	"github.com/mattgmak/agent-sesh/internal/counts"
	"github.com/mattgmak/agent-sesh/internal/notify"
	"github.com/mattgmak/agent-sesh/internal/picker"
	"github.com/mattgmak/agent-sesh/internal/registry"
	"github.com/mattgmak/agent-sesh/internal/version"
)

// DefaultHost is the interface the daemon binds to. It stays on loopback so a
// remote machine reaches it through an SSH tunnel, never on the network.
const DefaultHost = "127.0.0.1"

// heartbeat is the SSE comment interval that keeps idle connections alive.
const heartbeat = 15 * time.Second

// Options configures one daemon run.
type Options struct {
	// Addr is the TCP listen address, for example 127.0.0.1:2270.
	Addr string
	// Registry is the sessions.json path. Empty uses registry.DefaultPath().
	Registry string
	// NotifyOn raises notifications for the configured status changes.
	NotifyOn bool
	// Notify configures the notification channel used when NotifyOn is set.
	Notify notify.Options
	// PollInterval bounds how often sessions.json is re-read. Default one second.
	PollInterval time.Duration
	// LogTo receives daemon warnings. Defaults to os.Stderr.
	LogTo io.Writer
}

// Update is the SSE payload: the full session list plus the ids that changed.
type Update struct {
	At       int64              `json:"at"`
	Changed  []string           `json:"changed"`
	Sessions []registry.Session `json:"sessions"`
}

// hub fans one frame out to every subscriber. It keeps only the newest frame,
// so a slow client resynchronises instead of blocking the watch loop.
type hub struct {
	mu    sync.Mutex
	frame string
	subs  map[chan struct{}]struct{}
}

func newHub() *hub {
	return &hub{subs: make(map[chan struct{}]struct{})}
}

// subscribe returns a signal channel and its cancel function.
func (h *hub) subscribe() (<-chan struct{}, func()) {
	ch := make(chan struct{}, 1)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		h.mu.Unlock()
		close(ch)
	}
}

// publish replaces the current frame and wakes every subscriber.
func (h *hub) publish(frame string) {
	h.mu.Lock()
	h.frame = frame
	for ch := range h.subs {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	h.mu.Unlock()
}

func (h *hub) current() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.frame
}

// Server answers /v1 requests from the cached registry snapshot.
type Server struct {
	opts     Options
	cfg      config.Config
	mu       sync.Mutex
	sessions []registry.Session
	statuses map[string]registry.Status
	mtime    time.Time
	hub      *hub
}

// New loads the registry once and returns a ready server.
func New(opts Options) (*Server, error) {
	path, err := opts.registryPath()
	if err != nil {
		return nil, err
	}
	sessions, err := registry.Load(path)
	if err != nil {
		return nil, fmt.Errorf("load registry %s: %w", path, err)
	}
	s := &Server{
		opts:     opts.withDefaults(path),
		cfg:      config.Load(),
		sessions: sessions,
		hub:      newHub(),
	}
	s.indexLocked(sessions)
	s.publishLocked(time.Now(), nil)
	return s, nil
}

// Handler builds the HTTP routes.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/health", s.handleHealth)
	mux.HandleFunc("/v1/sessions", s.handleSessions)
	mux.HandleFunc("/v1/counts", s.handleCounts)
	mux.HandleFunc("/v1/preview", s.handlePreview)
	mux.HandleFunc("/v1/send", s.handleSend)
	mux.HandleFunc("/v1/events", s.handleEvents)
	mux.HandleFunc("/", s.handleIndex)
	return mux
}

// Serve listens until ctx ends, then closes the server and removes the pid file.
func Serve(ctx context.Context, opts Options) error {
	server, err := New(opts)
	if err != nil {
		return err
	}
	listener, err := net.Listen("tcp", server.opts.Addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", server.opts.Addr, err)
	}
	if err := WritePID(server.opts.Addr, os.Getpid()); err != nil {
		server.logf("pid file: %v", err)
	}
	httpServer := &http.Server{Handler: server.Handler(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		_ = httpServer.Close()
	}()
	go server.watch(ctx)
	err = httpServer.Serve(listener)
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	if removeErr := RemovePID(server.opts.Addr); removeErr != nil {
		server.logf("remove pid file: %v", removeErr)
	}
	return err
}

// Start launches `agent-sesh daemon serve` in the background and returns once
// the child answers /v1/health.
func Start(opts Options) error {
	if pid, ok, err := ReadPID(opts.Addr); err == nil && ok && Alive(pid) {
		return fmt.Errorf("daemon already running: pid %d", pid)
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	logPath, err := LogPath(opts.Addr)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	args := []string{"daemon", "serve", "--addr", opts.Addr}
	if opts.Registry != "" {
		args = append(args, "--registry", opts.Registry)
	}
	if opts.NotifyOn {
		args = append(args, "--notify")
	}
	cmd := exec.Command(self, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	Detach(cmd)
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	pid := cmd.Process.Pid
	if err := WritePID(opts.Addr, pid); err != nil {
		_ = logFile.Close()
		return err
	}
	_ = logFile.Close()
	return waitHealthy(opts.Addr, pid)
}

// waitHealthy polls /v1/health until the child answers or exits.
func waitHealthy(addr string, pid int) error {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := Get(addr, "/v1/health"); err == nil {
			return nil
		}
		if !Alive(pid) {
			return fmt.Errorf("daemon pid %d exited during startup", pid)
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("daemon pid %d did not answer /v1/health", pid)
}

// Stop asks the daemon to exit and removes its pid file.
func Stop(opts Options) error {
	pid, ok, err := ReadPID(opts.Addr)
	if err != nil {
		return err
	}
	if !ok || !Alive(pid) {
		_ = RemovePID(opts.Addr)
		return fmt.Errorf("daemon is not running")
	}
	if err := Terminate(pid); err != nil {
		return err
	}
	for start := time.Now(); time.Since(start) < 5*time.Second; {
		if !Alive(pid) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	return RemovePID(opts.Addr)
}

// Status writes the daemon health payload as JSON.
func Status(w io.Writer, opts Options) error {
	pid, ok, err := ReadPID(opts.Addr)
	if err != nil {
		return err
	}
	if !ok || !Alive(pid) {
		if ok {
			_ = RemovePID(opts.Addr)
		}
		return fmt.Errorf("daemon is not running")
	}
	body, err := Get(opts.Addr, "/v1/health")
	if err != nil {
		return err
	}
	_, err = w.Write(append(body, '\n'))
	return err
}

// Get fetches one daemon path and returns the response body.
func Get(addr, path string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + addr + path)
	if err != nil {
		return nil, fmt.Errorf("GET http://%s%s: %w", addr, path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return body, fmt.Errorf("GET %s: %s: %s", path, resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

// registryPath resolves the sessions.json path for these options.
func (o Options) registryPath() (string, error) {
	if p := strings.TrimSpace(o.Registry); p != "" {
		return p, nil
	}
	return registry.DefaultPath()
}

// withDefaults fills the fields Serve and the handlers need.
func (o Options) withDefaults(path string) Options {
	if o.Addr == "" {
		o.Addr = net.JoinHostPort(DefaultHost, strconv.Itoa(config.Load().Port()))
	}
	if o.PollInterval <= 0 {
		o.PollInterval = time.Second
	}
	if o.LogTo == nil {
		o.LogTo = os.Stderr
	}
	o.Registry = path
	return o
}

func (s *Server) logf(format string, args ...any) {
	fmt.Fprintf(s.opts.LogTo, "agent-sesh daemon: "+format+"\n", args...)
}

// Refresh re-reads the registry when its mtime moved and publishes one frame.
// It reports whether the snapshot changed.
func (s *Server) Refresh() (bool, error) {
	info, err := os.Stat(s.opts.Registry)
	if err != nil {
		return false, err
	}

	s.mu.Lock()
	if !info.ModTime().After(s.mtime) && s.mtime != (time.Time{}) {
		s.mu.Unlock()
		return false, nil
	}
	s.mtime = info.ModTime()
	sessions, err := registry.Load(s.opts.Registry)
	if err != nil {
		s.mu.Unlock()
		return false, err
	}
	changed := s.indexLocked(sessions)
	s.publishLocked(time.Now(), changed)
	s.mu.Unlock()

	for _, id := range changed {
		s.notifyStatus(id)
	}
	return true, nil
}

// indexLocked stores the new rows and returns the ids whose status changed.
// The caller holds s.mu.
func (s *Server) indexLocked(sessions []registry.Session) []string {
	previous := s.statuses
	statuses := make(map[string]registry.Status, len(sessions))
	for _, session := range sessions {
		statuses[session.ID] = session.DisplayStatus()
	}
	s.sessions = sessions
	s.statuses = statuses

	if previous == nil {
		return nil
	}
	var changed []string
	for id, status := range statuses {
		if old, ok := previous[id]; ok && old == status {
			continue
		}
		changed = append(changed, id)
	}
	for id := range previous {
		if _, ok := statuses[id]; !ok {
			changed = append(changed, id)
		}
	}
	sort.Strings(changed)
	return changed
}

// publishLocked sends the current snapshot as one SSE frame. The caller holds s.mu.
func (s *Server) publishLocked(at time.Time, changed []string) {
	sessions := s.sessions
	if sessions == nil {
		sessions = []registry.Session{}
	}
	data, err := json.Marshal(Update{
		At:       at.Unix(),
		Changed:  changed,
		Sessions: sessions,
	})
	if err != nil {
		s.logf("encode update: %v", err)
		return
	}
	s.hub.publish("event: sessions\ndata: " + string(data) + "\n\n")
}

// sessionByID finds one cached row by id.
func (s *Server) sessionByID(id string) (registry.Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, candidate := range s.sessions {
		if candidate.ID == id {
			return candidate, true
		}
	}
	return registry.Session{}, false
}

// notifyStatus sends one notification when the new status is on the notify list.
func (s *Server) notifyStatus(id string) {
	if !s.opts.NotifyOn {
		return
	}
	session, ok := s.sessionByID(id)
	if !ok {
		return
	}
	status := string(session.DisplayStatus())
	if !contains(s.cfg.EventList(), status) {
		return
	}
	event := notify.Event{
		Event:     status,
		SessionID: session.ID,
		Target:    session.TmuxTarget,
		Agent:     session.Agent,
		Project:   session.CWD,
		Branch:    session.Branch,
		Title:     session.Title,
	}
	if err := notify.Send(event, s.opts.Notify); err != nil {
		s.logf("notify %s: %v", id, err)
	}
}

// watch polls the registry until ctx ends.
func (s *Server) watch(ctx context.Context) {
	ticker := time.NewTicker(s.opts.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := s.Refresh(); err != nil {
				s.logf("refresh: %v", err)
				return
			}
		}
	}
}

// Sessions returns the cached rows.
func (s *Server) Sessions() []registry.Session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]registry.Session(nil), s.sessions...)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]any{
		"service": "agent-sesh",
		"routes": []string{
			"/v1/health", "/v1/sessions", "/v1/counts", "/v1/preview", "/v1/send", "/v1/events",
		},
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var vb bytes.Buffer
	_ = version.Write(&vb)
	writeJSON(w, map[string]any{
		"status":   "ok",
		"pid":      os.Getpid(),
		"addr":     s.opts.Addr,
		"version":  strings.TrimSpace(vb.String()),
		"sessions": len(s.Sessions()),
	})
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := picker.WriteSessionsJSON(w, s.Sessions()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleCounts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var buf bytes.Buffer
	if err := counts.Run(&buf, counts.Options{JSON: true}); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	target := r.URL.Query().Get("target")
	if target == "" {
		http.Error(w, "target is required", http.StatusBadRequest)
		return
	}
	var buf bytes.Buffer
	if err := picker.Preview(&buf, target); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write(buf.Bytes())
}

// SendRequest is the POST /v1/send body.
type SendRequest struct {
	Key   string `json:"key"`
	Text  string `json:"text"`
	Enter bool   `json:"enter"`
}

func (s *Server) handleSend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req SendRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	var buf bytes.Buffer
	if err := picker.Send(&buf, req.Key, req.Text, req.Enter); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSON(w, map[string]any{"ok": true, "detail": strings.TrimSpace(buf.String())})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	signal, unsubscribe := s.hub.subscribe()
	defer unsubscribe()
	if frame := s.hub.current(); frame != "" {
		_, _ = io.WriteString(w, frame)
		flusher.Flush()
	}

	ticker := time.NewTicker(heartbeat)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-signal:
		case <-ticker.C:
			if _, err := io.WriteString(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
			continue
		}
		frame := s.hub.current()
		if frame == "" {
			continue
		}
		if _, err := io.WriteString(w, frame); err != nil {
			return
		}
		flusher.Flush()
	}
}

func writeJSON(w http.ResponseWriter, value any) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_, _ = w.Write(append(data, '\n'))
}

// contains reports whether want is in list.
func contains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}
