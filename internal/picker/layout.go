package picker

import (
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/mattgmak/agent-sesh/internal/registry"
)

const (
	footerLines           = 2
	fallbackVisibleCount  = 5
	maxListWidth          = 60
	minListWidth          = 40
	previewSeparatorCol   = "│"
	previewSeparatorWidth = 1
	previewWidthPct       = 40
	previewMinWidth       = 80
	listGutterWidth       = 1
	listEntryGap          = " "
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

// previewChrome is the column cost of the preview: the separator column.
func previewChrome() int {
	return previewSeparatorWidth
}

func previewInnerWidth(cols int) int {
	w := cols - previewSeparatorWidth
	if w < 1 {
		return 1
	}
	return w
}

func previewInnerHeight(rows int) int {
	if rows < 1 {
		return 1
	}
	return rows
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

// previewColsFor sizes the preview column to the pane it shows. The column
// matches the pane when the terminal has room, so a captured row never wraps or
// truncates. A narrower terminal gives the preview its whole remaining width and
// truncates instead. paneWidth 0 keeps the width-only heuristic.
func previewColsFor(width, paneWidth int) int {
	cols := previewCols(width)
	if cols == 0 || paneWidth <= 0 {
		return cols
	}
	if want := paneWidth + previewChrome(); want < cols {
		cols = want
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

// renderItemRange picks a subset of items to format. Row scrolling still
// happens later; this bounds lipgloss work to roughly the visible row budget.
func renderItemRange(itemCount, cursor, visibleRows int) (lo, hi int) {
	if itemCount == 0 {
		return 0, 0
	}
	if itemCount <= visibleRows+4 {
		return 0, itemCount
	}
	lo, hi = listWindow(cursor, 0, itemCount, visibleRows+2)
	return lo, hi
}

type listRenderOpts struct {
	showCursor bool
	emptyText  string
	// scrollRail adds a one-column scrollbar at the right edge of the list.
	scrollRail bool
}

// Rail glyphs: the thumb marks the visible window, the track shows the rest.
const (
	railTrackGlyph = "│"
	railThumbGlyph = "█"
)

// railGeometry returns the thumb height and its first row for a list of total
// rows shown in a window of visible rows scrolled to top.
func railGeometry(total, visible, top int) (thumb, start int) {
	if visible < 1 {
		return 0, 0
	}
	if total <= visible {
		return visible, 0
	}
	if visible <= 1 {
		return 1, 0
	}
	thumb = visible * visible / total
	if thumb < 1 {
		thumb = 1
	}
	if thumb > visible {
		thumb = visible
	}
	track := visible - thumb
	if track > 0 {
		start = top * track / (total - visible)
		if start > track {
			start = track
		}
	}
	return thumb, start
}

// scrollRail renders the scrollbar column.
func scrollRail(total, visible, top int) []string {
	if visible < 1 {
		return nil
	}
	thumb, start := railGeometry(total, visible, top)
	rail := make([]string, visible)
	for i := range rail {
		if total <= visible || (i >= start && i < start+thumb) {
			rail[i] = railThumbStyle.Render(railThumbGlyph)
		} else {
			rail[i] = dimStyle.Render(railTrackGlyph)
		}
	}
	return rail
}

func truncateLine(line string, width int) string {
	return truncateANSI(line, width)
}

func renderListGutter(selected bool, status registry.Status) string {
	return gutterStyleFor(status, selected).Render(strings.Repeat("▌", listGutterWidth))
}

var listEntryGapWidth = lipgloss.Width(listEntryGap)

// previewBodyStyle renders preview text with no border and no padding. The
// separator column carries the visual break, so the preview keeps every column
// it owns for content.
var previewBodyStyle = lipgloss.NewStyle().UnsetBackground()

// previewSeparatorStyle is the vertical rule between the list and the preview.
var previewSeparatorStyle = lipgloss.NewStyle().UnsetBackground().
	Foreground(lipgloss.ANSIColor(8))

const (
	memoTagClip uint8 = iota
	memoTagPaneBody
	memoTagPaneLoading
	memoTagPaneError
)

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

	renderLo, renderHi := renderItemRange(len(items), cursor, visible)
	renderItems := items[renderLo:renderHi]
	localCursor := cursor - renderLo

	railWidth := 0
	if opts.scrollRail {
		railWidth = 1
	}

	blocks := make([]entryBlock, len(renderItems))
	allRows := make([]string, 0, len(renderItems)*3)
	rowStarts := make([]int, len(renderItems))

	for i := len(renderItems) - 1; i >= 0; i-- {
		item := renderItems[i]
		selected := opts.showCursor && i == localCursor

		gutter := renderListGutter(selected, item.DisplayStatus())
		gutterWidth := lipgloss.Width(gutter)
		bodyWidth := lineWidth - gutterWidth - listEntryGapWidth - railWidth
		if bodyWidth < 1 {
			bodyWidth = lineWidth
		}

		entryLines := renderEntry(item, bodyWidth)
		styled := make([]string, 0, len(entryLines))
		for _, part := range entryLines {
			line := gutter + listEntryGap + part
			styled = append(styled, line)
		}
		rowStarts[i] = len(allRows)
		blocks[i] = entryBlock{itemIndex: renderLo + i, lines: styled}
		allRows = append(allRows, styled...)
		if i > 0 {
			allRows = append(allRows, "")
		}
	}

	if len(allRows) == 0 {
		return strings.Repeat("\n", visible-1)
	}

	cursorStart := rowStarts[localCursor]
	cursorEnd := cursorStart + len(blocks[localCursor].lines)

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
	if opts.scrollRail && lineWidth > 1 {
		rail := scrollRail(len(allRows), visible, start)
		for i := range lines {
			pad := lineWidth - 1 - lipgloss.Width(lines[i])
			if pad < 0 {
				pad = 0
			}
			lines[i] += strings.Repeat(" ", pad) + rail[i]
		}
	}
	return strings.Join(lines, "\n")
}

func renderPreviewPane(content string, cols, rows int, previewErr error, loading bool) string {
	tag, aux := memoTagPaneBody, ""
	switch {
	case previewErr != nil:
		tag, aux = memoTagPaneError, previewErr.Error()
	case loading:
		tag = memoTagPaneLoading
	}

	dims := memoDims{width: cols, height: rows}
	key := memoKey(content, dims, tag, aux)
	if out, ok := paneMemo.get(content, aux, key, dims, tag); ok {
		return out
	}
	out := renderPreviewPaneUncached(content, cols, rows, previewErr, loading)
	paneMemo.put(content, aux, key, dims, tag, out)
	return out
}

func renderPreviewPaneUncached(content string, cols, rows int, previewErr error, loading bool) string {
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

	block := previewBodyStyle.
		Width(previewInnerWidth(cols)).
		Height(previewInnerHeight(rows)).
		Render(body)
	return withSeparator(block, previewInnerHeight(rows))
}

// withSeparator prefixes every line of block with the vertical rule.
func withSeparator(block string, rows int) string {
	sep := previewSeparatorStyle.Render(previewSeparatorCol)
	lines := strings.Split(strings.TrimSuffix(block, "\n"), "\n")
	if len(lines) < rows {
		for len(lines) < rows {
			lines = append(lines, "")
		}
	}
	for i, line := range lines {
		lines[i] = sep + line
	}
	return strings.Join(lines, "\n")
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
