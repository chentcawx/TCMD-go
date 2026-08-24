package tui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"tcmd/internal/fs"
)

var (
	pathStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	dirStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("39"))
	selStyle        = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	cursorStyle     = lipgloss.NewStyle().Reverse(true)
	statusStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	tabActiveStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231")).Background(lipgloss.Color("63"))
	tabInactiveStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	titleStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("63"))
	borderStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	activePathStyle = lipgloss.NewStyle().Reverse(true)
)

// View renders either the full dual-pane UI or the active modal overlay.
func (m *model) View() string {
	switch m.ov {
	case overlayConfirm:
		return m.renderConfirm()
	case overlayInput:
		return m.renderInput()
	case overlayViewer:
		return m.renderViewer()
	case overlayBatchRename:
		return m.renderBatchRename()
	case overlayContextMenu:
		return m.renderContextMenu()
	case overlayDrivePicker:
		return m.renderDrivePicker()
	case overlayTree:
		return m.renderTreeView()
	case overlayQueue:
		return m.renderQueueOverlay()
	case overlayAssoc:
		return m.renderAssocEditor()
	}

	w := m.width
	if w < 40 {
		w = 40
	}
	h := m.height
	if h < 10 {
		h = 10
	}
	// Reserve one trailing blank row of slack below the bottom status bar.
	//
	// Why: ConPTY / Windows Terminal report a content height 1-2 rows LARGER
	// than the real drawable area when the window is NOT maximized (the shell
	// reports the full outer size including its border/padding). If View()
	// emits exactly h rows, the extra rows overflow the real drawable area;
	// the terminal scrolls and pushes row 0 — the tab bar — out of the visible
	// viewport, so the tab bar "disappears" in non-maximized windows only.
	// Emitting h-1 rows keeps the tab bar pinned at row 0 regardless of that
	// reporting gap. (Maximized windows report accurately, which is why the
	// bug only surfaces when un-maximized.)
	//
	// Layout budget (rows): paneH = h-2 (tab + path + list, right-padded) + 1
	// bottom status row = h-1 total emitted rows. The remaining 1 row is the
	// slack that absorbs the ConPTY over-report.
	paneH := h - 2
	if paneH < 3 {
		paneH = 3
	}
	// The vertical separator "│" and every other glyph are measured with
	// runewidth — the SAME conservative ruler used everywhere else in this
	// file. Ambiguous-width glyphs (em dash '—', '│', full-width punctuation)
	// are counted as 2 cells by runewidth, which matches what East-Asian
	// terminals (e.g. Windows Terminal on a CJK locale) actually paint. Using
	// lipgloss.Width here would count them as 1, under-budget the row, and let
	// the terminal wrap the overflow onto a phantom extra line. Every width
	// measurement in the layout (truncateDW, padRightDW, clampRowWidth, sepW)
	// MUST use runewidth so the budget matches the painted width.
	sep := borderStyle.Render("│")
	sepW := runewidth.RuneWidth('│')
	if sepW < 1 {
		sepW = 1
	}
	half := (w - sepW) / 2
	left := m.renderPane(m.panes[0], m.active == 0, half, paneH)
	right := m.renderPane(m.panes[1], m.active == 1, w-half-sepW, paneH)
	panes := lipgloss.JoinHorizontal(lipgloss.Top, left, sep, right)
	bottom := m.renderBottom(w)
	return clampRowWidth(lipgloss.JoinVertical(lipgloss.Left, panes, bottom), w)
}

// clampRowWidth truncates every line in s to at most max cells (display width).
//
// Why: terminal emulators report a content width via tea.WindowSizeMsg that
// is sometimes one or two cells wider than the actual drawable area (a known
// ConPTY / Windows Terminal quirk when the window is not maximized, where the
// shell reports the full outer size including borders). Without this final
// guard, the View() output can be wider than the terminal's drawable area; the
// terminal then wraps the overflow onto the next line, smearing the right
// pane's tabs onto the left pane's row and producing the "right tab covers
// left tab" symptom that only shows up in non-maximized windows.
//
// Display width MUST be measured with runewidth.StringWidth — the SAME ruler
// truncateDW and padRightDW use. On East-Asian terminals ambiguous glyphs
// ('—', '│', full-width punctuation) paint as 2 cells; runewidth counts them
// as 2, so the truncation budget matches what the terminal actually draws and
// no phantom wrap line appears. (lipgloss.Width would count them as 1 and
// under-budget the row, which is exactly what produced the "extra line under
// an em-dash filename" regression.)
func clampRowWidth(s string, max int) string {
	if max <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if w := runewidth.StringWidth(l); w > max {
			lines[i] = truncateDW(l, max)
		}
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderPane(p *pane, active bool, w, paneH int) string {
	// Pad the tab and path rows to the full pane width so every row in the
	// pane block is exactly w cells wide. Without this, a short path/tab row
	// would let the vertical separator drift to a different column on that
	// row; padding keeps the separator pinned at a constant column and, for
	// the active pane, makes the reverse-video path bar span the whole pane.
	tabs := padRightDW(m.renderTabs(p, active, w), w)
	pathRaw := padRightDW(truncateDW(p.current().path, w), w)
	var pathStr string
	if active {
		pathStr = activePathStyle.Render(pathRaw)
	} else {
		pathStr = pathStyle.Render(pathRaw)
	}
	// paneH is the height of the pane block (tab + path + list), passed by
	// View() as h-2 so the overall View() output is h-1 rows — the trailing
	// row of slack absorbs ConPTY's non-maximized height over-report and keeps
	// the tab bar (row 0) on screen. Without a fixed height the panes would
	// shrink to whatever the entries list happens to render (often 3-4 lines
	// when a tab is still loading), leaving a blank gap and letting the
	// separator fall short of the bottom edge.
	paneHeight := paneH
	if paneHeight < 3 {
		paneHeight = 3
	}
	list := m.renderList(p.current(), w, paneHeight, active)
	joined := lipgloss.JoinVertical(lipgloss.Left, tabs, pathStr, list)
	// Pad the joined pane block to the full pane height so both panes are
	// exactly the same size before the bottom status row — that is what
	// makes the status bar actually sit at the very bottom and not float.
	return padRowsToHeight(joined, paneHeight, w)
}

// padRowsToHeight right-pads each existing line in s to width w, then
// appends blank rows until the total line count reaches h. Used by
// renderPane to keep both panes exactly the same height so the bottom
// status row always anchors at row m.height-1.
func padRowsToHeight(s string, h, w int) string {
	if h <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	blank := strings.Repeat(" ", w)
	for len(lines) < h {
		lines = append(lines, blank)
	}
	return strings.Join(lines, "\n")
}

func (m *model) renderTabs(p *pane, active bool, w int) string {
	var parts []string
	for i, t := range p.tabs {
		name := filepath.Base(t.path)
		if name == "" {
			name = t.path
		}
		label := " " + name + " "
		if i == p.active {
			parts = append(parts, tabActiveStyle.Render(label))
		} else {
			parts = append(parts, tabInactiveStyle.Render(label))
		}
	}
	return truncateDW(strings.Join(parts, " "), w)
}

func (m *model) renderList(t *tab, w, h int, active bool) string {
	// h is the height of the whole *pane block* (tab + path + list rows),
	// passed by renderPane as paneH. The tab row and path row consume 2
	// fixed lines that live above this function, so the scrollable list
	// area is h-2 rows. The 1-row bottom/status slack is owned by View()
	// (it emits h-1 total rows, reserving the last row as ConPTY-height
	// over-report slack), NOT here.
	visible := h - 2
	if visible < 1 {
		visible = 1
	}
	// Keep the cursor within the visible window.
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+visible {
		t.offset = t.cursor - visible + 1
	}
	if t.offset < 0 {
		t.offset = 0
	}

	var b strings.Builder

	// Show loading indicator if still loading.
	if t.loading {
		b.WriteString(statusStyle.Render("  " + t.loadingMsg))
		b.WriteString("\n")
		return b.String()
	}

	// Show error message if load failed.
	if t.loadErr != nil {
		b.WriteString(statusStyle.Render("  读取失败: " + t.loadErr.Error()))
		b.WriteString("\n")
		return b.String()
	}

	// Render entries (virtual scrolling: only visible rows).
	end := t.offset + visible
	if end > len(t.entries) {
		end = len(t.entries)
	}
	for i := t.offset; i < end; i++ {
		b.WriteString(m.formatEntry(t.entries[i], t.selected[t.entries[i].Path], i == t.cursor, active, w))
		b.WriteString("\n")
	}

	return b.String()
}

// formatEntry lays out one row: select-mark, name (fixed display width),
// size or <DIR>. Padding and truncation use display width (runes != cells for
// CJK), so Chinese filenames no longer overflow the column and the two panes
// stay horizontally aligned. The cursor highlight is drawn only on the active
// pane so it is unambiguous which side the keys control.
func (m *model) formatEntry(e fs.Entry, selected, cursor, active bool, w int) string {
	mark := "  "
	if selected {
		mark = "* "
	}
	nameFieldW := w - 2 - 13 // mark(2) + size(12) + separator(1)
	if nameFieldW < 4 {
		nameFieldW = 4
	}
	// Truncate to the column width FIRST (display cells, CJK-safe), then pad.
	// Without truncation a filename longer than the column would overflow,
	// widening the pane row and shoving the vertical separator + the other
	// pane sideways — the exact "display abnormal" symptom.
	name := padRightDW(truncateDW(e.Name, nameFieldW), nameFieldW)
	if e.IsDir {
		name = dirStyle.Render(name)
	}
	sizeField := "<DIR>"
	if !e.IsDir {
		sizeField = humanSize(e.Size)
	}
	line := mark + name + " " + padLeftDW(sizeField, 12)
	if cursor && active {
		return cursorStyle.Render(line)
	}
	return line
}

func (m *model) renderBottom(w int) string {
	t := m.curTab()
	selCount := len(t.selected)
	selSize := fs.SelectedSize(selectedEntries(t))
	line := fmt.Sprintf("选择:%d  大小:%s", selCount, humanSize(selSize))
	if m.status != "" {
		line = m.status
	}
	// Append queue status indicator when jobs are running (even when overlay
	// is hidden, so the user sees background activity).
	if m.queue != nil && len(m.queue.ActiveJobs()) > 0 {
		acts := m.queue.ActiveJobs()
		n := len(acts)
		if n > 1 {
			line += fmt.Sprintf("  [队列 %d 项运行中]", n)
		} else {
			line += fmt.Sprintf("  [队列 #%d %s]", acts[0].id, acts[0].typ)
		}
		if m.paused {
			line += " (暂停)"
		}
	}
	return statusStyle.Render(truncateDW(line, w))
}

// renderBatchRename draws the multi-field rename form with a live preview of
// every old -> new mapping. Conflicting rows are highlighted in red and a
// banner explains why the operation is currently blocked.
func (m *model) renderBatchRename() string {
	dir := m.curTab().path
	targets := make([]string, len(m.brFiles))
	for i, e := range m.brFiles {
		targets[i] = m.composeName(e.Name, i)
	}
	_, bad, planErr := planRenames(dir, m.brFiles, targets)
	badSet := make(map[int]bool, len(bad))
	for _, i := range bad {
		badSet[i] = true
	}

	innerW := m.width - 8
	if innerW < 40 {
		innerW = 40
	}

	var b strings.Builder
	// Title.
	b.WriteString(titleStyle.Render(fmt.Sprintf("批量重命名  (%d 项)", len(m.brFiles))))
	b.WriteString("\n")

	// Form fields.
	b.WriteString(m.brFieldLine(brFieldPrefix, "前缀: ", m.brPrefix))
	b.WriteString(m.brFieldLine(brFieldSuffix, "后缀: ", m.brSuffix))
	b.WriteString(m.brFieldLine(brFieldSearch, "查找: ", m.brSearch))
	b.WriteString(m.brFieldLine(brFieldReplace, "替换: ", m.brReplace))
	counter := "关"
	if m.brCounter {
		counter = "开"
	}
	b.WriteString(m.brFieldLine(brFieldCounter, "序号: ", "["+counter+"]"))
	b.WriteString(m.brFieldLine(brFieldStart, "起始: ", fmt.Sprintf("%d", m.brStart)))
	b.WriteString(m.brFieldLine(brFieldWidth, "位数: ", fmt.Sprintf("%d", m.brWidth)))
	b.WriteString("\n")

	// Preview header.
	b.WriteString(borderStyle.Render(strings.Repeat("─", innerW)))
	b.WriteString("\n")
	b.WriteString(brLabelStyle.Render("预览 (旧名 → 新名)"))
	b.WriteString("\n")

	// Preview rows (with scroll).
	nameW := 18
	if innerW > 60 {
		nameW = 30
	}
	visible := m.height - 14
	if visible < 3 {
		visible = 3
	}
	start := m.brScroll
	if start > len(m.brFiles)-visible {
		start = len(m.brFiles) - visible
	}
	if start < 0 {
		start = 0
	}
	end := start + visible
	if end > len(m.brFiles) {
		end = len(m.brFiles)
	}
	for i := start; i < end; i++ {
		old := truncateDW(m.brFiles[i].Name, nameW)
		arrow := " → "
		row := padRightDW(old, nameW) + arrow + truncateDW(targets[i], innerW-nameW-len(arrow))
		if badSet[i] {
			row = brConflictStyle.Render(row)
		} else if targets[i] == m.brFiles[i].Name {
			row = brDimStyle.Render(row + "  (不变)")
		}
		b.WriteString(row)
		b.WriteString("\n")
	}
	if len(m.brFiles) > visible {
		b.WriteString(brDimStyle.Render(fmt.Sprintf("... 共 %d 项，↑↓/PgUp/PgDn 滚动预览", len(m.brFiles))))
		b.WriteString("\n")
	}

	// Footer / banner.
	b.WriteString(borderStyle.Render(strings.Repeat("─", innerW)))
	b.WriteString("\n")
	if planErr != nil {
		b.WriteString(brConflictStyle.Render("✗ " + planErr.Error() + " — 修正规则后再执行"))
		b.WriteString("\n")
	} else {
		b.WriteString(brDimStyle.Render("就绪：按 Y 执行重命名"))
		b.WriteString("\n")
	}
	b.WriteString(brDimStyle.Render("F2 触发 · Tab/↑↓ 切换字段 · 文本域 ←→ 编辑 · 空格 切换序号 · Y 执行 · Esc 取消 · {n}=序号位置"))
	b.WriteString("\n")

	panel := brPanelStyle.Render(b.String())
	return centerBox(panel, m.width, m.height)
}

// brFieldLine renders one form field; the focused field is reverse-highlighted
// and, for text fields, shows a rune cursor inside the value.
func (m *model) brFieldLine(field int, label, value string) string {
	focused := m.brField == field
	var body string
	if focused && field != brFieldCounter && field != brFieldStart && field != brFieldWidth {
		rs := []rune(value)
		c := m.brCursor
		var shown string
		if c < len(rs) {
			shown = string(rs[:c]) + cursorNormal.Render(string(rs[c:c+1])) + string(rs[c+1:])
		} else {
			shown = string(rs) + cursorNormal.Render(" ")
		}
		body = label + shown
	} else {
		body = label + value
	}
	if focused {
		return brFocusStyle.Render(body) + "\n"
	}
	return brLabelStyle.Render(label) + value + "\n"
}

func (m *model) renderConfirm() string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 3).
		Render(m.confirmMsg + "\n\n  [Y] 确认    [N] 取消")
	return centerBox(box, m.width, m.height)
}

func (m *model) renderInput() string {
	rs := []rune(m.inputValue)
	cursor := m.inputCursor
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(rs) {
		cursor = len(rs)
	}
	var shown string
	if cursor < len(rs) {
		shown = string(rs[:cursor]) + cursorStyle.Render(string(rs[cursor:cursor+1])) + string(rs[cursor+1:])
	} else {
		shown = string(rs) + cursorStyle.Render(" ")
	}
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 3).
		Render(m.inputPrompt + "\n" + shown)
	return centerBox(box, m.width, m.height)
}

func (m *model) renderViewer() string {
	w := m.width
	h := m.height
	visible := h - 4
	if visible < 1 {
		visible = 1
	}
	title := titleStyle.Render("查看: " + m.viewerPath)
	var b strings.Builder
	end := m.viewerScroll + visible
	if end > len(m.viewerLines) {
		end = len(m.viewerLines)
	}
	for i := m.viewerScroll; i < end; i++ {
		b.WriteString(truncateDW(m.viewerLines[i], w))
		b.WriteString("\n")
	}
	footer := statusStyle.Render("F3/Esc 退出 · ↑↓ 滚动")
	return lipgloss.JoinVertical(lipgloss.Left, title, b.String(), footer)
}

// renderDrivePicker draws a centered list of available drive letters. The
// currently selected drive is reverse-highlighted.
func (m *model) renderDrivePicker() string {
	w := m.width
	h := m.height
	// Build the list lines.
	var b strings.Builder
	title := titleStyle.Render("选择盘符")
	b.WriteString(title)
	b.WriteString("\n\n")
	for i, d := range m.drives {
		line := "  " + d
		if i == m.pickerIndex {
			line = brFocusStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n  Enter 确认    Esc 取消")
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 3).
		Render(b.String())
	return centerBox(box, w, h)
}

// centerBox vertically centres a rendered box within the terminal.
func centerBox(s string, w, h int) string {
	lines := strings.Split(s, "\n")
	padTop := (h - len(lines)) / 2
	if padTop < 0 {
		padTop = 0
	}
	return strings.Repeat("\n", padTop) + s
}

// renderTreeView draws a recursive directory tree for the currently-viewed
// path. The root summary line shows total dirs/files/size across the whole
// subtree; each child row shows its own direct counts. ↑↓ navigates, Enter
// dives into the highlighted directory, Backspace returns to parent, Esc
// closes the overlay.
func (m *model) renderTreeView() string {
	w := m.width
	h := m.height
	if w < 40 {
		w = 40
	}
	if h < 8 {
		h = 8
	}
	var b strings.Builder
	// Title bar.
	title := titleStyle.Render("  目录树: " + m.treePath)
	b.WriteString(title)
	b.WriteString("\n\n")
	if m.treeLoading {
		b.WriteString("  加载中...")
		b.WriteString("\n\n  Esc 取消")
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(1, 2).
			Render(b.String())
		return centerBox(box, w, h)
	}
	if m.treeRoot == nil {
		b.WriteString("  （无法读取目录）")
		b.WriteString("\n\n  Esc 返回")
		box := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(1, 2).
			Render(b.String())
		return centerBox(box, w, h)
	}
	// Render tree lines.
	lines := fmtTree(m.treeRoot, w-6)
	for _, line := range lines {
		b.WriteString(line)
		b.WriteString("\n")
	}
	// Navigation hint.
	hint := "  ↑↓ 导航  Enter 进入  ← 返回上级  Esc 关闭"
	if len(m.treeHistory) > 0 {
		hint = "  ↑↓ 导航  Enter 进入  ←/Backspace 返回上级  Esc 关闭"
	}
	b.WriteString("\n" + statusStyle.Render(truncateDW(hint, w-4)))
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 2).
		Render(b.String())
	return centerBox(box, w, h)
}

func selectedEntries(t *tab) []fs.Entry {
	if len(t.selected) == 0 {
		return nil
	}
	out := make([]fs.Entry, 0, len(t.selected))
	for _, e := range t.entries {
		if t.selected[e.Path] {
			out = append(out, e)
		}
	}
	return out
}

// truncateDW cuts s to at most max DISPLAY cells (CJK counts as 2). Used for
// the path line where alignment must hold under mixed scripts.
//
// ANSI-aware truncation: every CSI escape sequence (e.g. an unclosed colour
// code from a styled tab label that has to be cut short) is preserved
// verbatim and not counted against the visible-cell budget. The output is
// closed with \x1b[0m whenever an SGR was still open, so the row that
// follows in the pane is NOT painted with the same background colour.
// Without this guard a wide tab bar in a narrow pane would clip the
// active-tab style and the rest of the pane — and the other pane after
// lipgloss.JoinHorizontal — would render with the leftover background,
// producing the "right-pane background bleeding onto the left pane" symptom.
//
// Why this loop and NOT a fast-path guard: a tempting optimisation is
// `if lipgloss.Width(s) <= max { return s }`, but that guard is WRONG for
// ambiguous-width glyphs. lipgloss.Width counts an em dash '—' as 1 cell,
// whereas East-Asian terminals (e.g. Windows Terminal on a CJK locale)
// paint it as 2. So a filename like "报告——终稿" would pass the lipgloss
// guard (under-budgeted) yet actually overflow the row by several cells in
// the real terminal, which then wraps the overflow onto a phantom extra
// line below the filename — the exact "extra > row" bug. The loop below is
// ANSI-aware and budgets each visible rune with runewidth.RuneWidth (em
// dash = 2), so it always matches what the terminal paints. It also handles
// the "already fits" case naturally (it just copies every rune until the
// budget is exhausted), so the guard is pure downside and is intentionally
// omitted.
//
// Note: runewidth.StringWidth is NOT a safe substitute for this loop
// either — it treats ANSI escape sequences as visible cells (it reports
// width 10 for "\x1b[44mabc\x1b[0m"), which would over-budget styled
// strings. Only the per-rune loop, which skips ESC sequences, is correct.
func truncateDW(s string, max int) string {
	if max <= 0 {
		return ""
	}
	if s == "" {
		return ""
	}
	var b strings.Builder
	w := 0
	for i := 0; i < len(s); {
		// Preserve complete CSI escape sequences (and 2-byte ESC escapes)
		// without consuming the visible-cell budget.
		if s[i] == 0x1b {
			esc := s[i:]
			j := 1 // skip ESC
			if j < len(esc) && esc[j] == '[' {
				j++
				for j < len(esc) {
					c := esc[j]
					j++
					if c >= 0x40 && c <= 0x7e {
						break
					}
				}
			} else if j < len(esc) {
				j++
			}
			b.WriteString(esc[:j])
			i += j
			continue
		}
		r, size := utf8DecodeRune(s[i:])
		// Budget each rune with runewidth.RuneWidth — the SAME conservative
		// ruler the terminal paints with on East-Asian locales (ambiguous
		// glyphs like '—' count as 2 cells, matching Windows Terminal). This
		// must agree with clampRowWidth and padRightDW below; if it ever
		// diverged to lipgloss.Width (which counts '—' as 1) the row would be
		// under-budgeted and the terminal would wrap the overflow onto a
		// phantom extra line under a filename containing '——'.
		rw := runewidth.RuneWidth(r)
		if rw < 0 {
			rw = 0
		}
		if w+rw > max {
			break
		}
		b.WriteString(s[i : i+size])
		w += rw
		i += size
	}
	out := b.String()
	if hasOpenSGR(out) {
		out += "\x1b[0m"
	}
	return out
}

// utf8DecodeRune returns the rune at s and its byte length (1-4). Used
// here instead of utf8.DecodeRune because the latter returns RuneError
// at EOF which we don't want to treat as a real character.
func utf8DecodeRune(s string) (rune, int) {
	if len(s) == 0 {
		return 0, 0
	}
	switch {
	case s[0] < 0x80:
		return rune(s[0]), 1
	case s[0] < 0xC0:
		return rune(s[0]), 1
	case s[0] < 0xE0:
		if len(s) < 2 {
			return rune(s[0]), 1
		}
		return rune(s[0]&0x1F)<<6 | rune(s[1]&0x3F), 2
	case s[0] < 0xF0:
		if len(s) < 3 {
			return rune(s[0]), 1
		}
		return rune(s[0]&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	default:
		if len(s) < 4 {
			return rune(s[0]), 1
		}
		return rune(s[0]&0x07)<<18 | rune(s[1]&0x3F)<<12 | rune(s[2]&0x3F)<<6 | rune(s[3]&0x3F), 4
	}
}

// hasOpenSGR returns true when s ends with an SGR sequence that has been
// started but not closed with a final reset ("\x1b[0m" or equivalent).
// Cheap heuristic: scan from the last ESC byte to the end.
func hasOpenSGR(s string) bool {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == 0x1b {
			// i is the last ESC; check what follows.
			rest := s[i:]
			// If the run ends with a CSI ... m final, consider it closed.
			// Otherwise the row still has an open colour.
			if len(rest) == 1 || rest[1] != '[' {
				return false
			}
			for j := len(rest) - 1; j >= 2; j-- {
				c := rest[j]
				if c >= 0x40 && c <= 0x7e {
					// final byte of an escape sequence
					if c == 'm' && strings.HasSuffix(rest, "\x1b[0m") {
						return false
					}
					return true
				}
			}
			return true
		}
	}
	return false
}

// padRightDW right-pads s with spaces until its DISPLAY width reaches n.
// Uses runewidth.StringWidth so the pad budget matches what the terminal
// actually paints (ambiguous glyphs = 2 cells). This MUST stay in lock-step
// with truncateDW and clampRowWidth — a divergent ruler (e.g. lipgloss.Width)
// would under-budget em-dash filenames and let the terminal wrap them.
func padRightDW(s string, n int) string {
	w := runewidth.StringWidth(s)
	if w >= n {
		return s
	}
	return s + strings.Repeat(" ", n-w)
}

// padLeftDW left-pads s with spaces until its DISPLAY width reaches n.
func padLeftDW(s string, n int) string {
	w := runewidth.StringWidth(s)
	if w >= n {
		return s
	}
	return strings.Repeat(" ", n-w) + s
}

// humanSize formats a byte count as a short human-readable string.
func humanSize(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div := int64(unit)
	exp := 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}

// renderQueueOverlay draws the active job list. Each job gets one row; the
// most recently started job is at the top. Succeeded jobs show a checkmark,
// failed ones an X, and in-flight ones a progress bar. The overlay consumes
// the full viewport so it can be used as a dedicated task monitor; hit Esc to
// return to the normal dual-pane view (the queue keeps running in the
// background).
func (m *model) renderQueueOverlay() string {
	w := m.width
	h := m.height
	if w < 40 {
		w = 40
	}
	if h < 6 {
		h = 6
	}

	var b strings.Builder
	header := titleStyle.Render("  任务队列 ")
	if m.paused {
		header += statusStyle.Render("[已暂停 按空格恢复]")
	} else {
		header += statusStyle.Render("[按 Esc 返回  空格暂停  Ctrl+C 取消当前  Ctrl+A 取消全部]")
	}
	b.WriteString(header)
	b.WriteString("\n")

	// Show all tracked jobs, newest first.
	all := m.queue.AllJobs()
	// Reverse iterate.
	for i := len(all) - 1; i >= 0; i-- {
		j := all[i]
		line := m.renderJobLine(j, w-4)
		b.WriteString(line)
		b.WriteString("\n")
	}

	if len(all) == 0 {
		b.WriteString("  无活跃任务")
	}

	// Bottom hint row.
	b.WriteString("\n  Esc 返回主界面")
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 2).
		Render(b.String())
	return centerBox(box, m.width, m.height)
}

func (m *model) renderJobLine(j *Job, maxW int) string {
	elapsed, err := j.Stats()
	statusIcon := " ▶"
	if j.IsDone() {
		if err == nil {
			statusIcon = " ✓"
		} else {
			statusIcon = " ✗"
		}
	}
	label := fmt.Sprintf(" #%d %s%s", j.id, j.typ, statusIcon)
	summary := jobSummary(j)
	dstBase := filepath.Base(j.dstDir)
	info := fmt.Sprintf("%s  →  %s", summary, dstBase)

	// Build the line: label (fixed width) + info + elapsed + status.
	// Reserve: label(12) + " " + info + " " + elapsed(8) + " " + done/total.
	// We truncate info on the right if needed.
	elapsedStr := FormatElapsed(elapsed)
	pad := 14 + 1 + 9 + 1 // label + sp + elapsed + sp + "done/total"
	avail := maxW - pad
	if avail < 20 {
		avail = 20
	}
	infoTrunc := truncateDW(info, avail)

	// Progress bar: show done/total if available, otherwise just "…"
	prog := "…"
	if j.IsDone() {
		if err == nil {
			prog = "完成"
		} else {
			prog = "失败"
		}
	}
	row := fmt.Sprintf("  %s %s %s  %s", label, infoTrunc, elapsedStr, prog)
	return statusStyle.Render(row)
}

// renderAssocEditor draws the extension -> custom application editor. It shows
// three action tabs (查看 F3 / 编辑 F4 / 打开 Enter), the bindings for the
// active tab, and a hint line. Adding/deleting bindings is driven by the
// shared input overlay invoked from handleAssocKey.
func (m *model) renderAssocEditor() string {
	w, h := m.width, m.height
	var b strings.Builder
	b.WriteString(titleStyle.Render("  扩展名关联应用 (Ctrl+E)  "))
	b.WriteString("\n\n")

	// Action tabs.
	actions := assocActions()
	var tabParts []string
	for i, a := range actions {
		label := " " + assocActionLabel(a) + " "
		if i == m.assocActionIdx {
			tabParts = append(tabParts, tabActiveStyle.Render(label))
		} else {
			tabParts = append(tabParts, tabInactiveStyle.Render(label))
		}
	}
	b.WriteString(strings.Join(tabParts, " "))
	b.WriteString("\n")
	b.WriteString(borderStyle.Render(strings.Repeat("─", maxDW(w-6, 10))))
	b.WriteString("\n\n")

	list := m.currentAssocList()
	if len(list) == 0 {
		b.WriteString(statusStyle.Render("  （无关联，按 a 新增）"))
		b.WriteString("\n")
	} else {
		for i, ext := range list {
			cmd := m.assoc[string(actions[m.assocActionIdx])][ext]
			line := fmt.Sprintf("  %s  →  %s", ext, cmd)
			if i == m.assocCursor {
				line = brFocusStyle.Render(line)
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	// Transient note (after add/delete).
	if m.assocMsg != "" {
		b.WriteString("\n  " + statusStyle.Render(m.assocMsg))
	}

	// Hint.
	hint := "  Tab 切换动作  ↑↓ 选择  a 新增  d 删除  Esc 关闭"
	b.WriteString("\n\n  " + statusStyle.Render(truncateDW(hint, w-4)))

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(1, 2).
		Render(b.String())
	return centerBox(box, w, h)
}

// maxDW returns the larger of a and b (guards against negative widths when the
// terminal is very narrow). Used to size border rules defensively.
func maxDW(a, b int) int {
	if a > b {
		return a
	}
	return b
}
