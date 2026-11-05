package picker

import (
	"image/color"
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
)

// backgroundFromEnv pins the picker background from AGENT_SESH_BG_COLOR.
// Accepts "#1d1f21", "1d1f21", or a 0-255 ANSI index. A Nix or shell config can
// then match the terminal background exactly without a rebuild.
func backgroundFromEnv() color.Color {
	value := strings.TrimSpace(os.Getenv("AGENT_SESH_BG_COLOR"))
	if value == "" {
		return nil
	}
	if !strings.HasPrefix(value, "#") {
		if n, err := strconv.Atoi(value); err == nil && n >= 0 && n <= 255 {
			return lipgloss.ANSIColor(n)
		}
		value = "#" + value
	}
	if !validHex(value) {
		return nil
	}
	return lipgloss.Color(value)
}

func validHex(value string) bool {
	digits := strings.TrimPrefix(value, "#")
	switch len(digits) {
	case 3, 6, 8, 12:
	default:
		return false
	}
	return strings.IndexFunc(digits, func(r rune) bool {
		return !strings.ContainsRune("0123456789abcdefABCDEF", r)
	}) == -1
}
