package picker

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

type metaChunk struct {
	icon  string
	text  string
	style lipgloss.Style
}

func ellipsis(text string, width int) string {
	if width < 1 {
		return "…"
	}
	if lipgloss.Width(text) <= width {
		return text
	}
	runes := []rune(text)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		if len(runes) == 1 {
			return "…"
		}
		runes = runes[:len(runes)-1]
	}
	if len(runes) == 0 {
		return "…"
	}
	return string(runes) + "…"
}

func shortCWD(path string) string {
	if path == "" {
		return ""
	}
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) <= 3 {
		return path
	}
	return "…/" + strings.Join(parts[len(parts)-3:], "/")
}

func renderStatusChunk(status registry.Status) string {
	return ensureResetSuffix(statusLabelStyle(status).Render(statusIcon(status) + " "))
}

func renderIconTextChunk(icon, text string, style lipgloss.Style, maxWidth int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if maxWidth < 1 {
		return ensureResetSuffix(style.Render(icon + " " + text))
	}

	iconPrefix := style.Render(icon + " ")
	iconWidth := lipgloss.Width(iconPrefix)
	if iconWidth >= maxWidth {
		return ensureResetSuffix(truncateANSI(iconPrefix, maxWidth))
	}

	textBudget := maxWidth - iconWidth
	text = ellipsis(text, textBudget)
	rendered := style.Render(icon + " " + text)
	if lipgloss.Width(rendered) > maxWidth {
		rendered = truncateANSI(rendered, maxWidth-1) + "…"
	}
	return ensureResetSuffix(rendered)
}

func buildMetaChunks(session registry.Session) []metaChunk {
	sessionName := strings.TrimSpace(session.TmuxSession)
	if sessionName == "" {
		sessionName = "?"
	}
	agent := strings.TrimSpace(session.Agent)
	if agent == "" {
		agent = "pi"
	}

	chunks := []metaChunk{
		{icon: iconSession, text: sessionName, style: sessionStyle},
	}
	if paneLabel := sessionPaneLabel(session); paneLabel != "" {
		chunks = append(chunks, metaChunk{icon: iconPane, text: paneLabel, style: paneStyle})
	}
	chunks = append(chunks, metaChunk{icon: iconAgent, text: agent, style: agentStyle})
	if branch := strings.TrimSpace(session.Branch); branch != "" {
		chunks = append(chunks, metaChunk{icon: iconBranch, text: branch, style: branchStyle})
	}
	if tool := strings.TrimSpace(session.ToolName); tool != "" {
		chunks = append(chunks, metaChunk{icon: iconToolCall, text: tool, style: toolStyle})
	}
	if cwd := shortCWD(strings.TrimSpace(session.CWD)); cwd != "" {
		chunks = append(chunks, metaChunk{icon: iconFolder, text: cwd, style: cwdStyle})
	}
	return chunks
}

func layoutMetaChunks(status registry.Status, chunks []metaChunk, width int) []string {
	if width < 1 {
		return nil
	}

	type piece struct {
		content  string
		width    int
		chunk    *metaChunk
		isStatus bool
	}

	pieces := make([]piece, 0, len(chunks)+1)
	statusContent := renderStatusChunk(status)
	if statusContent != "" {
		pieces = append(pieces, piece{content: statusContent, width: lipgloss.Width(statusContent), isStatus: true})
	}
	for i := range chunks {
		chunk := chunks[i]
		content := renderIconTextChunk(chunk.icon, chunk.text, chunk.style, 0)
		if content == "" {
			continue
		}
		pieces = append(pieces, piece{content: content, width: lipgloss.Width(content), chunk: &chunks[i]})
	}

	const sep = " "
	sepWidth := lipgloss.Width(sep)

	var lines []string
	var current []piece
	used := 0

	flush := func() {
		if len(current) == 0 {
			return
		}
		parts := make([]string, len(current))
		for i, p := range current {
			parts[i] = p.content
		}
		lines = append(lines, ensureResetSuffix(strings.Join(parts, sep)))
		current = nil
		used = 0
	}

	renderAtWidth := func(p piece, lineWidth int) string {
		if p.isStatus {
			return renderStatusChunk(status)
		}
		if p.chunk == nil {
			return truncateANSI(p.content, lineWidth)
		}
		return renderIconTextChunk(p.chunk.icon, p.chunk.text, p.chunk.style, lineWidth)
	}

	renderExclusive := func(p piece) string {
		content := renderAtWidth(p, width)
		if lipgloss.Width(content) > width {
			content = truncateANSI(content, width-1) + "…"
		}
		return ensureResetSuffix(content)
	}

	for _, p := range pieces {
		need := p.width
		if len(current) > 0 {
			need = used + sepWidth + p.width
		}

		if len(current) > 0 && need > width {
			// The chunk does not fit on the current line. If the line so far
			// only holds the status marker, keep it glued to the leading chunk
			// (the session name) and truncate that chunk to the remaining
			// budget. Truncating to the full width here would overflow the line
			// once the status icon and its space are prepended, so the terminal
			// wraps it.
			if len(current) == 1 && current[0].isStatus {
				if remaining := width - used - sepWidth; remaining >= 1 {
					content := renderAtWidth(p, remaining)
					if lipgloss.Width(content) > remaining {
						content = truncateANSI(content, remaining-1) + "…"
					}
					used += sepWidth
					current = append(current, piece{content: ensureResetSuffix(content), width: lipgloss.Width(content), chunk: p.chunk, isStatus: p.isStatus})
					used += lipgloss.Width(content)
					continue
				}
			}
			flush()
		}

		// Chunks wider than the line always get their own row at the full width budget.
		if p.width > width {
			lines = append(lines, renderExclusive(p))
			continue
		}

		if len(current) > 0 {
			used += sepWidth
		}
		current = append(current, piece{content: p.content, width: p.width, chunk: p.chunk, isStatus: p.isStatus})
		used += p.width
	}
	flush()

	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func formatPromptLine(session registry.Session, width int) string {
	prompt := strings.TrimSpace(session.LastPrompt)
	if prompt == "" {
		prompt = strings.TrimSpace(session.Title)
	}
	if prompt == "" {
		prompt = "(no prompt)"
	}
	// Prompts are free text and can contain line breaks. Collapse all
	// whitespace runs to a single space so the prompt always renders on one
	// line inside the entry block.
	prompt = strings.Join(strings.Fields(prompt), " ")
	return renderIconTextChunk(iconPrompt, prompt, promptStyle, width)
}

func formatSessionEntry(session registry.Session, width int) []string {
	meta := layoutMetaChunks(session.Status, buildMetaChunks(session), width)
	prompt := formatPromptLine(session, width)
	if prompt == "" {
		return meta
	}
	return append(meta, prompt)
}

func formatSessionLine(session registry.Session, width int) string {
	return strings.Join(formatSessionEntry(session, width), "\n")
}
