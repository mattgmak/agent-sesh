package picker

import (
	"fmt"
	"os"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/mattgmak/agent-sesh/internal/registry"
	"github.com/mattgmak/agent-sesh/internal/tmux"
)

type mode int

const (
	modeNormal mode = iota
	modeRename
)

const (
	registryRefreshInterval    = 3 * time.Second
	registryReloadDebounce     = 200 * time.Millisecond
	discoveryRefreshInterval   = 30 * time.Second
	previewMinRefreshInterval  = 1200 * time.Millisecond
	previewNavigateDebounce    = 200 * time.Millisecond
	previewLiveRefreshInterval = 500 * time.Millisecond
	previewBusyRefreshInterval = 100 * time.Millisecond
)

// capturePane is a test seam for the tmux capture call.
var capturePane = tmux.CapturePane

// Nerd Font codepoints (MDI from bin/scripts/lib/i_md.sh, FA from i_fa.sh).
const (
	iconUnknown          = "\U000F0625" // 󰘥 help-circle-outline
	iconIdle             = "\U000F04B2" // 󰒲 (user choice)
	iconWorking          = "\U000F09D1" // 󰧑 brain
	iconToolCall         = "\U000F0493" // 󰒓 cog (tool metadata)
	iconStatusToolCall   = "\U000F0493" // 󰒓 cog (tool_call status)
	iconSubagentsRunning = "\U000F04AA" // 󰒪 sitemap
	iconBgJobsRunning    = "\U000F08D6" // 󰣖 cogs
	iconHalted           = "\U000F0377" // 󰍷 (user choice)
	iconAwaitingInput    = "\uF128"     //  nf-fa-question
	iconFolder           = "\U000F0256" // 󰉖 folder-outline
	iconBranch           = "\U000F062C" // 󰘬 source-branch
	iconAgent            = "\U000F06A9" // 󰚩 robot
	iconModel            = "\U000F09D1" // 󰧑 brain
	iconSession          = "\U000F018D" // 󰆍 console
	iconSessionName      = "\U0000F02E" //  bookmark
	iconPane             = "\U000F0BCC" // 󰯌 view-split-vertical
	iconPrompt           = "\U000F036A" // 󰍪 message-text-outline
	iconAttach           = "\U000F0339" // 󰌹 link-variant
	iconKillPane         = "\U000F0158" // 󰅘 close-box-outline
	iconKillSess         = "\U000F05E8" // 󰗨 delete-forever
	iconNewWin           = "\U000F05B1" // 󰖱 window-open
	iconFilter           = "\U000F0349" // 󰍉 magnify
	iconRename           = "\U000F0455" // 󰑕 rename-box
	iconQuit             = "\U000F0206" // 󰈆 exit-to-app
)

type tickMsg struct{}

type registryReloadMsg struct{}

type discoveryTickMsg struct{}

type previewRefreshMsg struct {
	seq    int
	id     string
	target string
}

type previewLiveTickMsg struct{}

type discoveryLoadedMsg struct {
	gen      int
	sessions []registry.Session
	err      error
}

type registryReloadedMsg struct {
	gen      int
	baseKey  string
	sessions []registry.Session
	err      error
}

type previewLoadedMsg struct {
	seq     int
	id      string
	target  string
	content string
	err     error
}

type sessionsLoadedMsg struct {
	sessions []registry.Session
	err      error
}

type previewScheduleMode int

const (
	previewScheduleNormal previewScheduleMode = iota
	previewScheduleNavigate
	previewScheduleImmediate
)

type model struct {
	sessions                []registry.Session
	cursor                  int
	selectedID              string
	selectedTarget          string
	filter                  textinput.Model
	rename                  textinput.Model
	mode                    mode
	width                   int
	height                  int
	registry                string
	statusLine              string
	quitting                bool
	attach                  bool
	previewContent          string
	previewErr              error
	previewPending          string
	previewName             string
	previewTarget           string
	previewKey              uint64
	previewSeq              int
	loading                 bool
	registryMtime           time.Time
	registryReloadPending   bool
	registryReloadInFlight  bool
	registryReloadGen       int
	discoveryReloadInFlight bool
	discoveryReloadGen      int
	sessionsRenderKey       string
	filterCache             *sessionFilterCache
	previewLastFetch        time.Time
}

func Run() error {
	initTerminalColors()

	if _, err := initProfile(); err != nil {
		return fmt.Errorf("init profile: %w", err)
	}
	if _, err := startCPUProfile(); err != nil {
		return fmt.Errorf("init cpu profile: %w", err)
	}
	defer func() {
		tmux.InvalidateSnapshot()
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

	sessions, err := loadSessionsFast(path)
	if err != nil {
		return err
	}

	filter := textinput.New()
	filter.Prompt = "> "
	filter.CharLimit = 120
	filterStyles := filter.Styles()
	configureTextInputStyles(&filterStyles)
	filter.SetStyles(filterStyles)
	filter.Focus()

	rename := textinput.New()
	rename.Prompt = iconRename + " "
	rename.CharLimit = 120
	renameStyles := rename.Styles()
	configureTextInputStyles(&renameStyles)
	rename.SetStyles(renameStyles)

	m := model{
		sessions:    sessions,
		filter:      filter,
		rename:      rename,
		registry:    path,
		loading:     true,
		filterCache: &sessionFilterCache{},
	}
	if info, err := os.Stat(path); err == nil {
		m.registryMtime = info.ModTime()
	}
	m.reconcileCursor()
	(&m).syncSessionsRenderKey()

	profile := detectColorProfile()
	opts := []tea.ProgramOption{tea.WithColorProfile(profile), tea.WithFPS(30)}
	if term := strings.TrimSpace(os.Getenv("TERM")); term != "" {
		opts = append(opts, tea.WithEnvironment(append(os.Environ(), "TERM="+term)))
	}

	// tmux display-popup -E already owns the pane; alt-screen fights it and bleeds
	// the underlying pi UI through the picker.
	if _, err := tea.NewProgram(m, opts...).Run(); err != nil {
		return err
	}
	return nil
}

func pruneAndPersist(path string, sessions []registry.Session) []registry.Session {
	return sanitizeAndPersist(path, sessions)
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		scheduleRegistryRefresh(),
		scheduleDiscoveryRefresh(),
		(&m).schedulePreviewLiveRefresh(),
		m.loadSessionsAsync(),
		m.schedulePreview(),
	)
}

// previewRefreshInterval picks the live poll cadence. A pending capture or a
// key burst gets the fast tick. A quiet picker gets the busy-pane tick.
func (m model) previewRefreshInterval() time.Duration {
	if m.previewPending != "" {
		return previewBusyRefreshInterval
	}
	return previewLiveRefreshInterval
}

func (m *model) schedulePreviewLiveRefresh() tea.Cmd {
	return tea.Tick(m.previewRefreshInterval(), func(time.Time) tea.Msg {
		return previewLiveTickMsg{}
	})
}

func (m model) loadSessionsAsync() tea.Cmd {
	path := m.registry
	return func() tea.Msg {
		sessions, err := loadSessionsFull(path)
		return sessionsLoadedMsg{sessions: sessions, err: err}
	}
}

func (m model) reloadRegistryAsync(gen int, baseKey string) tea.Cmd {
	path := m.registry
	current := append([]registry.Session(nil), m.sessions...)
	return func() tea.Msg {
		sessions, err := reloadRegistry(path, current)
		return registryReloadedMsg{
			gen:      gen,
			baseKey:  baseKey,
			sessions: sessions,
			err:      err,
		}
	}
}

func scheduleRegistryRefresh() tea.Cmd {
	return tea.Tick(registryRefreshInterval, func(t time.Time) tea.Msg {
		return tickMsg{}
	})
}

func scheduleDiscoveryRefresh() tea.Cmd {
	return tea.Tick(discoveryRefreshInterval, func(t time.Time) tea.Msg {
		return discoveryTickMsg{}
	})
}

func (m model) reloadDiscoveryAsync(gen int) tea.Cmd {
	path := m.registry
	current := append([]registry.Session(nil), m.sessions...)
	return func() tea.Msg {
		sessions, err := reloadDiscovery(path, current)
		return discoveryLoadedMsg{gen: gen, sessions: sessions, err: err}
	}
}

// sessionFilterCache memoises filter work for one registry revision: the
// lowercased search haystack per session plus the last filtered result.
type sessionFilterCache struct {
	renderKey string
	count     int
	haystacks []string
	query     string
	items     []registry.Session
}

func (c *sessionFilterCache) fresh(renderKey string, count int) bool {
	return c != nil && c.renderKey == renderKey && c.count == count
}

func sessionSearchText(session registry.Session) string {
	return strings.ToLower(strings.Join([]string{
		session.Title,
		session.PiSessionName,
		session.LastPrompt,
		session.TmuxSession,
		session.TmuxWindow,
		session.TmuxPane,
		sessionPaneLabel(session),
		session.CWD,
		session.Branch,
		string(session.Status),
		string(session.DisplayStatus()),
		session.ToolName,
		session.Agent,
		session.Model,
		session.TmuxTarget,
	}, " "))
}

func (m model) searchHaystacks(cache *sessionFilterCache) []string {
	haystacks := make([]string, len(m.sessions))
	for i, session := range m.sessions {
		haystacks[i] = sessionSearchText(session)
	}
	if cache != nil {
		cache.renderKey = m.sessionsRenderKey
		cache.count = len(m.sessions)
		cache.haystacks = haystacks
		cache.query = ""
		cache.items = nil
	}
	return haystacks
}

func (m model) filteredSessions() []registry.Session {
	query := strings.TrimSpace(strings.ToLower(m.filter.Value()))
	if query == "" {
		return m.sessions
	}
	cache := m.filterCache
	cached := cache.fresh(m.sessionsRenderKey, len(m.sessions))
	if cached && cache.query == query {
		return cache.items
	}
	var haystacks []string
	if cached {
		haystacks = cache.haystacks
	}
	if len(haystacks) != len(m.sessions) {
		haystacks = m.searchHaystacks(cache)
	}
	out := make([]registry.Session, 0, len(m.sessions))
	for i, session := range m.sessions {
		if strings.Contains(haystacks[i], query) {
			out = append(out, session)
		}
	}
	if cache != nil {
		cache.query = query
		cache.items = out
	}
	return out
}

func (m model) displaySessions() []registry.Session {
	return m.filteredSessions()
}

func (m model) selected() (registry.Session, bool) {
	items := m.filteredSessions()
	if len(items) == 0 || m.cursor >= len(items) {
		return registry.Session{}, false
	}
	return items[m.cursor], true
}

func (m *model) reconcileCursor() {
	items := m.filteredSessions()
	if len(items) == 0 {
		m.cursor = 0
		m.selectedID = ""
		m.selectedTarget = ""
		return
	}

	if m.selectedTarget != "" {
		for i, session := range items {
			if session.TmuxTarget == m.selectedTarget {
				m.cursor = i
				m.selectedID = session.ID
				return
			}
		}
	}

	if m.selectedID != "" {
		for i, session := range items {
			if session.ID == m.selectedID {
				m.cursor = i
				m.selectedTarget = session.TmuxTarget
				return
			}
		}
	}

	// Default to the bottom-most entry (highest priority: halted, awaiting_input)
	// when there is no prior selection to restore.
	if m.selectedID == "" && m.selectedTarget == "" {
		m.cursor = 0
	}
	if m.cursor >= len(items) {
		m.cursor = len(items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.selectedID = items[m.cursor].ID
	m.selectedTarget = items[m.cursor].TmuxTarget
}

func (m model) splitActive() bool {
	return splitActive(layoutWidth(m.width), layoutHeight(m.height), len(m.filteredSessions()) > 0)
}

func (m *model) schedulePreview() tea.Cmd {
	return m.schedulePreviewOpts(previewScheduleNormal)
}

func (m *model) schedulePreviewNavigate() tea.Cmd {
	return m.schedulePreviewOpts(previewScheduleNavigate)
}

func (m *model) schedulePreviewOpts(mode previewScheduleMode) tea.Cmd {
	if !m.splitActive() {
		m.resetPreview()
		return nil
	}
	session, ok := m.selected()
	if !ok {
		m.resetPreview()
		return nil
	}
	id := session.ID
	target := strings.TrimSpace(session.TmuxTarget)
	if target == "" {
		m.resetPreview()
		return nil
	}

	// A pane switch shows that pane's last capture at once. The capture after
	// it always runs: pane text changes while the agent status stays the same.
	switched := target != m.previewTarget
	if switched {
		m.adoptCachedPreview(id, target)
	}
	// One capture at a time. A pending capture drops the queued one, so a key
	// burst never starts a second tmux call.
	if m.previewPending != "" {
		return nil
	}

	switch {
	case switched && mode == previewScheduleNavigate && m.previewContent != "":
		return m.deferPreviewFetch(id, target)
	case switched:
		return m.startPreviewFetch(id, target)
	case mode == previewScheduleNavigate:
		return nil
	case mode == previewScheduleImmediate:
		return m.startPreviewFetch(id, target)
	case time.Since(m.previewLastFetch) < previewMinRefreshInterval:
		return m.deferPreviewFetch(id, target)
	default:
		return m.startPreviewFetch(id, target)
	}
}

// resetPreview clears the preview pane and drops the capture in flight.
func (m *model) resetPreview() {
	m.previewSeq++
	m.previewName, m.previewTarget, m.previewPending = "", "", ""
	m.previewContent, m.previewErr, m.previewKey = "", nil, 0
}

// adoptCachedPreview shows the stored capture of target and drops captures in
// flight for the previous pane.
func (m *model) adoptCachedPreview(id, target string) {
	content, key, ok := cachedPreview(target)
	if !ok {
		content, key = "", 0
	}
	m.previewSeq++
	m.previewName, m.previewTarget = id, target
	m.previewContent, m.previewErr, m.previewKey = content, nil, key
}

func (m *model) startPreviewFetch(id, target string) tea.Cmd {
	m.previewSeq++
	m.previewPending = target
	m.previewLastFetch = time.Now()
	seq := m.previewSeq
	return m.fetchPreview(seq, id, target)
}

// deferPreviewFetch waits out the navigate debounce, then captures once.
func (m *model) deferPreviewFetch(id, target string) tea.Cmd {
	m.previewSeq++
	m.previewPending = target
	seq := m.previewSeq
	profileNote("schedulePreview", "deferred "+target)
	return tea.Tick(previewNavigateDebounce, func(time.Time) tea.Msg {
		return previewRefreshMsg{seq: seq, id: id, target: target}
	})
}

func (m *model) forcePreviewRefresh() tea.Cmd {
	if !m.splitActive() || m.previewPending != "" {
		return nil
	}
	session, ok := m.selected()
	if !ok {
		return nil
	}
	target := strings.TrimSpace(session.TmuxTarget)
	if target == "" {
		return nil
	}
	if time.Since(m.previewLastFetch) < previewLiveRefreshInterval {
		return nil
	}
	return m.startPreviewFetch(session.ID, target)
}

func (m model) fetchPreview(seq int, id, target string) tea.Cmd {
	return func() tea.Msg {
		defer profileStart("fetchPreview " + target)()
		if target == "" {
			return previewLoadedMsg{seq: seq, id: id, target: target}
		}
		content, err := capturePane(target, 0)
		if err == nil {
			storePreviewCapture(target, content)
		}
		return previewLoadedMsg{seq: seq, id: id, target: target, content: content, err: err}
	}
}

func (m model) reload() model {
	next, err := reloadRegistry(m.registry, m.sessions)
	if err != nil {
		m.statusLine = err.Error()
		return m
	}
	return m.applyReload(next)
}

func (m model) reloadWithSanitize(sanitize func([]registry.Session) []registry.Session) model {
	fresh, err := registry.Load(m.registry)
	if err != nil {
		m.statusLine = err.Error()
		return m
	}
	return m.applyReload(refreshSessionsFromRegistry(m.sessions, sanitize(fresh), nil))
}

func (m model) applyReload(next []registry.Session) model {
	if !m.applySessionsIfChanged(next) {
		profileNote("reload", "noop")
		return m
	}
	m.reconcileCursor()
	return m
}

func (m model) registryFileChanged() bool {
	info, err := os.Stat(m.registry)
	if err != nil {
		return false
	}
	if m.registryMtime.IsZero() {
		return true
	}
	return info.ModTime().After(m.registryMtime)
}

func (m model) reloadFull() model {
	defer profileStart("reloadFull")()
	tmux.InvalidateSnapshot()
	loaded := pruneAndPersist(m.registry, m.sessions)
	merged := mergeSessionsIncremental(m.sessions, loaded)
	if !m.applySessionsIfChanged(merged) {
		return m
	}
	m.reconcileCursor()
	return m
}

func (m model) setCursor(index int) model {
	items := m.filteredSessions()
	if len(items) == 0 {
		m.cursor = 0
		m.selectedID = ""
		m.selectedTarget = ""
		return m
	}
	// Cyclic scrolling: wrap around both ends instead of clamping.
	// (cursor+1 at the last item → 0; cursor-1 at 0 → last item)
	index = ((index % len(items)) + len(items)) % len(items)
	m.cursor = index
	m.selectedID = items[index].ID
	m.selectedTarget = items[index].TmuxTarget
	return m
}

func (m model) syncInputWidth() {
	width := m.contentWidth() - 4
	if width < 8 {
		width = 8
	}
	m.filter.SetWidth(width)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case previewLoadedMsg:
		// Clear the pending slot for the captured pane even when the capture
		// is stale, else a dropped capture blocks every later one.
		if msg.target != "" && msg.target == m.previewPending {
			m.previewPending = ""
		}
		if msg.seq != m.previewSeq {
			if m.previewPending == "" {
				return m, (&m).schedulePreview()
			}
			return m, nil
		}
		session, ok := m.selected()
		if !ok || strings.TrimSpace(session.TmuxTarget) != msg.target {
			return m, nil
		}
		key := previewContentKey(msg.content)
		if msg.err == nil && msg.target == m.previewTarget && key == m.previewKey && msg.err == m.previewErr {
			// Same bytes: the cached render stays valid.
			return m, nil
		}
		m.previewName = msg.id
		m.previewTarget = msg.target
		m.previewContent = msg.content
		m.previewErr = msg.err
		m.previewKey = key
		return m, nil

	case previewRefreshMsg:
		if msg.target != "" && msg.target == m.previewPending {
			m.previewPending = ""
		}
		if msg.seq != m.previewSeq {
			return m, nil
		}
		session, ok := m.selected()
		if !ok || strings.TrimSpace(session.TmuxTarget) != msg.target {
			return m, (&m).schedulePreview()
		}
		return m, (&m).schedulePreviewOpts(previewScheduleImmediate)

	case registryReloadMsg:
		if m.registryReloadInFlight {
			return m, nil
		}
		m.registryReloadPending = true
		m.registryReloadInFlight = true
		m.registryReloadGen++
		gen := m.registryReloadGen
		baseKey := m.sessionsRenderKey
		return m, m.reloadRegistryAsync(gen, baseKey)

	case registryReloadedMsg:
		if msg.gen != m.registryReloadGen || !m.registryReloadInFlight {
			return m, nil
		}
		m.registryReloadInFlight = false
		m.registryReloadPending = false
		if m.quitting {
			return m, tea.Quit
		}
		if msg.baseKey != m.sessionsRenderKey {
			return m, nil
		}
		if msg.err != nil {
			m.statusLine = msg.err.Error()
			return m, nil
		}
		if info, err := os.Stat(m.registry); err == nil {
			m.registryMtime = info.ModTime()
		}
		prevKey := m.sessionsRenderKey
		if !m.applySessionsIfChanged(msg.sessions) {
			profileNote("reload", "noop")
			return m, nil
		}
		m.reconcileCursor()
		if m.sessionsRenderKey != prevKey {
			return m, m.schedulePreview()
		}
		return m, nil

	case previewLiveTickMsg:
		var refresh tea.Cmd
		if m.splitActive() {
			if session, ok := m.selected(); ok {
				switch session.DisplayStatus() {
				case registry.StatusWorking, registry.StatusToolCall, registry.StatusSubagentsRunning, registry.StatusBgJobsRunning:
					refresh = (&m).forcePreviewRefresh()
				}
			}
		}
		return m, tea.Batch((&m).schedulePreviewLiveRefresh(), refresh)

	case sessionsLoadedMsg:
		if msg.err != nil {
			m.statusLine = msg.err.Error()
			m.loading = false
			return m, nil
		}
		m.loading = false
		merged := mergeSessionsIncremental(m.sessions, msg.sessions)
		if !m.applySessionsIfChanged(merged) {
			return m, nil
		}
		m.reconcileCursor()
		return m, m.schedulePreview()

	case discoveryLoadedMsg:
		if msg.gen != m.discoveryReloadGen || !m.discoveryReloadInFlight {
			return m, nil
		}
		m.discoveryReloadInFlight = false
		if msg.err != nil {
			m.statusLine = msg.err.Error()
			return m, nil
		}
		if !m.applySessionsIfChanged(msg.sessions) {
			return m, nil
		}
		m.reconcileCursor()
		return m, m.schedulePreview()

	case discoveryTickMsg:
		nextTick := scheduleDiscoveryRefresh()
		if m.discoveryReloadInFlight {
			return m, nextTick
		}
		m.discoveryReloadInFlight = true
		m.discoveryReloadGen++
		gen := m.discoveryReloadGen
		return m, tea.Batch(nextTick, m.reloadDiscoveryAsync(gen))

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncInputWidth()
		m.reconcileCursor()
		return m, m.schedulePreview()

	case tickMsg:
		var pendingReload tea.Cmd
		if m.registryFileChanged() && !m.registryReloadPending {
			m.registryReloadPending = true
			pendingReload = tea.Tick(registryReloadDebounce, func(time.Time) tea.Msg {
				return registryReloadMsg{}
			})
		}
		cmd := scheduleRegistryRefresh()
		if pendingReload != nil {
			cmd = tea.Batch(cmd, pendingReload)
		}
		return m, cmd

	case tea.KeyPressMsg:
		if m.mode == modeRename {
			switch msg.String() {
			case "esc":
				m.mode = modeNormal
				m.rename.SetValue("")
				m.statusLine = ""
				m.filter.Focus()
				return m, nil
			case "enter":
				name := strings.TrimSpace(m.rename.Value())
				session, ok := m.selected()
				if !ok {
					m.mode = modeNormal
					m.filter.Focus()
					return m, nil
				}
				sessionName, err := tmux.SessionName(session.TmuxTarget)
				if err != nil {
					m.statusLine = err.Error()
				} else if name == "" {
					m.statusLine = "session name cannot be empty"
				} else if err := tmux.RenameSession(sessionName, name); err != nil {
					m.statusLine = err.Error()
				} else {
					m.statusLine = fmt.Sprintf("renamed session to %s", name)
					m = m.reload()
					if m.quitting {
						return m, tea.Quit
					}
				}
				m.mode = modeNormal
				m.rename.SetValue("")
				m.filter.Focus()
				return m, nil
			}
			var cmd tea.Cmd
			m.rename, cmd = m.rename.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "ctrl+c", "q":
			m.quitting = true
			return m, tea.Quit
		case "esc":
			if strings.TrimSpace(m.filter.Value()) != "" {
				m.filter.SetValue("")
				m.reconcileCursor()
				return m, m.schedulePreview()
			}
			m.quitting = true
			return m, tea.Quit
		case "up", "ctrl+p", "ctrl+k", "shift+tab":
			m = m.setCursor(m.cursor + 1)
			return m, m.schedulePreviewNavigate()
		case "down", "ctrl+n", "ctrl+j", "tab":
			m = m.setCursor(m.cursor - 1)
			return m, m.schedulePreviewNavigate()
		case "enter":
			if m.loading {
				return m, nil
			}
			session, ok := m.selected()
			if !ok {
				return m, nil
			}
			// Transition halted → idle on focus so the picker shows
			// "acknowledged" state when the user returns to the pane.
			if session.Status == registry.StatusHalted {
				for i := range m.sessions {
					if m.sessions[i].ID == session.ID {
						m.sessions[i].Status = registry.StatusIdle
						break
					}
				}
				if err := registry.Save(m.registry, m.sessions); err != nil {
					m.statusLine = err.Error()
				}
			}
			if err := tmux.SwitchClient(session.TmuxTarget); err != nil {
				m.statusLine = err.Error()
				return m, nil
			}
			m.attach = true
			m.quitting = true
			return m, tea.Quit
		case "ctrl+x":
			session, ok := m.selected()
			if !ok {
				return m, nil
			}
			if err := tmux.KillPane(session.TmuxTarget); err != nil {
				m.statusLine = err.Error()
				return m, nil
			}
			m = m.reloadFull()
			if m.quitting {
				return m, tea.Quit
			}
			return m, m.schedulePreview()
		case "ctrl+X":
			session, ok := m.selected()
			if !ok {
				return m, nil
			}
			name, err := tmux.SessionName(session.TmuxTarget)
			if err != nil {
				m.statusLine = err.Error()
				return m, nil
			}
			if err := tmux.KillSession(name); err != nil {
				m.statusLine = err.Error()
				return m, nil
			}
			m = m.reloadFull()
			if m.quitting {
				return m, tea.Quit
			}
			return m, m.schedulePreview()
		case "ctrl+t":
			session, ok := m.selected()
			if !ok {
				return m, nil
			}
			if err := tmux.NewWindowAtPanePath(session.TmuxTarget); err != nil {
				m.statusLine = err.Error()
				return m, nil
			}
			m.statusLine = "new window created"
			return m, nil
		case "ctrl+r":
			session, ok := m.selected()
			if !ok {
				return m, nil
			}
			currentName, err := tmux.SessionName(session.TmuxTarget)
			if err != nil {
				m.statusLine = err.Error()
				return m, nil
			}
			m.mode = modeRename
			m.rename.SetValue(currentName)
			m.rename.Focus()
			return m, nil
		}

		prevValue := m.filter.Value()
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		if m.filter.Value() != prevValue {
			m.reconcileCursor()
			return m, tea.Batch(cmd, m.schedulePreview())
		}
		return m, cmd
	}

	return m, nil
}

func statusIcon(status registry.Status) string {
	switch status {
	case registry.StatusWorking:
		return iconWorking
	case registry.StatusHalted:
		return iconHalted
	case registry.StatusAwaitingInput:
		return iconAwaitingInput
	case registry.StatusToolCall:
		return iconStatusToolCall
	case registry.StatusSubagentsRunning:
		return iconSubagentsRunning
	case registry.StatusBgJobsRunning:
		return iconBgJobsRunning
	case registry.StatusUnknown:
		return iconUnknown
	default:
		return iconIdle
	}
}

func (m model) footerView() string {
	switch m.mode {
	case modeRename:
		return "  " + m.rename.View()
	default:
		return "  " + m.filter.View()
	}
}

func (m model) contentWidth() int {
	if m.width < 1 {
		return maxListWidth
	}
	return contentWidth(layoutWidth(m.width))
}

func (m model) View() tea.View {
	defer profileStart("View")()
	if m.quitting && m.attach {
		return tea.NewView("")
	}
	if m.width == 0 || m.height == 0 {
		return tea.NewView("Loading...")
	}

	visible := visibleCount(layoutHeight(m.height))
	listWidth := m.contentWidth()
	items := m.displaySessions()
	filtered := strings.TrimSpace(m.filter.Value()) != ""

	var listBody string
	switch {
	case m.loading && len(m.sessions) == 0:
		listBody = formatLoadingBody(visible, "Loading sessions...")
	default:
		emptyText := ""
		if len(items) == 0 {
			emptyText = formatEmptyListMessage(len(m.sessions) > 0 && filtered)
		}
		listBody = renderListFrame(
			items,
			m.cursor,
			visible,
			listWidth,
			listRenderOpts{
				showCursor: len(items) > 0,
				emptyText:  emptyText,
			},
			formatSessionEntry,
		)
	}

	footer := m.footerView()
	cols := previewCols(layoutWidth(m.width))
	var body string
	if m.splitActive() && cols > 0 {
		left := lipgloss.NewStyle().
			Width(listWidth).
			Render(strings.TrimSuffix(listBody, "\n") + "\n\n" + footer)
		preview := renderPreviewPane(
			m.previewContent,
			cols,
			visible+footerLines,
			m.previewErr,
			m.previewTarget == "",
		)
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, preview)
	} else {
		body = strings.TrimSuffix(listBody, "\n") + "\n\n" + footer
	}

	return tea.NewView(strings.TrimSuffix(body, "\n"))
}
