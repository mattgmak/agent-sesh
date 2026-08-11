package picker

import (
	"charm.land/bubbles/v2/textinput"
	"charm.land/lipgloss/v2"
)

func configureTextInputStyles(styles *textinput.Styles) {
	transparent := lipgloss.NewStyle().UnsetBackground()
	styles.Focused.Text = transparent
	styles.Focused.Prompt = transparent.Foreground(lipgloss.ANSIColor(8))
	styles.Focused.Placeholder = transparent.Foreground(lipgloss.ANSIColor(8)).Faint(true)
	styles.Focused.Suggestion = transparent.Foreground(lipgloss.ANSIColor(8)).Faint(true)
	styles.Blurred.Text = transparent
	styles.Blurred.Prompt = transparent.Foreground(lipgloss.ANSIColor(8))
	styles.Blurred.Placeholder = transparent.Foreground(lipgloss.ANSIColor(8)).Faint(true)
	styles.Blurred.Suggestion = transparent.Foreground(lipgloss.ANSIColor(8)).Faint(true)
	styles.Cursor.Blink = false
}
