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
	discoveryRefreshInterval   = 30 * time.Second
	previewMinRefreshInterval  = 1200 * time.Millisecond
	previewLiveRefreshInterval = 2 * time.Second
)

// Nerd Font codepoints (MDI from bin/scripts/lib/i_md.sh, FA from i_fa.sh).
const (
	iconUnknown        = "\U000F0625" // 󰘥 help-circle-outline
	iconIdle           = "\U000F04B2" // 󰒲 (user choice)
	iconWorking        = "\U000F09D1" // 󰧑 brain
	iconToolCall       = "\U000F1322" // 󱌢 hammer-screwdriver (tool metadata)
	iconStatusToolCall = "\U000F0996" // 󰦖 progress-clock (tool_call status)
	iconHalted         = "\U000F0377" // 󰍷 (user choice)
	iconAwaitingInput  = "\uF128"     //  nf-fa-question
	iconFolder         = "\U000F0256" // 󰉖 folder-outline
	iconBranch         = "\U000F062C" // 󰘬 source-branch
	iconAgent          = "\U000F06A9" // 󰚩 robot
	iconModel          = "\U000F09D1" // 󰧑 brain
	iconSession        = "\U000F018D" // 󰆍 console
	iconPane           = "\U000F0BCC" // 󰯌 view-split-vertical
	iconPrompt         = "\U000F036A" // 󰍪 message-text-outline
	iconAttach         = "\U000F0339" // 󰌹 link-variant
	iconKillPane       = "\U000F0158" // 󰅘 close-box-outline
	iconKillSess       = "\U000F05E8" // 󰗨 delete-forever
	iconNewWin         = "\U000F05B1" // 󰖱 window-open
	iconFilter         = "\U000F0349" // 󰍉 magnify
	iconRename         = "\U000F0455" // 󰑕 rename-box
	iconQuit           = "\U000F0206" // 󰈆 exit-to-app
)

type tickMsg struct{}

type discoveryTickMsg struct{}

type previewRefreshMsg struct {
	seq int
	id  string
	rev string
}

type previewLiveTickMsg struct{}

type discoveryLoadedMsg struct {
	sessions []registry.Session
	err      error
}

type previewLoadedMsg struct {
	seq      int
	id       string
	revision string
	content  string
	err      error
}

type sessionsLoadedMsg struct {
	sessions []registry.Session
	err      error
}

type model struct {
	sessions          []registry.Session
	cursor            int
	selectedID        string
	filter            textinput.Model
	rename            textinput.Model
	mode              mode
	width             int
	height            int
	registry          string
	statusLine        string
	quitting          bool
	attach            bool
	previewContent    string
	previewErr        error
	previewPending    string
	previewName       string
	previewRevision   string
	previewSeq        int
	loading           bool
	registryMtime     time.Time
	sessionsRenderKey string
	previewLastFetch  time.Time
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
		sessions: sessions,
		filter:   filter,
		rename:   rename,
		registry: path,
		loading:  true,
	}
	if info, err := os.Stat(path); err == nil {
		m.registryMtime = info.ModTime()
	}
	m.reconcileCursor()
	(&m).syncSessionsRenderKey()

	profile := detectColorProfile()
	opts := []tea.ProgramOption{tea.WithColorProfile(profile)}
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
		schedulePreviewLiveRefresh(),
		m.loadSessionsAsync(),
		m.schedulePreview(),
	)
}

func schedulePreviewLiveRefresh() tea.Cmd {
	return tea.Tick(previewLiveRefreshInterval, func(time.Time) tea.Msg {
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

func (m model) reloadDiscoveryAsync() tea.Cmd {
	path := m.registry
	current := append([]registry.Session(nil), m.sessions...)
	return func() tea.Msg {
		sessions, err := reloadDiscovery(path, current)
		return discoveryLoadedMsg{sessions: sessions, err: err}
	}
}

func (m model) filteredSessions() []registry.Session {
	query := strings.TrimSpace(strings.ToLower(m.filter.Value()))
	if query == "" {
		return m.sessions
	}
	out := make([]registry.Session, 0, len(m.sessions))
	for _, session := range m.sessions {
		searchKey := strings.ToLower(strings.Join([]string{
			session.Title,
			session.LastPrompt,
			session.TmuxSession,
			session.TmuxWindow,
			session.TmuxPane,
			sessionPaneLabel(session),
			session.CWD,
			session.Branch,
			string(session.Status),
			session.ToolName,
			session.Agent,
			session.Model,
			session.TmuxTarget,
		}, " "))
		if strings.Contains(searchKey, query) {
			out = append(out, session)
		}
	}
	return out
}

func (m model) displaySessions() []registry.Session {
	items := m.filteredSessions()
	if len(items) == 0 {
		return items
	}
	return enrichSessionsVisible(items, m.cursor, visibleCount(layoutHeight(m.height)))
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
		return
	}

	if m.selectedID != "" {
		for i, session := range items {
			if session.ID == m.selectedID {
				m.cursor = i
				return
			}
		}
	}

	// Default to the bottom-most entry (highest priority: halted, awaiting_input)
	// when there is no prior selection to restore.
	if m.selectedID == "" {
		m.cursor = 0
	}
	if m.cursor >= len(items) {
		m.cursor = len(items) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	m.selectedID = items[m.cursor].ID
}

func (m model) splitActive() bool {
	return splitActive(layoutWidth(m.width), layoutHeight(m.height), len(m.filteredSessions()) > 0)
}

func (m *model) schedulePreview() tea.Cmd {
	return m.schedulePreviewOpts(false)
}

func (m *model) schedulePreviewOpts(immediate bool) tea.Cmd {
	if !m.splitActive() {
		m.previewSeq++
		m.previewName, m.previewPending, m.previewContent, m.previewErr, m.previewRevision = "", "", "", nil, ""
		return nil
	}

	session, ok := m.selected()
	if !ok {
		m.previewSeq++
		m.previewName, m.previewPending, m.previewContent, m.previewErr, m.previewRevision = "", "", "", nil, ""
		return nil
	}

	id := session.ID
	rev := previewRevision(session)
	if id == m.previewName && rev == m.previewRevision && m.previewPending == "" {
		return nil
	}
	if id == m.previewPending {
		return nil
	}

	target := strings.TrimSpace(session.TmuxTarget)
	if content, err, hit := getPreviewCache(target, rev); hit {
		profileNote("schedulePreview", "cache hit "+target)
		m.previewName = id
		m.previewRevision = rev
		m.previewContent = content
		m.previewErr = err
		m.previewPending = ""
		return nil
	}

	if content, _, hit, err := getPreviewCacheAny(target); hit {
		m.previewName = id
		m.previewContent = content
		m.previewErr = err
	}

	if !immediate && id == m.previewName && m.previewContent != "" {
		if elapsed := time.Since(m.previewLastFetch); elapsed < previewMinRefreshInterval {
			if m.previewPending == id {
				return nil
			}
			wait := previewMinRefreshInterval - elapsed
			m.previewSeq++
			seq := m.previewSeq
			m.previewPending = id
			return tea.Tick(wait, func(time.Time) tea.Msg {
				return previewRefreshMsg{seq: seq, id: id, rev: rev}
			})
		}
	}

	m.previewSeq++
	m.previewPending = id
	seq := m.previewSeq
	m.previewLastFetch = time.Now()
	return m.fetchPreview(seq, id, target, rev)
}

func (m *model) forcePreviewRefresh() tea.Cmd {
	if !m.splitActive() {
		return nil
	}
	session, ok := m.selected()
	if !ok {
		return nil
	}
	id := session.ID
	if m.previewPending == id {
		return nil
	}
	target := strings.TrimSpace(session.TmuxTarget)
	if target == "" {
		return nil
	}
	if time.Since(m.previewLastFetch) < previewLiveRefreshInterval {
		return nil
	}
	rev := previewRevision(session)
	m.previewSeq++
	m.previewPending = id
	seq := m.previewSeq
	m.previewLastFetch = time.Now()
	return m.fetchPreview(seq, id, target, rev)
}

func (m model) fetchPreview(seq int, id, target, revision string) tea.Cmd {
	return func() tea.Msg {
		defer profileStart("fetchPreview " + target)()
		if target == "" {
			return previewLoadedMsg{seq: seq, id: id, revision: revision, content: "", err: nil}
		}
		content, err := tmux.CapturePane(target, 0)
		setPreviewCache(target, revision, content, err)
		return previewLoadedMsg{seq: seq, id: id, revision: revision, content: content, err: err}
	}
}

func (m model) reload() model {
	defer profileStart("reload")()
	fresh, err := registry.Load(m.registry)
	if err != nil {
		m.statusLine = err.Error()
		return m
	}
	sanitized, _ := registry.Sanitize(fresh, tmux.RegistrySanitizeOptions(nil))
	next := refreshSessionsFromRegistry(m.sessions, sanitized)
	if !m.applySessionsIfChanged(next) {
		return m
	}
	m.reconcileCursor()
	return m
}

func (m model) reloadIfChanged() (model, bool) {
	info, err := os.Stat(m.registry)
	if err == nil && !m.registryMtime.IsZero() && info.ModTime().Equal(m.registryMtime) {
		return m, false
	}
	prevKey := m.sessionsRenderKey
	m = m.reload()
	if err == nil {
		m.registryMtime = info.ModTime()
	}
	return m, m.sessionsRenderKey != prevKey
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
		return m
	}
	// Cyclic scrolling: wrap around both ends instead of clamping.
	// (cursor+1 at the last item → 0; cursor-1 at 0 → last item)
	index = ((index % len(items)) + len(items)) % len(items)
	m.cursor = index
	m.selectedID = items[index].ID
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
		if msg.seq != m.previewSeq {
			return m, nil
		}
		if msg.id == m.previewName && msg.revision == m.previewRevision && msg.content == m.previewContent && msg.err == m.previewErr {
			m.previewPending = ""
			return m, nil
		}
		m.previewPending = ""
		m.previewName = msg.id
		m.previewRevision = msg.revision
		m.previewContent = msg.content
		m.previewErr = msg.err
		return m, nil

	case previewRefreshMsg:
		if msg.seq != m.previewSeq {
			return m, nil
		}
		session, ok := m.selected()
		if !ok || session.ID != msg.id {
			return m, nil
		}
		if previewRevision(session) != msg.rev {
			return m, nil
		}
		m.previewPending = ""
		return m, (&m).schedulePreviewOpts(true)

	case previewLiveTickMsg:
		cmd := schedulePreviewLiveRefresh()
		if !m.splitActive() {
			return m, cmd
		}
		session, ok := m.selected()
		if !ok {
			return m, cmd
		}
		switch session.Status {
		case registry.StatusWorking, registry.StatusToolCall:
			return m, tea.Batch(cmd, (&m).forcePreviewRefresh())
		default:
			return m, cmd
		}

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
		if msg.err != nil {
			m.statusLine = msg.err.Error()
			return m, scheduleDiscoveryRefresh()
		}
		if !m.applySessionsIfChanged(msg.sessions) {
			return m, scheduleDiscoveryRefresh()
		}
		m.reconcileCursor()
		return m, tea.Batch(scheduleDiscoveryRefresh(), m.schedulePreview())

	case discoveryTickMsg:
		return m, tea.Batch(m.reloadDiscoveryAsync(), scheduleDiscoveryRefresh())

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.syncInputWidth()
		m.reconcileCursor()
		return m, m.schedulePreview()

	case tickMsg:
		oldRev := ""
		if session, ok := m.selected(); ok {
			oldRev = previewRevision(session)
		}
		var changed bool
		m, changed = m.reloadIfChanged()
		if m.quitting {
			return m, tea.Quit
		}
		cmd := scheduleRegistryRefresh()
		if changed {
			if session, ok := m.selected(); ok && previewRevision(session) != oldRev {
				cmd = tea.Batch(cmd, m.schedulePreview())
			}
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
		case "up", "k":
			m = m.setCursor(m.cursor + 1)
			return m, m.schedulePreview()
		case "down", "j":
			m = m.setCursor(m.cursor - 1)
			return m, m.schedulePreview()
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
			m.previewName == "",
		)
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, preview)
	} else {
		body = strings.TrimSuffix(listBody, "\n") + "\n\n" + footer
	}

	return tea.NewView(strings.TrimSuffix(body, "\n"))
}
