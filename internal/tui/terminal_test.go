package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestAltF7OpensCmdTerminal verifies the Alt+F7 (the only modifier+F7 the
// terminal protocol can distinguish) trigger runs beginCmdTerminal, which opens
// a standalone terminal and copies the cursor directory to the clipboard,
// without disturbing the mkdir binding on plain F7.
func TestAltF7OpensCmdTerminal(t *testing.T) {
	m := InitialModel()
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyF7, Alt: true})
	nm := mm.(*model)
	if nm.ov != overlayNone {
		t.Fatalf("Alt+F7 should not open an overlay, got %v", nm.ov)
	}
	const want = "已打开命令行于"
	if len(nm.status) < len(want) || nm.status[:len(want)] != want {
		t.Fatalf("expected status to start with %q, got %q", want, nm.status)
	}
}

// TestPlainF7StillMkdir ensures a plain F7 still opens the mkdir input overlay
// and was not clobbered by the new Alt+F7 branch.
func TestPlainF7StillMkdir(t *testing.T) {
	m := InitialModel()
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyF7})
	nm := mm.(*model)
	if nm.ov != overlayInput {
		t.Fatalf("plain F7 should open mkdir input, got %v", nm.ov)
	}
}
