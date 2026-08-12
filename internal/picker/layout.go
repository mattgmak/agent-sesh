package picker

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

const (
	footerLines          = 2
	fallbackVisibleCount = 5
	maxListWidth         = 60
	minListWidth         = 40
	previewPadding       = 1
	previewWidthPct      = 40
	previewMinWidth      = 80
	listGutterWidth      = 1
	listEntryGap         = " "
)

func layoutWidth(width int) int {
	if width < 1 {
		return 1
	}
	return width
}

func layoutHeight(height int) int {
	if height < 1 {
		return 1
	}
	return height
}

func visibleCount(height int) int {
	available := height - footerLines
	if available < 1 {
		return fallbackVisibleCount
	}
	return available
}

func previewActive(width int) bool {
	return width >= previewMinWidth
}

func previewChrome() int {
	return previewPadding*2 + 2
}

func previewInnerWidth(cols int) int {
	w := cols - 2 - previewPadding*2
	if w < 1 {
		return 1
	}
	return w
}

func previewInnerHeight(rows int) int {
	h := rows - 2 - previewPadding*2
	if h < 1 {
		return 1
	}
	return h
}

func splitActive(width, height int, hasSessions bool) bool {
	return hasSessions && previewActive(width) && previewCols(width) > 0 && height >= footerLines+minListRows()
}

func minListRows() int {
	return 4
}

func previewCols(width int) int {
	if !previewActive(width) {
		return 0
	}
	cols := width * previewWidthPct / 100
	if rest := width - maxListWidth; rest > cols {
		cols = rest
	}
	if max := width - minListWidth; cols > max {
		cols = max
	}
	if cols <= previewChrome() {
		return 0
	}
	return cols
}

func contentWidth(totalWidth int) int {
	w := totalWidth - previewCols(totalWidth)
	if w < 30 {
		w = minListWidth
	}
	if w > maxListWidth {
		w = maxListWidth
	}
	return w
}

func listWindow(cursor, offset, itemCount, visible int) (newOffset, end int) {
	if itemCount == 0 {
		return 0, 0
	}
	if cursor >= itemCount {
		cursor = itemCount - 1
	}
	if cursor < offset {
		offset = cursor
	}
	if cursor >= offset+visible {
		offset = cursor - visible + 1
	}
	end = offset + visible
	if end > itemCount {
		end = itemCount
	}
	return offset, end
}

type listRenderOpts struct {
	showCursor bool
	emptyText  string
}

func truncateLine(line string, width int) string {
	return truncateANSI(line, width)
}

func renderListGutter(selected bool) string {
	if !selected {
		return gutterMutedStyle.Render(strings.Repeat("▌", listGutterWidth))
	}
	return gutterStyle.Render(strings.Repeat("▌", listGutterWidth))
}

func renderListFrame(
	items []registry.Session,
	cursor int,
	visible int,
	lineWidth int,
	opts listRenderOpts,
	renderEntry func(session registry.Session, width int) []string,
) string {
	if visible < 1 {
		return ""
	}

	if len(items) == 0 {
		empty := opts.emptyText
		if empty == "" {
			empty = formatEmptyListMessage(false)
		}
		return formatAnchoredBody(visible, strings.Split(empty, "\n"))
	}

	type entryBlock struct {
		itemIndex int
		lines     []string
	}

	blocks := make([]entryBlock, len(items))
	allRows := make([]string, 0, len(items)*3)
	rowStarts := make([]int, len(items))

	for i := len(items) - 1; i >= 0; i-- {
		item := items[i]
		selected := opts.showCursor && i == cursor

		gutterWidth := lipgloss.Width(renderListGutter(selected))
		gapWidth := lipgloss.Width(listEntryGap)
		bodyWidth := lineWidth - gutterWidth - gapWidth
		if bodyWidth < 1 {
			bodyWidth = lineWidth
		}

		entryLines := renderEntry(item, bodyWidth)
		styled := make([]string, 0, len(entryLines))
		for _, part := range entryLines {
			line := renderListGutter(selected) + listEntryGap + part
			styled = append(styled, line)
		}
		rowStarts[i] = len(allRows)
		blocks[i] = entryBlock{itemIndex: i, lines: styled}
		allRows = append(allRows, styled...)
		if i > 0 {
			allRows = append(allRows, "")
		}
	}

	if len(allRows) == 0 {
		return strings.Repeat("\n", visible-1)
	}

	cursorStart := rowStarts[cursor]
	cursorEnd := cursorStart + len(blocks[cursor].lines)

	start := len(allRows) - visible
	if start < 0 {
		start = 0
	}
	if cursorStart < start {
		start = cursorStart
	}
	if cursorEnd > start+visible {
		start = cursorEnd - visible
	}
	if start < 0 {
		start = 0
	}

	end := start + visible
	if end > len(allRows) {
		end = len(allRows)
	}
	window := allRows[start:end]

	padTop := visible - len(window)
	lines := make([]string, 0, visible)
	for i := 0; i < padTop; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, window...)
	for len(lines) < visible {
		lines = append(lines, "")
	}
	if len(lines) > visible {
		lines = lines[:visible]
	}
	return strings.Join(lines, "\n")
}

func clipLines(text string, width, rows int) string {
	if width < 1 || rows < 1 {
		return ""
	}

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) > rows {
		// Keep the tail (bottom) of the capture so the preview shows the
		// agent TUI's full interface — status bar, tool output, input —
		// from the bottom-most content upward.
		lines = lines[len(lines)-rows:]
	}

	for i, line := range lines {
		line = truncateANSI(line, width)
		lines[i] = ensureResetSuffix(line)
	}
	return strings.Join(lines, "\n")
}

func renderPreviewPane(content string, cols, rows int, previewErr error, loading bool) string {
	faint := faintStyle
	var body string
	switch {
	case previewErr != nil:
		body = faint.Render("Preview unavailable: " + previewErr.Error())
	case loading:
		body = faint.Render("Loading preview...")
	case strings.TrimSpace(content) == "":
		body = faint.Render("No preview")
	default:
		body = clipLines(content, previewInnerWidth(cols), previewInnerHeight(rows))
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.ANSIColor(8)).
		UnsetBackground().
		UnsetBorderBackground().
		Padding(previewPadding).
		Width(cols).
		Height(rows).
		Render(body)
}

func padFrame(content string, height int) string {
	if height < 1 {
		return content
	}
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}
	return strings.Join(lines, "\n")
}

func loadingLine(text string) string {
	return faintStyle.Render("  " + text)
}

func formatAnchoredBody(visible int, contentLines []string) string {
	if visible < 1 {
		return ""
	}
	if len(contentLines) > visible {
		contentLines = contentLines[len(contentLines)-visible:]
	}
	padTop := visible - len(contentLines)
	lines := make([]string, 0, visible)
	for i := 0; i < padTop; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, contentLines...)
	return strings.Join(lines, "\n")
}

func formatEmptyListMessage(noMatches bool) string {
	if noMatches {
		return faintStyle.Render("  No matching sessions") + "\n" + dimStyle.Render("  Try a different filter")
	}
	return dimStyle.Render("  "+iconSession+"  No sessions") + "\n" + dimStyle.Render("  Start an agent in tmux")
}

func formatLoadingBody(visible int, text string) string {
	return formatAnchoredBody(visible, []string{loadingLine(text)})
}
