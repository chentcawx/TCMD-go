package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"tcmd/internal/fs"
)

// Batch-rename form field indices. Navigation moves between these.
const (
	brFieldPrefix = iota
	brFieldSuffix
	brFieldSearch
	brFieldReplace
	brFieldCounter
	brFieldStart
	brFieldWidth
	brFieldCount
)

var (
	brFocusStyle     = lipgloss.NewStyle().Reverse(true)
	brConflictStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("196")) // red
	brDimStyle       = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	brLabelStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("63"))
	cursorNormal     = lipgloss.NewStyle() // sits on a reversed line to mark the rune cursor
	brPanelStyle     = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(1, 2)
)

// composeName builds the new name for orig (the idx-th file in the batch).
// Order: 1) search & replace (all occurrences, case-sensitive) on the whole
// name; 2) wrap with prefix/suffix; 3) if counter is on, substitute "{n}"
// where present, otherwise inject the zero-padded counter just before the
// extension.
func (m *model) composeName(orig string, idx int) string {
	name := orig
	if m.brSearch != "" {
		name = strings.ReplaceAll(name, m.brSearch, m.brReplace)
	}
	name = m.brPrefix + name + m.brSuffix
	if m.brCounter {
		cs := fmt.Sprintf("%0*d", m.brWidth, m.brStart+idx)
		if strings.Contains(name, "{n}") {
			name = strings.ReplaceAll(name, "{n}", cs)
		} else {
			ext := filepath.Ext(name)
			stem := strings.TrimSuffix(name, ext)
			name = stem + cs + ext
		}
	}
	return name
}

// renameOp is one ordered rename (old -> new) within the same directory.
type renameOp struct {
	old, new string
}

// planRenames validates the rename and returns the operations in a safe order
// plus the indices whose result is unsafe (for UI highlighting) and a non-nil
// err when the whole operation must be blocked.
//
// Safety rules (never lose data):
//   - duplicate target names within the batch -> block + mark all involved;
//   - a target that already exists on disk and is not one of the sources being
//     moved away (i.e. it would overwrite an existing or kept file) -> block;
//   - rename cycles among sources (should not arise from a single global rule,
//     but guarded) -> block.
//
// When no cycle exists a topological order exists: a source is only renamed
// after its target name has been vacated by an earlier, already-moved source.
func planRenames(dir string, sources []fs.Entry, targets []string) (ops []renameOp, bad []int, err error) {
	badSet := make(map[int]bool)

	// 1. duplicate targets.
	cnt := make(map[string]int)
	for _, t := range targets {
		cnt[t]++
	}
	dupErr := false
	for t, c := range cnt {
		if c > 1 {
			dupErr = true
			for i := range targets {
				if targets[i] == t {
					badSet[i] = true
				}
			}
		}
	}

	// 2. overwrite checks.
	idxByName := make(map[string]int)
	for i, s := range sources {
		idxByName[s.Name] = i
	}
	ovErr := false
	for i, s := range sources {
		t := targets[i]
		if t == s.Name {
			continue // no-op
		}
		if j, ok := idxByName[t]; ok {
			if targets[j] == t {
				// t is kept in place by source j; renaming i->t would clobber
				// j's file.
				badSet[i] = true
				ovErr = true
			}
			// else j is moving away -> safe, resolved by ordering below.
		} else if fileExists(filepath.Join(dir, t)) {
			badSet[i] = true
			ovErr = true
		}
	}
	switch {
	case dupErr && ovErr:
		err = fmt.Errorf("存在重复且覆盖已有文件的目标名")
	case dupErr:
		err = fmt.Errorf("存在重复的目标文件名")
	case ovErr:
		err = fmt.Errorf("目标名将覆盖已有文件")
	}

	// 3. safe order for non-no-op sources.
	remIdx := make(map[string]int)
	for i, s := range sources {
		if targets[i] != s.Name {
			remIdx[s.Name] = i
		}
	}
	tgtOf := make(map[string]string)
	for name, i := range remIdx {
		tgtOf[name] = targets[i]
	}
	for len(remIdx) > 0 {
		progressed := false
		for name, i := range remIdx {
			t := tgtOf[name]
			if _, still := remIdx[t]; !still {
				ops = append(ops, renameOp{
					old: filepath.Join(dir, sources[i].Name),
					new: filepath.Join(dir, t),
				})
				delete(remIdx, name)
				progressed = true
				break
			}
		}
		if !progressed {
			// Unexpected rename cycle: refuse rather than risk data loss.
			for name, i := range remIdx {
				badSet[i] = true
				_ = name
			}
			err = fmt.Errorf("检测到循环重命名，已阻止以防数据丢失")
			break
		}
	}

	for i := range badSet {
		bad = append(bad, i)
	}
	return ops, bad, err
}

func fileExists(p string) bool {
	_, e := os.Stat(p)
	return e == nil
}

// beginBatchRename collects the rename targets (the selection, or the cursor
// row when nothing is selected) and opens the batch-rename overlay.
func (m *model) beginBatchRename() {
	t := m.curTab()
	var files []fs.Entry
	if len(t.selected) > 0 {
		for _, e := range t.entries {
			if t.selected[e.Path] {
				files = append(files, e)
			}
		}
	} else if len(t.entries) > 0 {
		files = append(files, t.entries[t.cursor])
	}
	if len(files) == 0 {
		m.status = "没有可重命名的项"
		return
	}
	m.brFiles = files
	m.brPrefix = ""
	m.brSuffix = ""
	m.brSearch = ""
	m.brReplace = ""
	m.brCounter = false
	m.brStart = 1
	m.brWidth = 3
	m.brField = brFieldPrefix
	m.brCursor = 0
	m.brScroll = 0
	m.brError = ""
	m.ov = overlayBatchRename
}

// handleBatchRenameKey implements the batch-rename form: field navigation,
// per-field editing, and the commit/cancel keys.
func (m *model) handleBatchRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEscape:
		m.closeOverlay()
		return m, nil
	case tea.KeyEnter, tea.KeyTab:
		// move to the next field; Enter also advances (Tab too).
		m.brField = (m.brField + 1) % brFieldCount
		m.brCursor = 0
		return m, nil
	case tea.KeyShiftTab:
		m.brField = (m.brField + brFieldCount - 1) % brFieldCount
		m.brCursor = 0
		return m, nil
	case tea.KeyUp, tea.KeyCtrlP:
		if m.brField > 0 {
			m.brField--
			m.brCursor = 0
		} else {
			if m.brScroll > 0 {
				m.brScroll--
			}
		}
		return m, nil
	case tea.KeyDown, tea.KeyCtrlN:
		if m.brField < brFieldCount-1 {
			m.brField++
			m.brCursor = 0
		} else {
			m.brScroll++
		}
		return m, nil
	case tea.KeyPgUp:
		if m.brScroll > 0 {
			m.brScroll--
		}
		return m, nil
	case tea.KeyPgDown:
		m.brScroll++
		return m, nil
	case tea.KeyRunes:
		if msg.String() == "y" || msg.String() == "Y" {
			m.applyBatchRename()
			return m, nil
		}
	}

	// Field-specific editing for the rest of the keys.
	switch m.brField {
	case brFieldCounter:
		if msg.Type == tea.KeySpace || msg.String() == " " {
			m.brCounter = !m.brCounter
		}
		// A literal space may arrive as KeyRunes{' '} during IME/typing.
		if msg.Type == tea.KeyRunes && string(msg.Runes) == " " {
			m.brCounter = !m.brCounter
		}
		return m, nil
	case brFieldStart, brFieldWidth:
		return m.handleBatchRenameNumber(msg)
	default:
		return m.handleBatchRenameText(msg)
	}
}

// handleBatchRenameText edits the active text field (prefix/suffix/search/
// replace) in a rune-aware way so CJK input works and the cursor is a rune
// index, mirroring the normal input field.
func (m *model) handleBatchRenameText(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyBackspace:
		rs := []rune(m.activeBrText())
		if m.brCursor > 0 && m.brCursor <= len(rs) {
			rs = append(rs[:m.brCursor-1], rs[m.brCursor:]...)
			m.setActiveBrText(string(rs))
			m.brCursor--
		}
	case tea.KeyLeft:
		if m.brCursor > 0 {
			m.brCursor--
		}
	case tea.KeyRight:
		if m.brCursor < len([]rune(m.activeBrText())) {
			m.brCursor++
		}
	case tea.KeyHome:
		m.brCursor = 0
	case tea.KeyEnd:
		m.brCursor = len([]rune(m.activeBrText()))
	case tea.KeyRunes:
		ins := []rune(msg.String())
		if len(ins) == 0 {
			return m, nil
		}
		rs := []rune(m.activeBrText())
		if m.brCursor < 0 {
			m.brCursor = 0
		}
		if m.brCursor > len(rs) {
			m.brCursor = len(rs)
		}
		rs = append(rs[:m.brCursor], append(ins, rs[m.brCursor:]...)...)
		m.setActiveBrText(string(rs))
		m.brCursor += len(ins)
	default:
		rs := []rune(msg.String())
		if len(rs) == 1 && rs[0] >= 32 {
			r := rs[0]
			cur := []rune(m.activeBrText())
			if m.brCursor < 0 {
				m.brCursor = 0
			}
			if m.brCursor > len(cur) {
				m.brCursor = len(cur)
			}
			cur = append(cur[:m.brCursor], append([]rune{r}, cur[m.brCursor:]...)...)
			m.setActiveBrText(string(cur))
			m.brCursor++
		}
	}
	return m, nil
}

// handleBatchRenameNumber edits an integer field (start / width) by appending
// typed digits and stripping them on backspace.
func (m *model) handleBatchRenameNumber(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyBackspace:
		if m.brField == brFieldStart {
			m.brStart /= 10
		} else {
			m.brWidth /= 10
		}
	case tea.KeyRunes, tea.KeyF10: // KeyRunes covers digit keys
		rs := []rune(msg.String())
		if len(rs) == 1 && rs[0] >= '0' && rs[0] <= '9' {
			d := int(rs[0] - '0')
			if m.brField == brFieldStart {
				m.brStart = m.brStart*10 + d
				if m.brStart > 1_000_000 {
					m.brStart = 1_000_000
				}
			} else {
				m.brWidth = m.brWidth*10 + d
				if m.brWidth > 9 {
					m.brWidth = 9
				}
			}
		}
	}
	return m, nil
}

func (m *model) activeBrText() string {
	switch m.brField {
	case brFieldPrefix:
		return m.brPrefix
	case brFieldSuffix:
		return m.brSuffix
	case brFieldSearch:
		return m.brSearch
	case brFieldReplace:
		return m.brReplace
	}
	return ""
}

func (m *model) setActiveBrText(v string) {
	switch m.brField {
	case brFieldPrefix:
		m.brPrefix = v
	case brFieldSuffix:
		m.brSuffix = v
	case brFieldSearch:
		m.brSearch = v
	case brFieldReplace:
		m.brReplace = v
	}
}

// applyBatchRename validates the plan and executes it, refusing (and surfacing
// the error) when any conflict is detected.
func (m *model) applyBatchRename() {
	dir := m.curTab().path
	targets := make([]string, len(m.brFiles))
	for i, e := range m.brFiles {
		targets[i] = m.composeName(e.Name, i)
	}
	ops, _, err := planRenames(dir, m.brFiles, targets)
	if err != nil {
		m.brError = err.Error()
		return
	}
	done := 0
	for _, op := range ops {
		if err := fs.Move(op.old, op.new); err != nil {
			m.brError = "重命名失败: " + err.Error()
			m.reloadCurrent()
			return
		}
		done++
	}
	m.status = fmt.Sprintf("已重命名 %d 项", done)
	m.clearSelection()
	m.closeOverlay()
	m.reloadCurrent()
}
