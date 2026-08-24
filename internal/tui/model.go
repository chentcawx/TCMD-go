package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbletea"
	"tcmd/internal/fs"
)

// SortField identifies the column used for sorting in a tab's file listing.
type SortField int

const (
	SortByName SortField = iota
	SortByModTime
	SortBySize
)

// tab is one open directory within a pane. A pane holds several tabs, which is
// the multi-tab feature mirroring Total Commander's per-side tabs.
type tab struct {
	path        string
	entries     []fs.Entry
	cursor      int                   // index of the highlighted row in entries
	selected    map[string]bool       // set of selected entry paths
	offset      int                   // first visible row (scroll position)
	hOffset     int                   // horizontal scroll offset (column offset for wide filenames)
	loadErr     error                 // non-nil when the last reload failed
	loading     bool                  // true while async reload is in progress
	loadingMsg  string                // status message shown during loading
	_reloadCh   chan tabReloadMsg     // internal channel for async reload result

	// Sorting state: applied after every load so the listing is always sorted.
	// Default: name ascending.
	sortField SortField
	sortAsc   bool

	// Quick-locate accumulator: when the user presses a letter key while
	// browsing, the next letter (if any) appends to this buffer and the
	// cursor jumps to the first entry whose Name starts with the full buffer
	// (case-insensitive). Non-letter keys clear the buffer.
	lastQuickType []rune
}

func newTab(path string) *tab {
	t := &tab{path: path, selected: make(map[string]bool), sortField: SortByName, sortAsc: true}
	// Start async reload; t.entries stays nil until completion.
	t.asyncReload()
	return t
}

// asyncReload starts a goroutine to load the directory listing asynchronously.
// The result is delivered via a global channel that the Update loop polls.
func (t *tab) asyncReload() {
	if t.loading {
		return // already loading
	}
	t.loading = true
	t.loadingMsg = "加载中..."
	t.loadErr = nil
	t._reloadCh = make(chan tabReloadMsg, 1)
	go func() {
		defer close(t._reloadCh)
		entries, err := fs.ListDir(t.path)
		t._reloadCh <- tabReloadMsg{tab: t, entries: entries, err: err}
	}()
}

// checkReloadResult checks if there's a pending reload result for this tab.
// Returns true if a result was found and applied.
func (t *tab) checkReloadResult() bool {
	if !t.loading || t._reloadCh == nil {
		return false
	}
	select {
	case msg, ok := <-t._reloadCh:
		if !ok {
			t.loading = false
			return false
		}
		t.applyReloadResult(msg.entries, msg.err)
		return true
	default:
		return false
	}
}

// waitForLoading blocks until the tab finishes loading (or times out).
// Only for use in tests.
func (t *tab) waitForLoading(timeout time.Duration) bool {
	deadline := time.After(timeout)
	for t.loading {
		select {
		case <-deadline:
			return false
		default:
			// Poll for result.
			t.checkReloadResult()
			time.Sleep(10 * time.Millisecond)
		}
	}
	return true
}
func (t *tab) applyReloadResult(entries []fs.Entry, err error) {
	t.loading = false
	t.loadingMsg = ""
	if err != nil {
		t.loadErr = err
		return
	}
	t.loadErr = nil
	t.entries = sortAndClampCursor(t.entries, entries, t.cursor, t.sortField, t.sortAsc)
	if t.cursor >= len(t.entries) {
		t.cursor = len(t.entries) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
}

// sortAndClampCursor sorts entries by the requested field/direction and then
// clamps cursor to the new length. Used when a fresh listing replaces the
// previous one so the sort always takes effect immediately.
func sortAndClampCursor(prev []fs.Entry, next []fs.Entry, cursor int, field SortField, asc bool) []fs.Entry {
	out := make([]fs.Entry, len(next))
	copy(out, next)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].IsDir != out[j].IsDir {
			return out[i].IsDir // dirs first
		}
		var less bool
		switch field {
		case SortByModTime:
			less = out[i].ModTime.Before(out[j].ModTime)
		case SortBySize:
			less = out[i].Size < out[j].Size
		default:
			less = strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
		}
		if !asc {
			less = !less
		}
		return less
	})
	if cursor >= len(out) {
		cursor = len(out) - 1
	}
	// Try to keep the same item under the cursor by name (stable for re-sorts).
	if cursor >= 0 && len(prev) > 0 {
		target := prev[cursor].Name
		for i, e := range out {
			if strings.EqualFold(e.Name, target) {
				cursor = i
				break
			}
		}
	}
	if cursor < 0 {
		cursor = 0
	}
	return out
}

// tabReloadMsg is the bubbletea message delivered after async reload completes.
type tabReloadMsg struct {
	tab     *tab
	entries []fs.Entry
	err     error
}

// newTabAt creates a tab for path and, if focusName is non-empty and matches
// an entry in the listing, positions the cursor on that entry. Used when going
// up a directory so the cursor lands on the directory we just left rather than
// jumping back to the first row.
//
// Because newTab starts an async reload, newTabAt must wait for the load to
// complete before it can resolve the focus name.
func newTabAt(path, focusName string) *tab {
	t := newTab(path)
	if focusName == "" {
		return t
	}
	// Wait for async load to finish so we can find the focus name.
	t.waitForLoading(5 * time.Second)
	for i, e := range t.entries {
		if e.Name == focusName {
			t.cursor = i
			break
		}
	}
	return t
}

// reload re-reads the directory from disk synchronously. Kept for backward
// compatibility and for cases where async loading isn't appropriate (e.g.,
// closeCurrentTab on last tab). For new tabs and user-triggered refreshes,
// prefer asyncReload() to keep the TUI responsive.
func (t *tab) reload() {
	entries, err := fs.ListDir(t.path)
	if err != nil {
		t.loadErr = err
		return
	}
	t.loadErr = nil
	t.entries = entries
	if t.cursor >= len(entries) {
		t.cursor = len(entries) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
}

// pane is one of the two side-by-side panels; it owns its own tab stack.
type pane struct {
	tabs   []*tab
	active int
}

func newPane(path string) *pane {
	return &pane{tabs: []*tab{newTab(path)}, active: 0}
}

func (p *pane) current() *tab { return p.tabs[p.active] }

func (p *pane) addTab(path string) {
	p.tabs = append(p.tabs, newTab(path))
	p.active = len(p.tabs) - 1
}

// closeCurrentTab closes the active tab, or reloads it when it is the last one
// (matching Total Commander, which never closes the only remaining tab).
func (p *pane) closeCurrentTab() {
	if len(p.tabs) <= 1 {
		p.current().reload()
		return
	}
	p.tabs = append(p.tabs[:p.active], p.tabs[p.active+1:]...)
	if p.active >= len(p.tabs) {
		p.active = len(p.tabs) - 1
	}
}

// overlayKind enumerates the modal layers drawn on top of the panes.
type overlayKind int

const (
	overlayNone overlayKind = iota
	overlayConfirm
	overlayInput
	overlayViewer
	overlayBatchRename
	overlayContextMenu
	overlayDrivePicker
	overlayTree    // directory tree viewer (F3 on a directory)
	overlayQueue  // bottom progress bar for in-flight copy/move jobs
	overlayAssoc  // extension -> custom application editor (Ctrl+E)
)

// model is the root bubbletea model: both panes plus any active modal overlay.
type model struct {
	panes  [2]*pane
	active int // 0 = left, 1 = right

	ov          overlayKind
	confirmMsg  string
	confirmYes  func()
	inputPrompt string
	inputValue  string
	inputCursor int
	inputCommit func(string)

	// assoc maps each action ("view"/"edit"/"open") to an extension->command
	// map, driving the custom-application feature for F3/F4/Enter. Loaded from
	// and persisted to tcmd.json; empty when the user has set no bindings.
	assoc map[string]map[string]string

	// association editor overlay state (Ctrl+E).
	assocActionIdx int    // index into assocActions() for the active tab
	assocCursor    int    // highlighted row in the current action's binding list
	assocPhase     int    // 0 = list view, 1 = entering extension, 2 = entering command
	assocExtDraft  string // extension typed so far when adding a binding
	assocMsg       string // transient note shown at the bottom of the editor

	viewerPath   string
	viewerLines  []string
	viewerScroll int

	// batch rename overlay state. brField indexes the focused form field:
	// 0 前缀 1 后缀 2 查找 3 替换 4 序号开关 5 起始值 6 位数.
	brFiles   []fs.Entry
	brPrefix  string
	brSuffix  string
	brSearch  string
	brReplace string
	brCounter bool
	brStart   int
	brWidth   int
	brField   int
	brCursor  int // rune cursor within the active text field
	brScroll  int // preview scroll position
	brError   string

	// right-click context menu overlay state. ctxItems is the rendered menu;
	// the menu is anchored at (ctxX, ctxY) and grows upward so it never leaves
	// the viewport. ctxDir is the directory the menu acts on.
	ctxItems []ctxItem
	ctxX     int
	ctxY     int
	ctxIndex int
	ctxDir   string

	// double-click detection (terminals report no native double-click event, so
	// we measure the gap between two Left presses on the same cell).
	lastClickX int
	lastClickY int
	lastClickT time.Time

	// sort column click detection: tracks the last x-position and timestamp
	// of a sort-column click within the list header (rowList) so a second
	// click on the same column reverses direction.
	lastSortClickX     int
	lastSortClickTime  time.Time

	// drive picker overlay state. drives is the list of available drive roots
	// (e.g. ["C:\\", "D:\\"]); pickerIndex is the currently highlighted item.
	drives     []string
	pickerIndex int

	// copy/move job queue (background worker pool). nil until the first
	// operation is enqueued; created lazily to avoid starting goroutines when
	// the user never copies or moves. queueStatus tracks the overlay display
	// state: 0 = hidden (status line), 1 = visible (overlayQueue).
	queue      *JobQueue
	queueStatus int // 0 hidden / 1 visible
	paused    bool // global pause: new jobs still enqueue, existing jobs yield

	// directory tree overlay state. treeRoot is the root of the async-built
	// tree; treeCursor tracks the flattened index into the visible rows so
	// the user can navigate with ↑↓. treePath is the directory currently
	// being inspected (for Backspace to return to parent). treeHistory
	// remembers visited directories for stack-based back navigation.
	treeRoot     *treeDir
	treeFlat     []*treeDir // pre-order list of visible nodes (excludes root summary), indexed by treeCursor
	treeCursor   int
	treeScroll   int // vertical scroll offset for tree overlay (rows above the viewport)
	treePath     string
	treeHistory  []string // parent paths, top = closest ancestor
	treeLoading  bool     // true while async stat is in flight
	status string // transient status line
	width  int
	height int

	quitting  bool
	lastCmd   tea.Cmd // one-shot command from the last Init(); cleared after first fire
}

// InitialModel builds the starting application state. Both panes open the user
// home directory so the dual-pane layout is visible immediately on launch.
func InitialModel() *model {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		if wd, werr := os.Getwd(); werr == nil {
			home = wd
		} else {
			home = "."
		}
	}
	return &model{
		panes:  [2]*pane{newPane(home), newPane(home)},
		active: 0,
		width:  80,
		height: 24,
		assoc:  make(map[string]map[string]string),
		status: "Tab 切换面板 · ↑↓ 移动 · Enter 进入/打开 · Backspace 上级 · F2 批量重命名 · Alt+F7 命令行 · F5 复制 · F7 新建 · F8 删除 · Ctrl+E 关联应用 · ? 帮助",
	}
}

// homeDir returns the user home directory, falling back to the working
// directory and then "." so the app never fails to start.
func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return home
	}
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return "."
}

// ---- navigation / selection (pure state) ----

func (m *model) curPane() *pane { return m.panes[m.active] }
func (m *model) curTab() *tab   { return m.curPane().current() }
func (m *model) otherPane() *pane { return m.panes[1-m.active] }

// quickLocate appends r to the current tab's quick-locate buffer and jumps to
// the first entry whose Name starts with that buffer (case-insensitive). The
// search wraps around: if nothing follows the current cursor, it restarts at
// index 0. Non-letter input on the tab clears the buffer (handled by caller).
func (m *model) quickLocate(r rune) {
	t := m.curTab()
	if len(t.entries) == 0 {
		return
	}
	// Accumulate typed letters; clamp to a sensible buffer so a slow typer
	// doesn't build an unreasonably long prefix across unrelated keystrokes.
	t.lastQuickType = append(t.lastQuickType, r)
	if len(t.lastQuickType) > 8 {
		t.lastQuickType = t.lastQuickType[len(t.lastQuickType)-8:]
	}
	// Search forward from cursor+1; wrap around if not found.
	prefix := string(t.lastQuickType)
	start := (t.cursor + 1) % len(t.entries)
	for i := 0; i < len(t.entries); i++ {
		idx := (start + i) % len(t.entries)
		if strings.HasPrefix(strings.ToLower(t.entries[idx].Name), prefix) {
			t.cursor = idx
			m.ensureCursorVisible(t)
			return
		}
	}
	// Nothing matched — shake the buffer so the next letter starts fresh.
	t.lastQuickType = t.lastQuickType[:0]
}

// cycleSortField advances sortField by +1 mod 3 (name → date → size → name…)
// and resets the cursor to 0 so the user sees the new top immediately.
func (m *model) cycleSortField() {
	t := m.curTab()
	t.sortField = (t.sortField + 1) % 3
	t.applyCurrentSort()
	t.cursor = 0
	t.offset = 0
	m.status = sortFieldLabel(t.sortField) + " 正序"
}

// reverseSort toggles sortAsc and re-applies. No cursor change.
func (m *model) reverseSort() {
	t := m.curTab()
	t.sortAsc = !t.sortAsc
	t.applyCurrentSort()
	t.cursor = 0
	t.offset = 0
	m.status = sortFieldLabel(t.sortField) + " " + func() string { if t.sortAsc { return "正序" }; return "反序" }()
}

// applyCurrentSort re-sorts the current tab's entries in place using the
// tab's current sort field/direction, then clamps cursor.
func (t *tab) applyCurrentSort() {
	t.entries = sortAndClampCursor(nil, t.entries, t.cursor, t.sortField, t.sortAsc)
}

// clearQuickType drops the accumulated quick-locate buffer so the next
// letter starts a fresh search instead of extending a stale one.
func (t *tab) clearQuickType() {
	t.lastQuickType = t.lastQuickType[:0]
}

// sortFieldLabel returns a short human-readable label for the field enum.
func sortFieldLabel(f SortField) string {
	switch f {
	case SortByName:
		return "名称"
	case SortByModTime:
		return "日期"
	case SortBySize:
		return "大小"
	default:
		return "名称"
	}
}

// timeFmt is the display format for the time column. Picked as "01-02 15:04"
// (11 display cells) instead of the fuller "2006-01-02 15:04" (16 cells) so
// narrow panes (>=30 cols) still have room for name+size+time without
// overflowing. The year is rarely needed in a file-listing context; month+day
// + 24h time is sufficient for sorting and scanning.
const timeFmt = "01-02 15:04"

// formatTime returns the entry's ModTime formatted for the time column, or
// a blank 11-cell placeholder when ModTime is the zero value (matching
// timeFieldW in view.go so padLeftDW never inflates the row width).
func formatTime(e fs.Entry) string {
	if e.ModTime.IsZero() {
		return strings.Repeat(" ", 11)
	}
	return e.ModTime.Format(timeFmt)
}

func (m *model) moveCursor(d int) {
	t := m.curTab()
	if len(t.entries) == 0 {
		return
	}
	t.cursor += d
	if t.cursor < 0 {
		t.cursor = 0
	}
	if t.cursor >= len(t.entries) {
		t.cursor = len(t.entries) - 1
	}
}

// pageMove scrolls by roughly half a screen so PgUp/PgDn feel native.
func (m *model) pageMove(d int) {
	page := 10
	if m.height > 0 {
		if p := (m.height - 6) / 2; p > 0 {
			page = p
		}
	}
	m.moveCursor(d * page)
}

// incrementRow moves the cursor by exactly d rows (d > 0 = down, d < 0 = up).
// Used by mouse wheel events so each wheel tick advances the highlight by one
// file rather than jumping by half a screen.
func (m *model) incrementRow(d int) {
	m.moveCursor(d)
}

func (m *model) toggleSelect() {
	t := m.curTab()
	if len(t.entries) == 0 {
		return
	}
	e := t.entries[t.cursor]
	if t.selected[e.Path] {
		delete(t.selected, e.Path)
	} else {
		t.selected[e.Path] = true
	}
	// Stay on the same row so a second Space toggles the mark back off, and
	// the highlight does not jump away from what the user just acted on.
}

func (m *model) selectAll() {
	for _, e := range m.curTab().entries {
		m.curTab().selected[e.Path] = true
	}
}

func (m *model) clearSelection() {
	m.curTab().selected = make(map[string]bool)
}

func (m *model) invertSelection() {
	t := m.curTab()
	for _, e := range t.entries {
		if t.selected[e.Path] {
			delete(t.selected, e.Path)
		} else {
			t.selected[e.Path] = true
		}
	}
}

// enterDir replaces the current tab's directory with the highlighted entry when
// it is a directory. A file is opened with the OS default association (see
// openFile in update.go). Either way the session is persisted.
func (m *model) enterDir() {
	t := m.curTab()
	if t.loading {
		m.status = "目录加载中，请稍候..."
		return
	}
	if len(t.entries) == 0 {
		return
	}
	e := t.entries[t.cursor]
	if !e.IsDir {
		// Enter on a file: prefer a custom "open" association; fall back to the
		// system default (ShellExecute "open").
		if cmd, ok := m.ResolveAssoc(AssocOpen, extOf(e.Name)); ok {
			m.launchAssoc(AssocOpen, cmd, e.Path)
			return
		}
		if err := openFile(e.Path); err != nil {
			m.status = "打开失败: " + err.Error()
		} else {
			m.status = "已用默认程序打开: " + e.Name
		}
		return
	}
	m.curPane().tabs[m.curPane().active] = newTab(e.Path)
	m.curTab().clearQuickType()
	m.saveConfig()
}

func (m *model) upDir() {
	t := m.curTab()
	parent := filepath.Dir(t.path)
	if parent == t.path {
		return
	}
	// Land the cursor on the directory we just left, so returning up does not
	// dump the highlight back to the first row.
	child := filepath.Base(t.path)
	m.curPane().tabs[m.curPane().active] = newTabAt(parent, child)
	m.curTab().clearQuickType()
	m.saveConfig()
}

func (m *model) newTabHere() {
	m.curPane().addTab(m.curTab().path)
	m.curTab().clearQuickType()
	m.saveConfig()
}

func (m *model) switchTab(d int) {
	p := m.curPane()
	n := len(p.tabs)
	if n <= 1 {
		return
	}
	p.active = (p.active + d + n) % n
	m.curTab().clearQuickType()
	m.saveConfig()
}

func (m *model) reloadCurrent() {
	m.curTab().asyncReload()
}

// selectedOrCurrent returns the selected paths, or the current row if nothing
// is selected. An empty slice means "nothing to act on".
func (m *model) selectedOrCurrent() []string {
	t := m.curTab()
	if len(t.selected) > 0 {
		out := make([]string, 0, len(t.selected))
		for _, e := range t.entries {
			if t.selected[e.Path] {
				out = append(out, e.Path)
			}
		}
		return out
	}
	if len(t.entries) > 0 {
		return []string{t.entries[t.cursor].Path}
	}
	return nil
}

func (m *model) reloadBoth() {
	for _, p := range m.panes {
		p.current().asyncReload()
	}
}

// reapplySort re-sorts the active tab using its current field/direction, then
// clamps the cursor. Called when the user explicitly requests a sort change.
func (m *model) reapplySort() {
	t := m.curTab()
	t.applyCurrentSort()
	if t.cursor >= len(t.entries) {
		t.cursor = len(t.entries) - 1
	}
	if t.cursor < 0 {
		t.cursor = 0
	}
}

// ---- overlay helpers ----

func (m *model) openInput(prompt, def string, commit func(string)) {
	m.ov = overlayInput
	m.inputPrompt = prompt
	m.inputValue = def
	m.inputCursor = len([]rune(def)) // rune index, not byte index (CJK-safe)
	m.inputCommit = commit
}

func (m *model) openConfirm(msg string, yes func()) {
	m.ov = overlayConfirm
	m.confirmMsg = msg
	m.confirmYes = yes
}

// beginAssocEditor opens the extension->application association manager. It
// starts on the "view" tab with the cursor at the top of the binding list.
func (m *model) beginAssocEditor() {
	m.ov = overlayAssoc
	m.assocActionIdx = 0
	m.assocCursor = 0
	m.assocPhase = 0
	m.assocExtDraft = ""
	m.assocMsg = ""
}

// openViewer loads a text file into the built-in read-only viewer, or falls
// back to a status message for binaries / oversized files (to avoid memory
// blow-up or mojibake).
func (m *model) openViewer(path string) {
	fi, err := os.Stat(path)
	if err != nil {
		m.status = "无法查看: " + err.Error()
		return
	}
	if fi.Size() > maxViewBytes {
		m.status = "文件过大，无法内置查看"
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		m.status = "无法读取: " + err.Error()
		return
	}
	if isBinary(data) {
		m.status = "二进制文件，无法内置查看（可用外部编辑器打开）"
		return
	}
	m.ov = overlayViewer
	m.viewerPath = path
	m.viewerLines = splitLines(data)
	m.viewerScroll = 0
}

// beginDrivePicker opens the drive-letter picker for the active pane. It
// enumerates available drives once and anchors the selection at the current
// drive (or the first drive if the current path doesn't start with a drive
// root — common when browsing inside a archive or a network share).
func (m *model) beginDrivePicker() {
	m.drives = listDrives()
	if len(m.drives) == 0 {
		m.status = "未检测到可用盘符"
		return
	}
	// Anchor the highlight on the drive the active pane is currently rooted
	// at, falling back to the first drive.
	curPath := m.curTab().path
	m.pickerIndex = 0
	for i, d := range m.drives {
		if strings.HasPrefix(curPath, d) {
			m.pickerIndex = i
			break
		}
	}
	m.ov = overlayDrivePicker
}

// switchToDrive navigates the active pane to the selected drive root.
func (m *model) switchToDrive(drive string) {
	m.curPane().tabs[m.curPane().active] = newTab(drive)
	m.saveConfig()
	m.status = "已切换到盘符: " + drive
}

func (m *model) closeOverlay() {
	m.ov = overlayNone
	m.confirmYes = nil
	m.inputCommit = nil
	m.inputPrompt = ""
	m.inputValue = ""
	m.inputCursor = 0
	m.drives = nil
	m.pickerIndex = 0
	m.ctxItems = nil
	m.ctxIndex = 0
	m.ctxX = 0
	m.ctxY = 0
	m.ctxDir = ""
	// Association editor transient state.
	m.assocActionIdx = 0
	m.assocCursor = 0
	m.assocPhase = 0
	m.assocExtDraft = ""
	m.assocMsg = ""
	// Text viewer transient state — reset so a subsequent viewer open starts
	// clean without stale scroll/line data lingering in the model.
	m.viewerPath = ""
	m.viewerLines = nil
	m.viewerScroll = 0
	// Leave queueStatus alone: the queue overlay is toggled via its own key
	// handler, not via closeOverlay.
}

// QueueStop shuts down the background job queue and waits for in-flight jobs
// to finish (up to 30 s). Safe to call multiple times; a nil queue is a no-op.
func (m *model) QueueStop() {
	if m.queue == nil {
		return
	}
	m.queue.Stop()
}
