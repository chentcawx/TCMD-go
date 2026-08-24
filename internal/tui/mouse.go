package tui

import (
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	tea "github.com/charmbracelet/bubbletea"
)

// Layout row/column constants shared by the renderer and the mouse hit-tester.
// The dual-pane block is: row 0 = tab bar, row 1 = path bar, row 2+ = file
// list. The vertical separator sits between the two panes at column sepCol.
const (
	rowTabs  = 0
	rowPath  = 1
	rowList  = 2
	sepWidth = 2 // the box-drawing separator occupies 2 display cells on CJK locales

	// doubleClickMS is the maximum gap between two Left presses on cells near
	// each other (within doubleClickTolerance) for them to count as a
	// double-click. Terminals never emit a native double-click event, so we
	// synthesize one by timing presses.
	doubleClickMS = 500 * time.Millisecond

	// doubleClickTolerance is the max cell-distance allowed between two clicks
	// for them to be considered the "same" cell. Real users rarely land on the
	// exact same cell twice; a tolerance of ±3 cells covers the vast majority
	// of natural double-clicks without turning unrelated single clicks into
	// false doubles.
	doubleClickTolerance = 3
)

// sepCol returns the display column of the vertical separator given the
// terminal width, matching the split computed in View().
func (m *model) sepCol() int {
	w := m.width
	if w < 40 {
		w = 40
	}
	sepW := runewidth.RuneWidth('│') // MUST match View()'s separator width ruler
	if w-sepW < 2 {
		sepW = 1
	}
	return (w - sepW) / 2
}

// mousePaneAt returns 0 (left) or 1 (right) for an X cell coordinate, and
// whether X actually falls on a pane (false on the separator gap).
func (m *model) mousePaneAt(x int) (pane int, onPane bool) {
	sc := m.sepCol()
	if x < sc {
		return 0, true
	}
	if x > sc+sepWidth-1 {
		return 1, true
	}
	return 0, false
}

// mouseListRow converts a Y cell coordinate (0-based from the top of the
// alt-screen) into a global entry index for the given pane, accounting for the
// list scroll offset. Returns -1 when Y is not on the list area.
func (m *model) mouseListRow(p int, y int) int {
	if y < rowList {
		return -1
	}
	t := m.panes[p].current()
	// The pane block occupies m.height-1 rows (tab + path + list); the tab
	// and path rows consume 2 fixed lines, leaving m.height-3 scrollable
	// list rows. This must stay in lock-step with renderPane/renderList; the
	// extra 1-row slack at the very bottom (emitted as h-1 total rows) is the
	// ConPTY non-maximized height over-report buffer and is not clickable.
	visible := m.height - 3
	if visible < 1 {
		visible = 1
	}
	idx := t.offset + (y - rowList)
	if idx < 0 || idx >= len(t.entries) {
		return -1
	}
	return idx
}

// handleMouse dispatches mouse events. Overlays consume their own input; the
// dual-pane UI only reacts when no overlay is active.
func (m *model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	switch m.ov {
	case overlayContextMenu:
		return m.handleContextMenuMouse(msg)
	case overlayNone:
		// fall through to normal pane interaction
	default:
		return m, nil // other overlays ignore mouse
	}

	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.pageMove(-1)
	case tea.MouseButtonWheelDown:
		m.pageMove(1)
	case tea.MouseButtonLeft:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		// Synthesize a double-click: two Left presses on cells near each
		// other (within doubleClickTolerance) and within doubleClickMS
		// open/enter the item under the cursor (dir → enter in TUI, file
		// → system default via the open verb).
		if !m.lastClickT.IsZero() &&
			time.Since(m.lastClickT) <= doubleClickMS &&
			absInt(msg.X-m.lastClickX) <= doubleClickTolerance &&
			absInt(msg.Y-m.lastClickY) <= doubleClickTolerance {
			m.lastClickT = time.Time{} // consume so a triple click isn't two doubles
			m.handleDoubleClick(msg.X, msg.Y)
			return m, nil
		}
		m.lastClickX = msg.X
		m.lastClickY = msg.Y
		m.lastClickT = time.Now()
		m.handleLeftClick(msg.X, msg.Y)
	case tea.MouseButtonRight:
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}
		m.openContextMenu(msg.X, msg.Y)
	}
	return m, nil
}

func absInt(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// handleLeftClick implements the core mouse interaction: activate a pane by
// clicking anywhere in it, switch its tab when the click lands on the tab bar,
// move the cursor to the clicked file, and — for the list area — enter a
// directory (single click) or toggle selection on a file (single click).
func (m *model) handleLeftClick(x, y int) {
	p, onPane := m.mousePaneAt(x)
	if !onPane {
		return
	}
	// Activate the clicked pane.
	if m.active != p {
		m.active = p
	}
	tb := m.panes[p]
	// Tab bar: pick the tab whose label spans this X.
	if y == rowTabs {
		if idx := tb.tabAt(x, m.width); idx >= 0 {
			tb.active = idx
			m.saveConfig()
		}
		return
	}
	// Path bar click: just focus the pane (already done above).
	if y == rowPath {
		return
	}
	// List area: move cursor to the clicked row.
	idx := m.mouseListRow(p, y)
	if idx < 0 {
		return
	}
	tb.current().cursor = idx
	// Keep the clicked row within the visible window.
	m.ensureCursorVisible(tb.current())
}

// handleDoubleClick opens or enters the item under the clicked cell. A
// directory is entered in-place (matching Total Commander's double-click); a
// file is launched with the OS default association via the system "open" verb
// (the genuine Explorer action, not a `start` approximation).
func (m *model) handleDoubleClick(x, y int) {
	p, onPane := m.mousePaneAt(x)
	if !onPane {
		return
	}
	if m.active != p {
		m.active = p
	}
	tb := m.panes[p]
	idx := m.mouseListRow(p, y)
	if idx < 0 {
		return
	}
	tb.current().cursor = idx
	m.ensureCursorVisible(tb.current())
	m.openOrEnterCurrent()
}

// openOrEnterCurrent acts on the cursor row of the active pane: directories are
// entered in the TUI, files are opened through the system "open" verb.
func (m *model) openOrEnterCurrent() {
	t := m.curTab()
	if len(t.entries) == 0 {
		return
	}
	e := t.entries[t.cursor]
	if e.IsDir {
		m.curPane().tabs[m.curPane().active] = newTab(e.Path)
		m.saveConfig()
		return
	}
	if cmd, ok := m.ResolveAssoc(AssocOpen, extOf(e.Name)); ok {
		m.launchAssoc(AssocOpen, cmd, e.Path)
		return
	}
	if err := openFile(e.Path); err != nil {
		m.status = "打开失败: " + err.Error()
	} else {
		m.status = "已用默认程序打开: " + e.Name
	}
}

// ensureCursorVisible re-derives the scroll offset so the cursor stays in view,
// mirroring renderList's clamping. visible must equal the list area height
// (m.height-3: pane block m.height-1 minus the 2 fixed tab/path rows).
func (m *model) ensureCursorVisible(t *tab) {
	visible := m.height - 3
	if visible < 1 {
		visible = 1
	}
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	if t.cursor >= t.offset+visible {
		t.offset = t.cursor - visible + 1
	}
	if t.offset < 0 {
		t.offset = 0
	}
}

// tabAt returns the tab index whose label covers display column x, or -1.
func (p *pane) tabAt(x, w int) int {
	// Reconstruct the same label layout renderTabs uses.
	var parts []string
	widths := make([]int, len(p.tabs))
	cum := 0
	for i, t := range p.tabs {
		name := filepathBase(t.path)
		label := " " + name + " "
		parts = append(parts, label)
		// Each label is padded to its rune/display width; renderTabs joins with
		// a single space, so account for that gap after the first.
		wLabel := runeWidthOf(label)
		widths[i] = wLabel
		cum += wLabel
		if i > 0 {
			cum += 1 // joining space
		}
	}
	// Walk the cumulative layout.
	pos := 0
	for i := range p.tabs {
		if i > 0 {
			pos += 1 // gap
		}
		if x >= pos && x < pos+widths[i] {
			return i
		}
		pos += widths[i]
	}
	return -1
}

// ---- right-click context menu ----

type ctxItem struct {
	label string
	run   func(*model)
}

// openContextMenu anchors a Total-Commander-style action menu at the cursor.
// If the click is on a file row, that row becomes the cursor (and its file is
// the implicit target); otherwise the menu acts on the pane's directory.
func (m *model) openContextMenu(x, y int) {
	p, onPane := m.mousePaneAt(x)
	if !onPane {
		return
	}
	if m.active != p {
		m.active = p
	}
	tb := m.panes[p]
	targetDir := tb.current().path
	cursorSet := false
	if y >= rowList {
		if idx := m.mouseListRow(p, y); idx >= 0 {
			tb.current().cursor = idx
			m.ensureCursorVisible(tb.current())
			cursorSet = true
		}
	}
	// Build the menu. "选择此项" only makes sense when a file row was clicked.
	items := []ctxItem{
		{label: "打开/进入", run: func(m *model) { m.openOrEnterCurrent() }},
		{label: "复制 (F5)", run: func(m *model) { m.beginCopy() }},
		{label: "移动 (F6)", run: func(m *model) { m.beginMove() }},
		{label: "批量重命名 (F2)", run: func(m *model) { m.beginBatchRename() }},
		{label: "新建目录 (F7)", run: func(m *model) { m.beginMkdir() }},
		{label: "删除 (F8)", run: func(m *model) { m.beginDelete() }},
		{label: "刷新 (Ctrl+R)", run: func(m *model) { m.reloadCurrent() }},
	}
	if cursorSet {
		// Insert a "select this item" action near the top.
		items = append([]ctxItem{
			{label: "选择此项", run: func(m *model) {
				m.toggleSelect()
			}},
		}, items...)
	}
	m.ctxItems = items
	m.ctxDir = targetDir
	m.ctxX = x
	m.ctxY = y
	m.ctxIndex = 0
	m.ov = overlayContextMenu
}

// handleContextMenuKey navigates and executes the context menu.
func (m *model) handleContextMenuKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeOverlay()
		return m, nil
	}
	switch msg.Type {
	case tea.KeyUp, tea.KeyCtrlP:
		if m.ctxIndex > 0 {
			m.ctxIndex--
		}
	case tea.KeyDown, tea.KeyCtrlN:
		if m.ctxIndex < len(m.ctxItems)-1 {
			m.ctxIndex++
		}
	case tea.KeyRunes:
		switch msg.String() {
		case "k":
			if m.ctxIndex > 0 {
				m.ctxIndex--
			}
		case "j":
			if m.ctxIndex < len(m.ctxItems)-1 {
				m.ctxIndex++
			}
		}
	case tea.KeyEnter:
		m.execContextItem()
		return m, nil
	}
	return m, nil
}

// handleContextMenuMouse closes the menu on any click outside, or executes the
// hovered item when the click lands on a menu row.
func (m *model) handleContextMenuMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Button == tea.MouseButtonRight && msg.Action == tea.MouseActionPress {
		m.closeOverlay()
		return m, nil
	}
	if msg.Button == tea.MouseButtonLeft && msg.Action == tea.MouseActionPress {
		row := m.ctxItemRowAt(msg.Y)
		left := m.ctxPanelLeft() + 2 // skip left border + 1 padding column
		right := left + contextMenuWidth(m.ctxItems)
		if row >= 0 && msg.X >= left && msg.X < right {
			m.ctxIndex = row
			m.execContextItem()
			return m, nil
		}
		// Click outside the menu closes it without acting.
		m.closeOverlay()
	}
	return m, nil
}

// handleDrivePickerKey navigates the drive-letter picker: ↑↓ to move, Enter
// to confirm, Esc to cancel.
func (m *model) handleDrivePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.closeOverlay()
		return m, nil
	case "enter":
		if len(m.drives) > 0 && m.pickerIndex >= 0 && m.pickerIndex < len(m.drives) {
			drive := m.drives[m.pickerIndex]
			m.switchToDrive(drive)
		}
		m.closeOverlay()
		return m, nil
	}
	switch msg.Type {
	case tea.KeyUp, tea.KeyCtrlP:
		if m.pickerIndex > 0 {
			m.pickerIndex--
		}
	case tea.KeyDown, tea.KeyCtrlN:
		if m.pickerIndex < len(m.drives)-1 {
			m.pickerIndex++
		}
	}
	return m, nil
}

func (m *model) execContextItem() {
	if m.ctxIndex < 0 || m.ctxIndex >= len(m.ctxItems) {
		m.closeOverlay()
		return
	}
	fn := m.ctxItems[m.ctxIndex].run
	m.closeOverlay()
	if fn != nil {
		fn(m)
	}
}

// contextMenuTop returns the top row of the menu, grown upward from the anchor
// so it never overflows the bottom of the screen. The drawn panel is a bordered
// box: 1 top border line + 1 title line + len(ctxItems) item lines + 1 bottom
// border line, so its full height is len(ctxItems)+3.
func (m *model) contextMenuTop() int {
	h := len(m.ctxItems) + 3 // top border + title + items + bottom border
	top := m.ctxY - h + 1
	if top < 0 {
		top = 0
	}
	if top+h > m.height {
		top = m.height - h
	}
	if top < 0 {
		top = 0
	}
	return top
}

// ctxPanelLeft returns the display column where the bordered panel starts,
// matching the right-shift applied in renderContextMenu (the menu is pushed
// left only when it would otherwise run off the right edge).
func (m *model) ctxPanelLeft() int {
	shift := m.ctxX
	panelW := contextMenuWidth(m.ctxItems) + 4 // 2 borders + 2 padding(0,1)
	if shift+panelW > m.width {
		shift = maxInt(0, m.width-panelW)
	}
	return shift
}

// ctxItemRowAt maps a mouse Y cell to a menu item index, accounting for the
// panel's top border line and the title line that precede the items. Returns
// -1 when Y is not over an item row.
func (m *model) ctxItemRowAt(y int) int {
	top := m.contextMenuTop()
	row := y - top - 2 // skip top border + title line
	if row < 0 || row >= len(m.ctxItems) {
		return -1
	}
	return row
}

// ---- small helpers ----

func filepathBase(p string) string {
	b := filepath.Base(p)
	if b == "" || b == "." {
		return p
	}
	return b
}

func runeWidthOf(s string) int {
	return runewidth.StringWidth(s)
}

// contextMenuWidth returns the inner width (excluding border) needed to render
// the menu without wrapping. Uses runewidth.StringWidth so the budget matches
// the painted width (ambiguous glyphs like '—' count as 2, as the terminal
// draws them — keeping the menu from wrapping on CJK locales).
func contextMenuWidth(items []ctxItem) int {
	w := 0
	for _, it := range items {
		if lw := runewidth.StringWidth(it.label); lw > w {
			w = lw
		}
	}
	if lw := runewidth.StringWidth("上下文菜单"); lw > w {
		w = lw
	}
	return w
}

// renderContextMenu draws the right-click action menu anchored at (ctxX,ctxY),
// growing upward so it stays on screen, with the active row reverse-highlighted.
func (m *model) renderContextMenu() string {
	innerW := contextMenuWidth(m.ctxItems)
	top := m.contextMenuTop()

	var b strings.Builder
	b.WriteString(brLabelStyle.Render("上下文菜单"))
	b.WriteString("\n")
	for i, it := range m.ctxItems {
		line := padRightDW(it.label, innerW)
		if i == m.ctxIndex {
			line = brFocusStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	panel := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Render(b.String())

	// Position the panel: pad above to the top row, then the panel, then the
	// remainder below (so it sits at the anchor regardless of growth). The
	// left shift must match ctxPanelLeft() used by the hit-test so clicks line
	// up with what is drawn.
	lines := strings.Split(panel, "\n")
	padTop := top
	shift := m.ctxPanelLeft()
	var out strings.Builder
	for i := 0; i < padTop; i++ {
		out.WriteString("\n")
	}
	for _, l := range lines {
		out.WriteString(strings.Repeat(" ", shift))
		out.WriteString(l)
		out.WriteString("\n")
	}
	return out.String()
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
