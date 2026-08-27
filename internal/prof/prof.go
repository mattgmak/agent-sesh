// Package prof provides opt-in wall-clock timing and CPU profiling for
// agent-sesh, enabled via environment variables:
//
//	AGENT_SESH_PROFILE=1|/path/to/profile.log   timed op log + summary (appends across runs)
//	AGENT_SESH_CPUPROFILE=1|/path/to/cpu.prof    pprof CPU profile
//
// Both default to $XDG_STATE_HOME/agent-sesh (or ~/.local/state/agent-sesh)
// when the value is "1" or "true".
package prof

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"time"
)

type stat struct {
	count int
	total time.Duration
	max   time.Duration
}

var (
	mu     sync.Mutex
	writer io.Writer
	file   *os.File
	path   string
	stats  map[string]*stat

	cpuFile *os.File
	cpuPath string
)

// Init opens the profile log if AGENT_SESH_PROFILE is set and returns the log
// path (empty when profiling is disabled).
func Init() (string, error) {
	raw := strings.TrimSpace(os.Getenv("AGENT_SESH_PROFILE"))
	if raw == "" {
		return "", nil
	}

	p := raw
	if raw == "1" || strings.EqualFold(raw, "true") {
		state, err := defaultStateDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(state, "agent-sesh", "profile.log")
	}

	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}

	f, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return "", err
	}

	mu.Lock()
	file = f
	writer = f
	path = p
	stats = make(map[string]*stat)
	mu.Unlock()

	_, _ = fmt.Fprintf(f, "agent-sesh profile started %s pid=%d\n", time.Now().Format(time.RFC3339), os.Getpid())
	return p, nil
}

// Close flushes the summary, closes the log, and returns the log path.
func Close() string {
	mu.Lock()
	defer mu.Unlock()

	if file == nil {
		return ""
	}

	writeSummaryLocked()
	_, _ = fmt.Fprintf(file, "agent-sesh profile ended %s pid=%d\n", time.Now().Format(time.RFC3339), os.Getpid())
	p := path
	_ = file.Close()
	file = nil
	writer = nil
	stats = nil
	path = ""
	return p
}

// Start begins timing an operation. The returned func records the elapsed
// duration when called (intended for use with defer).
func Start(name string) func() {
	if writer == nil {
		return func() {}
	}
	start := time.Now()
	return func() {
		elapsed := time.Since(start)
		record(name, elapsed)
		write(name, elapsed.String())
	}
}

// Note writes a free-form profiling note.
func Note(name, detail string) {
	if writer == nil {
		return
	}
	write(name, detail)
}

// StartCPUProfile begins a pprof CPU profile if AGENT_SESH_CPUPROFILE is set
// and returns the profile path (empty when disabled).
func StartCPUProfile() (string, error) {
	raw := strings.TrimSpace(os.Getenv("AGENT_SESH_CPUPROFILE"))
	if raw == "" {
		return "", nil
	}

	p := raw
	if raw == "1" || strings.EqualFold(raw, "true") {
		state, err := defaultStateDir()
		if err != nil {
			return "", err
		}
		p = filepath.Join(state, "agent-sesh", "cpu.prof")
	}

	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return "", err
	}

	f, err := os.Create(p)
	if err != nil {
		return "", err
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		_ = f.Close()
		return "", err
	}

	mu.Lock()
	cpuFile = f
	cpuPath = p
	mu.Unlock()
	return p, nil
}

// StopCPUProfile stops the CPU profile and returns the profile path.
func StopCPUProfile() string {
	mu.Lock()
	defer mu.Unlock()

	if cpuFile == nil {
		return ""
	}

	pprof.StopCPUProfile()
	_ = cpuFile.Close()
	p := cpuPath
	cpuFile = nil
	cpuPath = ""
	return p
}

func defaultStateDir() (string, error) {
	if state := os.Getenv("XDG_STATE_HOME"); state != "" {
		return state, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state"), nil
}

func write(name, detail string) {
	mu.Lock()
	defer mu.Unlock()
	if writer == nil {
		return
	}
	_, _ = fmt.Fprintf(writer, "agent-sesh profile: %s %s\n", name, detail)
}

func record(name string, elapsed time.Duration) {
	mu.Lock()
	defer mu.Unlock()
	s, ok := stats[name]
	if !ok {
		s = &stat{}
		stats[name] = s
	}
	s.count++
	s.total += elapsed
	if elapsed > s.max {
		s.max = elapsed
	}
}

func writeSummaryLocked() {
	if writer == nil || len(stats) == 0 {
		return
	}

	names := make([]string, 0, len(stats))
	for name := range stats {
		names = append(names, name)
	}
	sort.Strings(names)

	_, _ = fmt.Fprintln(writer, "agent-sesh profile: summary")
	for _, name := range names {
		s := stats[name]
		avg := s.total / time.Duration(s.count)
		_, _ = fmt.Fprintf(writer, "agent-sesh profile: %-28s count=%4d total=%12s avg=%10s max=%10s\n",
			name, s.count, s.total.Round(time.Microsecond), avg.Round(time.Microsecond), s.max.Round(time.Microsecond))
	}
}
