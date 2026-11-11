package picker

import (
	"sync"
)

// frameInputs lists every model field View reads. A frame is reproducible from
// these alone, so an equal key means the cached string still renders the same
// screen. footer carries the rendered filter or rename line, which covers the
// textinput state the frame shows.
type frameInputs struct {
	width       int
	height      int
	cursor      int
	mode        mode
	sessionsKey string
	sessionsLen int
	footer      string
	preview     string
	previewTgt  string
	previewPane int
	previewErr  string
	loading     bool
	quitting    bool
	attach      bool
	bgSet       bool
	bgR         uint32
	bgG         uint32
	bgB         uint32
	bgA         uint32
}

// frameMemo holds one frame plus the inputs that produced it. One slot matches
// the bound the render memos use, so memory stays flat across a long session.
type frameMemo struct {
	mu      sync.Mutex
	inputs  frameInputs
	content string
	valid   bool
}

// frameCache memoises the composed frame. Most frames repeat: the preview tick,
// the registry tick and every keystroke that leaves the visible text alone all
// rebuild the same string, and the rebuild costs more than the model update.
var frameCache frameMemo

func (m *frameMemo) get(inputs frameInputs) (string, bool) {
	if m == nil {
		return "", false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.valid || m.inputs != inputs {
		return "", false
	}
	return m.content, true
}

func (m *frameMemo) put(inputs frameInputs, content string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inputs, m.content, m.valid = inputs, content, true
}

func (m *frameMemo) reset() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.inputs, m.content, m.valid = frameInputs{}, "", false
}

// frameInputsFor collects the key for the current state. The caller supplies
// footer because View renders it for the key and the frame alike.
func (m model) frameInputsFor(footer string) frameInputs {
	inputs := frameInputs{
		width:       m.width,
		height:      m.height,
		cursor:      m.cursor,
		mode:        m.mode,
		sessionsKey: m.sessionsRenderKey,
		sessionsLen: len(m.sessions),
		footer:      footer,
		preview:     m.previewContent,
		previewTgt:  m.previewTarget,
		previewPane: m.previewPaneWidth,
		loading:     m.loading,
		quitting:    m.quitting,
		attach:      m.attach,
	}
	if m.previewErr != nil {
		inputs.previewErr = m.previewErr.Error()
	}
	if m.bgColor != nil {
		inputs.bgSet = true
		inputs.bgR, inputs.bgG, inputs.bgB, inputs.bgA = m.bgColor.RGBA()
	}
	return inputs
}
