package tui

import (
	"testing"
	"time"

	"tcmd/internal/fs"

	tea "github.com/charmbracelet/bubbletea"
)

// newTestModel returns a bare model with empty tabs whose width/height are set
// for deterministic coordinate math. Tests inject synthetic entries directly
// into the tabs instead of hitting the disk.
func newTestModel() *model {
	m := &model{
		panes:  [2]*pane{{tabs: []*tab{{}}, active: 0}, {tabs: []*tab{{}}, active: 0}},
		active: 0,
		width:  80,
		height: 24,
	}
	return m
}

func TestSepColSymmetry(t *testing.T) {
	m := newTestModel()
	m.width = 80
	sc := m.sepCol()
	// With w=80, sepW=2 => (80-2)/2 = 39. Pane split must be near center.
	if sc != 39 {
		t.Fatalf("sepCol(80) = %d, want 39", sc)
	}
	// A click left of the separator lands on the left pane; right of it on the
	// right pane; on the separator column itself is "not on a pane".
	p, on := m.mousePaneAt(sc - 1)
	if !on || p != 0 {
		t.Fatalf("x=%d should be left pane (on=%v p=%d)", sc-1, on, p)
	}
	p, on = m.mousePaneAt(sc + sepWidth)
	if !on || p != 1 {
		t.Fatalf("x=%d should be right pane (on=%v p=%d)", sc+sepWidth, on, p)
	}
	_, on = m.mousePaneAt(sc) // exactly on the separator
	if on {
		t.Fatalf("x=%d on separator should report onPane=false", sc)
	}
}

func TestMouseListRowClamps(t *testing.T) {
	m := newTestModel()
	m.height = 24

	// Build a tab with 50 entries and an offset of 10.
	t2 := &tab{
		path:     ".",
		offset:   10,
		cursor:   10,
		entries:  makeEntries(50),
		selected: make(map[string]bool),
	}
	m.panes[0].tabs[0] = t2

	// rowList=2; clicking the first visible row => offset 10 (entry 10)
	idx := m.mouseListRow(0, rowList)
	if idx != 10 {
		t.Fatalf("first visible row should map to offset 10, got %d", idx)
	}
	// Clicking below all entries returns -1.
	idx = m.mouseListRow(0, rowList+100)
	if idx != -1 {
		t.Fatalf("row past the end should be -1, got %d", idx)
	}
	// Clicking above the list (tab bar) returns -1.
	idx = m.mouseListRow(0, rowTabs)
	if idx != -1 {
		t.Fatalf("tab bar row should be -1, got %d", idx)
	}
}

// makeEntries builds n synthetic fs.Entry values (alternating dir/file) with
// stable names so tests can reason about cursor targets.
func makeEntries(n int) []fs.Entry {
	out := make([]fs.Entry, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, fs.Entry{
			Name:  entryName(i),
			IsDir: i%2 == 0,
			Path:  entryName(i),
		})
	}
	return out
}

func TestHandleLeftClickSetsCursor(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	t2 := &tab{
		path:     ".",
		offset:   0,
		cursor:   0,
		entries:  makeEntries(30),
		selected: make(map[string]bool),
	}
	m.panes[0].tabs[0] = t2
	m.active = 0

	// Click the right pane first to ensure activation works, then a left-pane row.
	sc := m.sepCol()
	m.handleLeftClick(sc+sepWidth+2, rowList+5) // right pane, 5th visible row
	if m.active != 1 {
		t.Fatalf("click on right pane should activate it, active=%d", m.active)
	}

	// Click left pane, 7th visible row => cursor should be 7 (offset 0).
	m.handleLeftClick(2, rowList+7)
	if m.active != 0 {
		t.Fatalf("click on left pane should activate it, active=%d", m.active)
	}
	if t2.cursor != 7 {
		t.Fatalf("left click row 7 should set cursor=7, got %d", t2.cursor)
	}
}

func TestHandleLeftClickActivatesPaneOnlyOnPathBar(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	t2 := &tab{
		path:     ".",
		offset:   0,
		cursor:   0,
		entries:  makeEntries(10),
		selected: make(map[string]bool),
	}
	m.panes[1].tabs[0] = t2
	m.active = 0

	// Clicking the path bar (row 1) of the right pane focuses it but must not
	// move the cursor.
	sc := m.sepCol()
	m.handleLeftClick(sc+sepWidth+2, rowPath)
	if m.active != 1 {
		t.Fatalf("path-bar click should focus right pane, active=%d", m.active)
	}
	if t2.cursor != 0 {
		t.Fatalf("path-bar click must not move cursor, got %d", t2.cursor)
	}
}

func TestWheelScrollCallsPageMove(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	t2 := &tab{
		path:     ".",
		offset:   0,
		cursor:   0,
		entries:  makeEntries(100),
		selected: make(map[string]bool),
	}
	m.panes[0].tabs[0] = t2
	m.active = 0

	before := t2.cursor
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonWheelDown, Action: tea.MouseActionPress, X: 2, Y: rowList + 3})
	if t2.cursor <= before {
		t.Fatalf("wheel down should advance cursor (before=%d after=%d)", before, t2.cursor)
	}

	after := t2.cursor
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonWheelUp, Action: tea.MouseActionPress, X: 2, Y: rowList + 3})
	if t2.cursor >= after {
		t.Fatalf("wheel up should retreat cursor (after=%d now=%d)", after, t2.cursor)
	}
}

func TestOpenContextMenuAndDispatch(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	t2 := &tab{
		path:     "C:\\work",
		offset:   0,
		cursor:   2,
		entries:  makeEntries(10),
		selected: make(map[string]bool),
	}
	m.panes[0].tabs[0] = t2
	m.active = 0

	// Right-click a file row (row 3 => entry index 3) on the left pane.
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonRight, Action: tea.MouseActionPress, X: 2, Y: rowList + 3})
	if m.ov != overlayContextMenu {
		t.Fatalf("right-click should open context menu, ov=%d", m.ov)
	}
	if t2.cursor != 3 {
		t.Fatalf("right-click on a row should set cursor to that row, got %d", t2.cursor)
	}
	// The "选择此项" item is prepended when a file row was clicked.
	if len(m.ctxItems) == 0 || m.ctxItems[0].label != "选择此项" {
		t.Fatalf("expected 选择此项 as first item, got %v", itemLabels(m.ctxItems))
	}

	// Navigate down two rows and dispatch "刷新" via index.
	m.ctxIndex = len(m.ctxItems) - 1 // 刷新 (Ctrl+R)
	m.execContextItem()
	if m.ov != overlayNone {
		t.Fatalf("execContextItem should close the overlay, ov=%d", m.ov)
	}
	if m.ctxItems != nil {
		t.Fatalf("ctxItems should be reset after close")
	}
}

func TestContextMenuClosesOnRightClick(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	t2 := &tab{
		path:     ".",
		offset:   0,
		cursor:   0,
		entries:  makeEntries(10),
		selected: make(map[string]bool),
	}
	m.panes[0].tabs[0] = t2
	m.active = 0

	sc := m.sepCol()
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonRight, Action: tea.MouseActionPress, X: 2, Y: rowList + 1})
	if m.ov != overlayContextMenu {
		t.Fatalf("context menu should be open, ov=%d", m.ov)
	}
	// A second right-click anywhere closes it.
	m.handleContextMenuMouse(tea.MouseMsg{Button: tea.MouseButtonRight, Action: tea.MouseActionPress, X: sc + sepWidth + 5, Y: 10})
	if m.ov != overlayNone {
		t.Fatalf("second right-click should close the menu, ov=%d", m.ov)
	}
}

func TestContextMenuTopStaysOnScreen(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	// 12 items => bordered panel height = 12 + 3 = 15 (top border + title +
	// items + bottom border). Anchor near bottom (y=20).
	m.ctxItems = make([]ctxItem, 12)
	m.ctxY = 20
	m.height = 24
	top := m.contextMenuTop()
	if top < 0 {
		t.Fatalf("menu top must not be negative, got %d", top)
	}
	if top+15 > m.height {
		t.Fatalf("menu bottom (%d) overflows screen height %d", top+15, m.height)
	}
}

// TestContextMenuClickSelectsExactRow locks the double-row offset bug: a left
// click on the visual row of item i must dispatch item i, not i+2. The rendered
// item i sits at contextMenuTop()+2 (top border + title) + i. Each item's run
// records its own index so we can observe which one actually fired (note:
// execContextItem resets ctxIndex via closeOverlay, so we can't read ctxIndex
// after dispatch).
func TestContextMenuClickSelectsExactRow(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	m.ctxX = 4
	m.ctxY = 20
	m.height = 24

	dispatched := -1
	buildItems := func() {
		m.ctxItems = make([]ctxItem, 8)
		for i := range m.ctxItems {
			idx := i
			m.ctxItems[i] = ctxItem{
				label: "item" + itoa(i),
				run:   func(m *model) { dispatched = idx },
			}
		}
	}
	buildItems()
	top := m.contextMenuTop() // must be computed AFTER items exist (height depends on len)
	for i := 0; i < len(m.ctxItems); i++ {
		dispatched = -1
		y := top + 2 + i
		x := m.ctxPanelLeft() + 2 // inside left border + padding
		m.ov = overlayContextMenu
		buildItems()
		m.handleContextMenuMouse(tea.MouseMsg{
			Button: tea.MouseButtonLeft,
			Action: tea.MouseActionPress,
			X:      x,
			Y:      y,
		})
		if m.ov == overlayContextMenu {
			t.Fatalf("click on item %d should have dispatched (overlay closed), still open", i)
		}
		if dispatched != i {
			t.Fatalf("click at visual item %d dispatched index %d (offset bug)", i, dispatched)
		}
	}
}

// TestContextMenuClickOutsideCloses verifies clicks outside the item area (e.g.
// on the title or border) do not dispatch an item.
// TestDoubleClickTolerance verifies that a double-click is recognized even
// when the second press lands a few cells away from the first (realistic
// human behavior). The tolerance is doubleClickTolerance (default 3).
// Each offset is tested in isolation because a successful double-click
// replaces the tab (newTab), which would leave subsequent iterations on an
// empty/invalid tab.
func TestDoubleClickTolerance(t *testing.T) {
	baseX := 2
	baseY := rowList // entry 0
	for dx := -doubleClickTolerance; dx <= doubleClickTolerance; dx++ {
		if dx == 0 {
			continue // exact same cell covered by TestDoubleClickEntersDir
		}
		m := newTestModel()
		m.width = 80
		m.height = 24
		t2 := &tab{
			path:     "C:\\root",
			offset:   0,
			cursor:   0,
			entries:  makeDirEntries(10),
			selected: make(map[string]bool),
		}
		m.panes[0].tabs[0] = t2
		m.active = 0

		// First click at base — should only move cursor, not enter.
		m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: baseX, Y: baseY})
		if m.panes[0].current().path != "C:\\root" {
			t.Fatalf("single click at (%d,%d) should not enter, path=%s", baseX, baseY, m.panes[0].current().path)
		}
		// Second click within tolerance — should trigger double-click enter.
		m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: baseX + dx, Y: baseY})
		cur := m.panes[0].current()
		if cur.path != "dir0" {
			t.Fatalf("double-click at (%d+%d,%d) should enter dir0, path=%s (cursor=%d)",
				baseX, dx, baseY, cur.path, cur.cursor)
		}
	}
}

func TestContextMenuClickOutsideCloses(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	m.ctxItems = make([]ctxItem, 4)
	m.ctxX = 4
	m.ctxY = 20
	m.ov = overlayContextMenu

	top := m.contextMenuTop()
	// Click on the title line (top+1) or top border (top) => no dispatch.
	m.handleContextMenuMouse(tea.MouseMsg{
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
		X:      m.ctxPanelLeft() + 2,
		Y:      top + 1, // title row, not an item
	})
	if m.ov != overlayNone {
		t.Fatalf("click on title row should close without dispatch, ov=%d", m.ov)
	}
}

func itemLabels(items []ctxItem) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.label)
	}
	return out
}

func entryName(i int) string {
	if i%2 == 0 {
		return "dir" + itoa(i)
	}
	return "file" + itoa(i) + ".txt"
}

// makeDirEntries builds n entries where entry 0 is a directory (so a
// double-click on it exercises the in-TUI enter branch without spawning an
// external viewer), the rest are files.
func makeDirEntries(n int) []fs.Entry {
	out := make([]fs.Entry, 0, n)
	for i := 0; i < n; i++ {
		isDir := i == 0
		name := "dir0"
		if !isDir {
			name = "file" + itoa(i) + ".txt"
		}
		out = append(out, fs.Entry{Name: name, IsDir: isDir, Path: name})
	}
	return out
}

func TestSingleClickDoesNotEnterDir(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	t2 := &tab{
		path:     "C:\\root",
		offset:   0,
		cursor:   0,
		entries:  makeDirEntries(10),
		selected: make(map[string]bool),
	}
	m.panes[0].tabs[0] = t2
	m.active = 0

	// One click on the first row (a directory) must only move the cursor.
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 2, Y: rowList})
	if t2.cursor != 0 {
		t.Fatalf("single click should set cursor=0, got %d", t2.cursor)
	}
	if t2.path != "C:\\root" {
		t.Fatalf("single click must not enter the directory, path=%s", t2.path)
	}
}

func TestDoubleClickEntersDir(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	t2 := &tab{
		path:     "C:\\root",
		offset:   0,
		cursor:   0,
		entries:  makeDirEntries(10),
		selected: make(map[string]bool),
	}
	m.panes[0].tabs[0] = t2
	m.active = 0

	click := tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 2, Y: rowList}
	m.handleMouse(click)        // first click: cursor to row 0
	m.handleMouse(click)        // second click on same cell <= 500ms: double-click => enter dir0
	if m.panes[0].tabs[0].path != "dir0" {
		t.Fatalf("double-click on a directory should enter it, path=%s (cursor=%d)",
			m.panes[0].tabs[0].path, m.panes[0].tabs[0].cursor)
	}
}

func TestDoubleClickRequiresSameCell(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	t2 := &tab{
		path:     "C:\\root",
		offset:   0,
		cursor:   0,
		entries:  makeDirEntries(10),
		selected: make(map[string]bool),
	}
	m.panes[0].tabs[0] = t2
	m.active = 0

	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 2, Y: rowList})
	// Second press on a DIFFERENT cell is a new single click, not a double.
	m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 2, Y: rowList + 1})
	if m.panes[0].tabs[0].path != "C:\\root" {
		t.Fatalf("clicking two different cells must not trigger double-click enter, path=%s",
			m.panes[0].tabs[0].path)
	}
}

func TestDoubleClickExpiresAfterThreshold(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	t2 := &tab{
		path:     "C:\\root",
		offset:   0,
		cursor:   0,
		entries:  makeDirEntries(10),
		selected: make(map[string]bool),
	}
	m.panes[0].tabs[0] = t2
	m.active = 0

	click := tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: 2, Y: rowList}
	m.handleMouse(click)
	// Simulate a stale first click by rewinding lastClickT beyond the window.
	m.lastClickT = time.Now().Add(-time.Second)
	m.handleMouse(click)
	if m.panes[0].tabs[0].path != "C:\\root" {
		t.Fatalf("a click outside the double-click window must not enter, path=%s",
			m.panes[0].tabs[0].path)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
