package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Init kicks off the async-reload watch loop and primes the bubbletea
// event loop so the very first paint always has a valid (width, height) —
// even on terminals that, for whatever reason, never deliver their own
// initial WindowSizeMsg.
//
// Why a continuous tick and not just one:
//   - bubbletea ONLY drives Update in response to messages on its msgs
//     channel. The async-reload goroutine posts results to t._reloadCh,
//     NOT to that channel.
//   - Without an active timer, the very first reloadTick could fire BEFORE
//     fs.ListDir returns (checkReloadResult's select-default hits), and
//     then nothing in the system would ever drain t._reloadCh again —
//     every tab would be stuck on "加载中..." forever.
//   - 50 ms is well below human perception (~20 fps) and the channel is
//     empty in 99 % of frames, so the cost is negligible.
//
// On every reloadTick we also reschedule tickReloadCmd itself, so the
// loop self-perpetuates until the program exits.
func (m *model) Init() tea.Cmd {
	return tickReloadCmd()
}

// tickReloadCmd is the unit-schedule for our reload watcher: it fires one
// reloadTickMsg reloadTickInterval from now. The Update loop reschedules
// it after each tick, so the timer chain self-perpetuates.
func tickReloadCmd() tea.Cmd {
	return tea.Tick(reloadTickInterval, func(time.Time) tea.Msg {
		return reloadTickMsg{}
	})
}

// reloadTickInterval is how often the watch loop polls for async reload
// results. 50 ms is plenty fast for a human eye (~20 fps) and keeps CPU
// idle when nothing is loading.
const reloadTickInterval = 50 * time.Millisecond

// reloadTickMsg is an internal marker message that triggers
// drainReloadResults on all tabs and reschedules itself for the next tick.
type reloadTickMsg struct{}

// Update is the single entry point for all messages.
//
// Two invariants matter here:
//  1. EVERY message path must call drainReloadResults before returning,
//     otherwise an async reload that lands on t._reloadCh between two
//     Update calls (the common case: user pressed a key right after the
//     fs.ListDir goroutine completed) will wait for the next event —
//     potentially forever. Drain-on-every-message is robust without
//     having to invent a separate watcher goroutine.
//  2. reloadTickMsg must reschedule tickReloadCmd so the watch loop is
//     self-perpetuating. Without the reschedule, the timer fires exactly
//     once (Init) and the chain dies.
//
// handleKey / handleMouse return (Model, Cmd) per the bubbletea idiom,
// so we have to merge their returned model into `m` (in case a handler
// swapped the model — none do today, but the indirection is required
// by the interface) and still drain.
func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.drainReloadResults()
		return m, nil
	case tea.KeyMsg:
		out, c := m.handleKey(msg)
		m2 := asModel(out)
		m2.drainReloadResults()
		return m2, c
	case tea.MouseMsg:
		out, c := m.handleMouse(msg)
		m2 := asModel(out)
		m2.drainReloadResults()
		return m2, c
	case treeStats:
		m.treeLoading = false
		if msg.err != nil {
			m.ov = overlayNone
			m.status = "目录读取失败: " + msg.err.Error()
		} else {
			m.treeRoot = msg.root
			m.treeFlat = flattenTree(msg.root)
			m.treeCursor = 0
		}
		m.drainReloadResults()
		return m, nil
	case reloadTickMsg:
		// Self-perpetuating tick: drain pending reloads, then reschedule
		// the next tick so the watch loop never dies.
		m.drainReloadResults()
		return m, tickReloadCmd()
	}
	// Catch-all for any future message type — still drain.
	m.drainReloadResults()
	return m, cmd
}

// asModel narrows a bubbletea Model return value back to *model. Every
// handler in this file returns the *model receiver unchanged, so the type
// assertion always succeeds today. The helper keeps Update tidy in case a
// future handler decides to return a different concrete type — Update
// would still operate on the original m, which keeps the rest of the
// program (View, persistence) sound.
func asModel(m tea.Model) *model {
	if mm, ok := m.(*model); ok {
		return mm
	}
	return m.(*model) // the contract is that bubbletea.Model == *model
}

// drainReloadResults walks every pane and tab and applies any pending
// async-reload result sitting on t._reloadCh. It's idempotent (a select
// with default) and cheap when the channel is empty, so calling it on
// every Update is safe.
//
// Why a method on model and not free-standing: it touches t.entries,
// t.cursor, t.loading, t.loadErr and t._reloadCh — the inner data race
// between the goroutine that wrote to t._reloadCh and the goroutine now
// reading is the reason checkReloadResult uses a buffered (cap 1)
// channel and non-blocking receive. As long as that contract holds, the
// model can be shared safely between the bubbletea loop and the reader
// goroutine.
//
// A buffered cap=1 channel has another nice property: a single "miss"
// (reader too slow to drain within one timer tick) still keeps the next
// result available — we won't silently drop the load.
//
// Test paths build a partial model (e.g. only m.panes[0]), so we nil-guard
// each level here — better than sprinkling nil checks at every call site.
func (m *model) drainReloadResults() {
	for _, p := range m.panes {
		if p == nil {
			continue
		}
		for _, t := range p.tabs {
			if t == nil {
				continue
			}
			t.checkReloadResult()
		}
	}
}

// UpdateNoReturn is a variant of Update that does not return a new model or Cmd.
// It is used in tests to simulate the event loop without consuming the return value.
func (m *model) UpdateNoReturn(msg tea.Msg) {
	m.Update(msg)
}

// handleKey routes all incoming events to the right per-overlay handler.
// Each overlay has its own handler; adding a new overlay requires:
//   1. Adding a case to this switch (or the overlayKind-to-handler map below).
//   2. Implementing the corresponding handle*Key method.
//   3. Adding a case in View() to render the overlay.
func (m *model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		m.saveConfig()
		m.quitting = true
		return m, tea.Quit
	}
	// Overlay-key dispatch table: each overlayKind maps to its handler.
	// Staying as an explicit switch avoids an extra indirection and keeps
	// the call stack easy to follow during debugging.
	switch m.ov {
	case overlayConfirm:
		return m.handleConfirmKey(msg)
	case overlayInput:
		return m.handleInputKey(msg)
	case overlayViewer:
		return m.handleViewerKey(msg)
	case overlayBatchRename:
		return m.handleBatchRenameKey(msg)
	case overlayContextMenu:
		return m.handleContextMenuKey(msg)
	case overlayDrivePicker:
		return m.handleDrivePickerKey(msg)
	case overlayTree:
		return m.handleTreeViewKey(msg)
	case overlayQueue:
		return m.handleQueueKey(msg)
	case overlayAssoc:
		return m.handleAssocKey(msg)
	default:
		return m.handleNormalKey(msg)
	}
}

// handleNormalKey implements Total-Commander-style keybindings.
func (m *model) handleNormalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyTab:
		m.active = 1 - m.active
		m.saveConfig()
	case tea.KeyUp, tea.KeyCtrlP:
		m.moveCursor(-1)
	case tea.KeyDown, tea.KeyCtrlN:
		m.moveCursor(1)
	case tea.KeyPgUp:
		m.pageMove(-1)
	case tea.KeyPgDown:
		m.pageMove(1)
	case tea.KeyHome:
		m.curTab().cursor = 0
	case tea.KeyEnd:
		t := m.curTab()
		t.cursor = len(t.entries) - 1
	case tea.KeyEnter:
		m.enterDir()
	case tea.KeyBackspace:
		m.upDir()
	case tea.KeySpace, tea.KeyInsert:
		m.toggleSelect()
	case tea.KeyRunes:
		// A literal space is normally KeySpace, but during IME composition or
		// on some terminals bubbletea delivers it as KeyRunes{' '}. Handle both
		// so Space selection never silently becomes a no-op.
		if string(msg.Runes) == " " {
			m.toggleSelect()
		}
		// Quick-locate: any letter key accumulates into the current tab's
		// prefix buffer and jumps the cursor to the first entry whose name
		// starts with that prefix (case-insensitive). Non-letter runes clear
		// the buffer and fall through to the character-key handler below.
		rs := msg.Runes
		if len(rs) == 1 && rs[0] >= 'a' && rs[0] <= 'z' {
			m.quickLocate(rs[0])
		} else if len(rs) == 1 && rs[0] >= 'A' && rs[0] <= 'Z' {
			m.quickLocate(rs[0] + ('a' - 'A'))
		}
	case tea.KeyCtrlA:
		m.selectAll()
	case tea.KeyCtrlR:
		m.reloadCurrent()
	case tea.KeyCtrlT:
		m.newTabHere()
	case tea.KeyCtrlW:
		m.curPane().closeCurrentTab()
	case tea.KeyRight:
		if msg.Alt {
			m.switchTab(1)
		} else {
			m.moveCursor(1)
		}
	case tea.KeyLeft:
		if msg.Alt {
			m.switchTab(-1)
		}
	case tea.KeyF3:
		return m.beginView()
	case tea.KeyF4:
		m.beginEdit()
	case tea.KeyF2:
		m.beginBatchRename()
	case tea.KeyF5:
		m.beginCopy()
	case tea.KeyF6:
		m.beginMove()
	case tea.KeyF11:
		m.beginMoveWithLink()
	case tea.KeyF7:
		if msg.Alt {
			// Alt+F7 (the only modifier+F7 bubbletea can detect; a true
			// Ctrl+F7 is indistinguishable from plain F7 in the terminal
			// protocol) opens a standalone Command Prompt at the cursor's
			// directory and copies that path to the clipboard.
			m.beginCmdTerminal()
		} else {
			m.beginMkdir()
		}
	case tea.KeyF8, tea.KeyDelete:
		m.beginDelete()
	case tea.KeyCtrlD:
		// Ctrl+D opens the drive-letter picker for the active pane.
		m.beginDrivePicker()
	case tea.KeyCtrlE:
		// Ctrl+E opens the extension -> custom application association editor.
		m.beginAssocEditor()
	case tea.KeyEscape:
		if len(m.curTab().selected) > 0 {
			m.clearSelection()
		} else {
			m.openConfirm("退出 tcmd? (Y/N)", func() { m.quitting = true })
		}
	}

	// Character keys only (special keys already handled above).
	switch msg.String() {
	case ":":
		m.beginCommand()
	case "?":
		m.status = "Tab切换 · ↑↓移动 · Enter进入/打开 · Backspace上级 · Space选择 · Ctrl+A全选 · F2批量重命名 · Alt+F7命令行(复制路径) · F3查看 · F4编辑(可绑定) · F5复制 · F6移动 · F7新建 · F8删除 · F11移动+链接 · Ctrl+E/:assoc 扩展名关联应用 · Ctrl+T新标签 · Ctrl+W关标签 · Alt+1/2/3 排序 · Alt+R 反转 · :命令 · Esc取消"
	}
	// Sort hotkeys: alt+1/2/3 select sort field (name/date/size).
	// These are checked AFTER the switch so they don't interfere with the
	// character-key dispatch above.
	//
	// Alt+key combination arrives as KeyRunes with Alt:true flag.
	if msg.Type == tea.KeyRunes && msg.Alt && len(msg.Runes) == 1 {
		switch msg.Runes[0] {
		case '1': // alt+1 → name sort
			m.curTab().sortField = SortByName
			m.curTab().applyCurrentSort()
			m.curTab().cursor = 0
			m.curTab().offset = 0
			m.status = sortFieldLabel(SortByName) + " 正序"
		case '2': // alt+2 → date sort
			m.curTab().sortField = SortByModTime
			m.curTab().applyCurrentSort()
			m.curTab().cursor = 0
			m.curTab().offset = 0
			m.status = sortFieldLabel(SortByModTime) + " 正序"
		case '3': // alt+3 → size sort
			m.curTab().sortField = SortBySize
			m.curTab().applyCurrentSort()
			m.curTab().cursor = 0
			m.curTab().offset = 0
			m.status = sortFieldLabel(SortBySize) + " 正序"
		}
	}
	if msg.Type == tea.KeyRunes && msg.Alt && len(msg.Runes) == 1 && msg.Runes[0] == 'r' {
		m.reverseSort()
	}
	return m, nil
}

func (m *model) handleConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEscape {
		m.closeOverlay()
		return m, nil
	}
	switch msg.String() {
	case "y", "Y":
		yes := m.confirmYes
		m.closeOverlay()
		if yes != nil {
			yes()
		}
		if m.quitting {
			m.saveConfig()
			return m, tea.Quit
		}
	case "n", "N":
		m.closeOverlay()
	}
	return m, nil
}

func (m *model) handleInputKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.closeOverlay()
		return m, nil
	case tea.KeyEnter:
		val := m.inputValue
		commit := m.inputCommit
		m.closeOverlay()
		if commit != nil {
			commit(val)
		}
		return m, nil
	case tea.KeyBackspace:
		rs := []rune(m.inputValue)
		if m.inputCursor > 0 && m.inputCursor <= len(rs) {
			rs = append(rs[:m.inputCursor-1], rs[m.inputCursor:]...)
			m.inputValue = string(rs)
			m.inputCursor--
		}
	case tea.KeyLeft:
		if m.inputCursor > 0 {
			m.inputCursor--
		}
	case tea.KeyRight:
		if m.inputCursor < len([]rune(m.inputValue)) {
			m.inputCursor++
		}
	case tea.KeyHome:
		m.inputCursor = 0
	case tea.KeyEnd:
		m.inputCursor = len([]rune(m.inputValue))
	case tea.KeyRunes:
		// Composed text, including CJK from an IME, arrives as KeyRunes. Insert
		// it at the rune cursor; never insert control sequences.
		ins := []rune(msg.String())
		if len(ins) == 0 {
			return m, nil
		}
		rs := []rune(m.inputValue)
		if m.inputCursor < 0 {
			m.inputCursor = 0
		}
		if m.inputCursor > len(rs) {
			m.inputCursor = len(rs)
		}
		rs = append(rs[:m.inputCursor], append(ins, rs[m.inputCursor:]...)...)
		m.inputValue = string(rs)
		m.inputCursor += len(ins)
	default:
		// Fallback for any other printable single rune not delivered as
		// KeyRunes (rare). Skip control characters.
		rs := []rune(msg.String())
		if len(rs) == 1 && rs[0] >= 32 {
			r := rs[0]
			cur := []rune(m.inputValue)
			if m.inputCursor < 0 {
				m.inputCursor = 0
			}
			if m.inputCursor > len(cur) {
				m.inputCursor = len(cur)
			}
			cur = append(cur[:m.inputCursor], append([]rune{r}, cur[m.inputCursor:]...)...)
			m.inputValue = string(cur)
			m.inputCursor++
		}
	}
	return m, nil
}

func (m *model) handleViewerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEscape || msg.Type == tea.KeyF3 || msg.String() == "q" {
		m.closeOverlay()
		return m, nil
	}
	switch msg.Type {
	case tea.KeyUp, tea.KeyCtrlP:
		if m.viewerScroll > 0 {
			m.viewerScroll--
		}
	case tea.KeyDown, tea.KeyCtrlN:
		m.viewerScroll++
	case tea.KeyPgUp:
		m.viewerScroll -= (m.height - 4)
		if m.viewerScroll < 0 {
			m.viewerScroll = 0
		}
	case tea.KeyPgDown:
		m.viewerScroll += (m.height - 4)
		// Clamp to the last visible window so the viewer never scrolls past
		// the end of the file. The original code mistakenly checked < 0 here
		// (a copy-paste from KeyPgUp), which was dead code for PgDown.
		maxScroll := len(m.viewerLines) - (m.height - 4)
		if maxScroll < 0 {
			maxScroll = 0
		}
		if m.viewerScroll > maxScroll {
			m.viewerScroll = maxScroll
		}
	}
	return m, nil
}

// ---- operation triggers (open the right overlay; execution happens on commit) ----

func (m *model) beginCopy() {
	srcs := m.selectedOrCurrent()
	if len(srcs) == 0 {
		m.status = "没有可复制的项"
		return
	}
	dst := m.otherPane().current().path
	m.confirmOp(JobCopy, srcs, dst)
}

func (m *model) beginMove() {
	srcs := m.selectedOrCurrent()
	if len(srcs) == 0 {
		m.status = "没有可移动的项"
		return
	}
	dst := m.otherPane().current().path
	m.confirmOp(JobMove, srcs, dst)
}

func (m *model) beginMoveWithLink() {
	srcs := m.selectedOrCurrent()
	if len(srcs) == 0 {
		m.status = "没有可移动的项"
		return
	}
	dst := m.otherPane().current().path
	m.confirmOp(JobMoveWithLink, srcs, dst)
}

// confirmOp opens a confirmation overlay showing the operation type, source
// count, and destination. Y proceeds to enqueue; N/Esc cancels.
func (m *model) confirmOp(typ JobType, sources []string, dstDir string) {
	n := len(sources)
	first := ""
	if n > 0 {
		first = filepath.Base(sources[0])
	}
	msg := fmt.Sprintf("%s %d 项？\n\n源: %s%s\n目: %s",
		typ, n,
		first,
		func() string { if n > 1 { return fmt.Sprintf(" 等 %d 项", n) }; return "" }(),
		filepath.Base(dstDir),
	)
	m.openConfirm(msg, func() { m.enqueueOp(typ, sources, dstDir) })
}

// enqueueOp starts (or resumes) the job queue, enqueues the operation, and
// switches the overlay to overlayQueue so the user can watch progress.
func (m *model) enqueueOp(typ JobType, sources []string, dstDir string) {
	if m.queue == nil {
		m.queue = NewJobQueue()
		go m.queue.Run()
	}
	j, err := m.queue.Enqueue(typ, sources, dstDir)
	if err != nil {
		m.status = "入队失败: " + err.Error()
		return
	}
	m.ov = overlayQueue
	m.queueStatus = 1
	m.clearSelection()
	m.reloadBoth()
	m.status = fmt.Sprintf("已入队 %s 任务 #%d：%s", typ, j.id, jobSummary(j))
}

func (m *model) beginCmdTerminal() {
	// Open a standalone Command Prompt at the active pane's current directory
	// and copy that path to the clipboard. Both steps are best-effort: a
	// failure to open the terminal or write the clipboard is reported in the
	// status line but never aborts the TUI.
	dir := m.curTab().path
	clipErr := writeClipboard(dir)
	if err := openCmdTerminal(dir); err != nil {
		m.status = "打开命令行失败: " + err.Error()
		return
	}
	if clipErr != nil {
		m.status = "已打开命令行于: " + dir + "（剪贴板写入失败）"
		return
	}
	m.status = "已打开命令行于: " + dir + "（路径已复制到剪贴板）"
}

func (m *model) beginMkdir() {
	parent := m.curTab().path
	m.openInput("新建目录:", "", func(val string) {
		val = strings.TrimSpace(val)
		if val == "" {
			return
		}
		if err := makeDir(parent, val); err != nil {
			m.status = "新建失败: " + err.Error()
		} else {
			m.status = "已创建: " + val
			m.reloadCurrent()
		}
	})
}

func (m *model) beginDelete() {
	srcs := m.selectedOrCurrent()
	if len(srcs) == 0 {
		m.status = "没有可删除的项"
		return
	}
	m.openConfirm(fmt.Sprintf("确认删除 %d 项? (Y/N)", len(srcs)), func() {
		if err := deleteItems(srcs); err != nil {
			m.status = "删除失败: " + err.Error()
		} else {
			m.status = fmt.Sprintf("已删除 %d 项", len(srcs))
		}
		m.clearSelection()
		m.reloadBoth()
	})
}

func (m *model) beginView() (tea.Model, tea.Cmd) {
	t := m.curTab()
	if t.loading {
		m.status = "目录加载中，请稍候..."
		return m, nil
	}
	if len(t.entries) == 0 {
		return m, nil
	}
	e := t.entries[t.cursor]
	if e.IsDir {
		return m, m.openTreeView(e.Path)
	}
	// F3: if the extension has a custom "view" association, launch that app
	// instead of the built-in text viewer.
	if cmd, ok := m.ResolveAssoc(AssocView, extOf(e.Name)); ok {
		m.launchAssoc(AssocView, cmd, e.Path)
		return m, nil
	}
	m.openViewer(e.Path)
	return m, nil
}

// launchAssoc runs a custom-associated application on file and reports the
// outcome in the status line. It is the shared sink for F3/F4/Enter so the
// status wording stays consistent.
func (m *model) launchAssoc(action AssocAction, appCmd, file string) {
	if err := runWithFile(appCmd, file); err != nil {
		m.status = fmt.Sprintf("关联应用启动失败(%s): %s", assocActionLabel(action), err.Error())
		return
	}
	m.status = fmt.Sprintf("已用关联应用打开(%s): %s", assocActionLabel(action), baseName(file))
}

// beginEdit handles F4. For files with a custom "edit" association the bound
// app is launched; otherwise a stub notice is shown (built-in editor is a
// future feature) so the key never silently does nothing.
func (m *model) beginEdit() {
	t := m.curTab()
	if len(t.entries) == 0 {
		return
	}
	e := t.entries[t.cursor]
	if e.IsDir {
		m.status = "F4 编辑仅适用于文件"
		return
	}
	if cmd, ok := m.ResolveAssoc(AssocEdit, extOf(e.Name)); ok {
		m.launchAssoc(AssocEdit, cmd, e.Path)
		return
	}
	m.status = "F4 编辑将在后续版本提供（当前可用 F3 查看；可在 Ctrl+E 为扩展名绑定编辑器）"
}

// openTreeView starts an async stat of dir and opens the tree overlay.
// Returns a one-shot tea.Cmd that fires treeStats once the stat completes.
// openTreeView starts an async stat of dir and opens the tree overlay fresh:
// all prior tree state (including the history stack) is cleared. Used by F3.
func (m *model) openTreeView(dir string) tea.Cmd {
	m.treePath = dir
	m.treeRoot = nil
	m.treeFlat = nil
	m.treeCursor = 0
	m.treeHistory = nil
	m.treeLoading = true
	m.ov = overlayTree
	ch := make(chan treeStats, 1)
	go AsyncTreeStat(dir, ch)
	return func() tea.Msg {
		return <-ch
	}
}

// restatTree re-runs the async stat for dir WITHOUT clearing the history
// stack or cursor bookkeeping that the caller has already set up. Used when
// navigating into a subdirectory (Enter) or back to a parent (Backspace),
// where the caller pushes/pops treeHistory before calling this.
func (m *model) restatTree(dir string) tea.Cmd {
	m.treePath = dir
	m.treeRoot = nil
	m.treeFlat = nil
	m.treeCursor = 0
	m.treeLoading = true
	m.ov = overlayTree
	ch := make(chan treeStats, 1)
	go AsyncTreeStat(dir, ch)
	return func() tea.Msg {
		return <-ch
	}
}

func (m *model) beginCommand() {
	m.openInput("cmd> ", "", func(val string) {
		val = strings.TrimSpace(val)
		if val == "" {
			return
		}
		// The ":assoc" command opens the extension -> application editor, a
		// reliable alternative to Ctrl+E for terminals that don't deliver the
		// control combination reliably.
		if strings.EqualFold(val, "assoc") {
			m.beginAssocEditor()
			return
		}
		// A bare directory path jumps the active pane; otherwise run as a shell
		// command and surface its combined output in the status line.
		if fi, err := os.Stat(val); err == nil && fi.IsDir() {
			m.curPane().tabs[m.curPane().active] = newTab(val)
			return
		}
		// Validate before executing to prevent command injection.
		if err := sanitizeShellInput(val); err != nil {
			m.status = err.Error()
			return
		}
		out, err := runShell(val)
		if err != nil {
			m.status = fmt.Sprintf("命令错误: %v", err)
		} else {
			m.status = strings.TrimSpace(string(out))
		}
	})
}

// openFile launches the given path with the operating system's default
// association (the same action Explorer performs on a double-click / the
// "打开" context-menu verb). On Windows this calls ShellExecuteW with the
// "open" verb — the genuine system handler, not a `cmd /c start`
// approximation — so per-extension and UAC behavior match Explorer exactly.
// The external process is detached so the TUI is never blocked; the spawned
// helper is reaped in a background goroutine to avoid a zombie.
func openFile(path string) error {
	return shellOpen(path)
}

// dangerousPatterns lists substrings that must never appear in a :cmd input,
// regardless of quoting. This is a defense-in-depth layer on top of the
// explicit allowlist below; it catches sloppy attempts to pipe/redirect or
// chain commands even when the user thinks they're being clever.
var dangerousPatterns = []string{
	";",   // command separator (cmd) / pipeline (sh)
	"&",   // background / AND chain
	"|",   // pipe
	">",   // output redirect
	"<",   // input redirect
	"&&",  // chained AND
	"||",  // chained OR
	"``",  // nested command substitution
	"$(",  // command substitution (sh)
	"`",   // command substitution (legacy sh)
	"&&&", // extended chainer
}

// allowedShellCommands is the explicit allowlist for :cmd. Only commands whose
// first token (after trimming) appears here are permitted. Everything else is
// rejected with a clear status message. This is the primary gate; the
// dangerousPatterns check is a secondary defense-in-depth layer.
var allowedShellCommands = map[string]bool{
	"dir":    true,
	"ls":     true,
	"cd":     true,
	"pwd":    true,
	"echo":   true,
	"date":   true,
	"time":   true,
	"tree":   true,
	"find":   true,
	"where":  true,
	"which":  true,
	"ping":   true,
	"ipconfig": true,
	"ifconfig": true,
	"netstat": true,
	"tasklist": true,
	"ps":     true,
	"vol":    true,
	" ver":   true, // space-prefix avoids matching "version" etc.
}

// sanitizeShellInput performs two layers of validation on a user-supplied :cmd
// string: (1) an explicit allowlist of permitted command names, and (2) a
// blacklist of dangerous substrings. It returns an error when the input is
// rejected so the caller can surface a clear message in the status line.
func sanitizeShellInput(cmd string) error {
	// Layer 1: reject known-dangerous characters regardless of quoting.
	for _, pat := range dangerousPatterns {
		if strings.Contains(cmd, pat) {
			return fmt.Errorf("命令被拒绝: 包含危险字符 %q", pat)
		}
	}
	// Layer 2: allowlist check on the first token.
	first := strings.Fields(cmd)
	if len(first) == 0 {
		return nil // empty after trim — caller already guards against this.
	}
	base := strings.ToLower(filepath.Base(first[0]))
	if !allowedShellCommands[base] {
		return fmt.Errorf("命令不被允许: %q（仅允许 %v）", first[0], shellAllowedNames())
	}
	return nil
}

// shellAllowedNames returns a human-readable list of permitted command names
// for error messages.
func shellAllowedNames() []string {
	names := make([]string, 0, len(allowedShellCommands))
	for n := range allowedShellCommands {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// runShell executes a command via the platform shell (cmd on Windows, sh
// elsewhere) and returns combined output. Caller MUST have already validated
// the command via sanitizeShellInput.
func runShell(command string) ([]byte, error) {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", command).CombinedOutput()
	}
	return exec.Command("sh", "-c", command).CombinedOutput()
}

// handleQueueKey routes keys while overlayQueue is visible.
// Esc toggles the overlay back to the normal view. Space pauses/resumes all
// in-flight jobs; Ctrl+C cancels the active job; Ctrl+A cancels all.
func (m *model) handleTreeViewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEscape {
		m.ov = overlayNone
		m.treePath = ""
		m.treeRoot = nil
		m.treeHistory = nil
		m.treeLoading = false
		m.status = ""
		return m, nil
	}
	if m.treeLoading {
		return m, nil
	}
	if m.treeRoot == nil {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyUp, tea.KeyCtrlP:
		if m.treeCursor > 0 {
			m.treeCursor--
			// Keep cursor visible: if scrolling up past the top, scroll with it.
			if m.treeCursor < m.treeScroll {
				m.treeScroll = m.treeCursor
			}
		}
	case tea.KeyDown, tea.KeyCtrlN:
		// treeCursor indexes into treeFlat (pre-order visible nodes). Clamp to
		// the last entry so a long press at the bottom is a no-op, not OOB.
		if m.treeCursor < len(m.treeFlat)-1 {
			m.treeCursor++
			// Keep cursor visible: if scrolling down past the bottom, scroll with it.
			visibleLines := m.height - 4
			if visibleLines < 2 {
				visibleLines = 2
			}
			if m.treeCursor >= m.treeScroll+visibleLines {
				m.treeScroll = m.treeCursor - visibleLines + 1
			}
		}
	case tea.KeyPgUp:
		// Page up: move cursor up by one screenful.
		visibleLines := m.height - 4
		if visibleLines < 2 {
			visibleLines = 2
		}
		m.treeCursor = maxInt(0, m.treeCursor-visibleLines)
		if m.treeCursor < m.treeScroll {
			m.treeScroll = m.treeCursor
		}
	case tea.KeyPgDown:
		// Page down: move cursor down by one screenful.
		visibleLines := m.height - 4
		if visibleLines < 2 {
			visibleLines = 2
		}
		m.treeCursor = minInt(len(m.treeFlat)-1, m.treeCursor+visibleLines)
		if m.treeCursor >= m.treeScroll+visibleLines {
			m.treeScroll = m.treeCursor - visibleLines + 1
		}
	case tea.KeyHome:
		m.treeCursor = 0
		m.treeScroll = 0
	case tea.KeyEnd:
		m.treeCursor = len(m.treeFlat) - 1
		visibleLines := m.height - 4
		if visibleLines < 2 {
			visibleLines = 2
		}
		maxScroll := maxInt(0, len(m.treeFlat)-visibleLines)
		if maxScroll < 0 {
			maxScroll = 0
		}
		m.treeScroll = maxScroll
	case tea.KeyEnter:
		// Enter descends into the directory under the cursor. The current path
		// is pushed onto the history stack so ←/Backspace can return to it.
		if m.treeCursor < 0 || m.treeCursor >= len(m.treeFlat) {
			return m, nil
		}
		node := m.treeFlat[m.treeCursor]
		m.treeHistory = append(m.treeHistory, m.treePath)
		return m, m.restatTree(node.path)
	case tea.KeyBackspace, tea.KeyLeft:
		if len(m.treeHistory) > 0 {
			parent := m.treeHistory[len(m.treeHistory)-1]
			m.treeHistory = m.treeHistory[:len(m.treeHistory)-1]
			m.treeCursor = 0
			return m, m.restatTree(parent)
		}
	}
	return m, nil
}

// handleQueueKey routes keys while overlayQueue is visible.
// Esc toggles the overlay back to the normal view. Space pauses/resumes all
// in-flight jobs; Ctrl+C cancels the active job; Ctrl+A cancels all.
func (m *model) handleQueueKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.ov = overlayNone
		// Keep the queue running in background; just hide the overlay.
		return m, nil
	case " ":
		m.paused = !m.paused
		if m.paused {
			m.status = "队列已暂停（新任务仍可入队，进行中任务等待恢复）"
		} else {
			m.status = "队列已恢复"
		}
		return m, nil
	}
	switch msg.Type {
	case tea.KeyCtrlC:
		// Cancel the first active job.
		acts := m.queue.ActiveJobs()
		if len(acts) > 0 {
			m.queue.Cancel(acts[0].id)
			m.status = "已取消任务 #" + utoa(acts[0].id)
		}
		return m, nil
	case tea.KeyCtrlA:
		m.queue.CancelAll()
		m.status = "已取消全部任务"
		return m, nil
	}
	return m, nil
}

// handleAssocKey routes keys for the association editor (Ctrl+E).
//   - Tab / Shift+Tab : switch between view / edit / open action tabs
//   - ↑/↓             : move the cursor within the current action's list
//   - a               : add a new binding (prompts for extension, then command)
//   - d / Delete      : delete the binding under the cursor
//   - Enter           : (no-op on list; add is driven by 'a')
//   - Esc             : close the editor
//
// Adding flows through the shared input overlay: first the extension, then on
// commit the command, so we never block the event loop with a custom prompt.
func (m *model) handleAssocKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyEscape {
		m.closeOverlay()
		return m, nil
	}
	// While entering the extension or command we defer to the input overlay,
	// which is active on top of this editor. Guard against re-entrancy.
	if m.ov == overlayInput || m.ov == overlayConfirm {
		return m, nil
	}
	switch msg.Type {
	case tea.KeyTab:
		// Tab cycles forward; Shift+Tab (Alt+Tab is not reliably reported) also
		// cycles forward here — acceptable for a 3-tab editor.
		m.assocActionIdx = (m.assocActionIdx + 1) % len(assocActions())
		m.assocCursor = 0
		m.assocMsg = ""
		return m, nil
	}
	switch msg.String() {
	case "up", "k":
		if m.assocCursor > 0 {
			m.assocCursor--
		}
		return m, nil
	case "down", "j":
		n := len(m.currentAssocList())
		if m.assocCursor < n-1 {
			m.assocCursor++
		}
		return m, nil
	case "a":
		// Begin add: ask for the extension first, then the command. The action
		// tab is captured now so the two-step input survives closeOverlay's
		// state reset between prompts.
		action := assocActions()[m.assocActionIdx]
		m.openInput("扩展名 (如 txt 或 .txt):", "", func(ext string) {
			ext = strings.TrimSpace(ext)
			if ext == "" {
				return
			}
			m.openInput("关联程序 (如 notepad 或 C:\\app\\a.exe):", "", func(cmd string) {
				m.SetAssoc(action, ext, cmd)
				// Restore the editor overlay (closeOverlay reset ov to None).
				m.ov = overlayAssoc
				m.assocMsg = fmt.Sprintf("已绑定 %s -> %s", assocKey(ext), cmd)
				m.assocCursor = 0
			})
		})
		return m, nil
	case "d", "delete":
		list := m.currentAssocList()
		if m.assocCursor < 0 || m.assocCursor >= len(list) {
			return m, nil
		}
		ext := list[m.assocCursor]
		action := assocActions()[m.assocActionIdx]
		m.DelAssoc(action, ext)
		m.assocMsg = fmt.Sprintf("已删除 %s 的绑定", ext)
		if m.assocCursor >= len(m.currentAssocList()) {
			m.assocCursor = len(m.currentAssocList()) - 1
		}
		if m.assocCursor < 0 {
			m.assocCursor = 0
		}
		return m, nil
	}
	return m, nil
}

// currentAssocList returns the sorted extension keys bound to the active action
// tab, for stable cursor navigation in the editor.
func (m *model) currentAssocList() []string {
	action := string(assocActions()[m.assocActionIdx])
	m2 := m.assoc[action]
	out := make([]string, 0, len(m2))
	for k := range m2 {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// utoa converts an int64 to string — avoids importing strconv just for one call.
func utoa(n int64) string {
	if n == 0 {
		return "0"
	}
	var buf [24]byte
	pos := len(buf)
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[pos:])
}
