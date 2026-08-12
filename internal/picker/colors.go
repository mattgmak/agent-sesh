package picker

import (
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/compat"
	"github.com/charmbracelet/colorprofile"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

var (
	matchStyle = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(1)).Bold(true)

	// Entry element styles — each meta chunk gets its own hue for quick scanning.
	sessionStyle = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(15)).Bold(true)
	paneStyle    = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(3))
	agentStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("135"))
	branchStyle  = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(14)).Bold(true)
	toolStyle    = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(5)).Bold(true)
	cwdStyle     = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(4)).Faint(true)
	promptStyle  = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(7)).Faint(true)

	normalStyle = lipgloss.NewStyle()
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(8)).Faint(true)
	mutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(8)).Faint(true)
	faintStyle  = lipgloss.NewStyle().Faint(true)
	borderStyle = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(8))
)

func initTerminalColors() {
	profile := detectColorProfile()
	compat.Profile = profile
	lipgloss.Writer.Profile = profile
}

func detectColorProfile() colorprofile.Profile {
	if os.Getenv("NO_COLOR") != "" {
		return colorprofile.Ascii
	}
	if forced, ok := colorProfileFromEnv(); ok {
		return forced
	}
	return colorprofile.Detect(os.Stdout, os.Environ())
}

func colorProfileFromEnv() (colorprofile.Profile, bool) {
	term := os.Getenv("TERM")
	colorTerm := strings.ToLower(os.Getenv("COLORTERM"))
	switch {
	case colorTerm == "truecolor" || colorTerm == "24bit":
		return colorprofile.TrueColor, true
	case strings.Contains(term, "256color"):
		return colorprofile.ANSI256, true
	case strings.Contains(term, "color"), strings.Contains(term, "ansi"):
		return colorprofile.ANSI, true
	default:
		return colorprofile.Ascii, false
	}
}

func gutterStyleFor(status registry.Status, selected bool) lipgloss.Style {
	s := statusLabelStyle(status)
	if selected {
		return s.Background(lipgloss.ANSIColor(15)).UnsetFaint()
	}
	return s.Faint(true)
}

func statusLabelStyle(status registry.Status) lipgloss.Style {
	base := lipgloss.NewStyle().UnsetBackground()
	switch status {
	case registry.StatusWorking:
		return base.Foreground(lipgloss.ANSIColor(2)).Bold(true)
	case registry.StatusToolCall:
		return base.Foreground(lipgloss.ANSIColor(2)).Bold(true)
	case registry.StatusHalted:
		return base.Foreground(lipgloss.ANSIColor(1)).Bold(true)
	case registry.StatusAwaitingInput:
		return base.Foreground(lipgloss.Color("220")).Bold(true)
	default:
		return base.Foreground(lipgloss.ANSIColor(8))
	}
}
