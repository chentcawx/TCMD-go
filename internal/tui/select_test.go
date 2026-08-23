package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestSpaceSelectsViaKeySpace covers the normal case where a space arrives as
// the dedicated KeySpace event.
func TestSpaceSelectsViaKeySpace(t *testing.T) {
	// Create a temp dir with one file so the tab has entries after async load.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &model{active: 0}
	m.panes[0] = &pane{tabs: []*tab{newTab(dir)}, active: 0}
	// Wait for async load.
	m.panes[0].current().waitForLoading(2 * time.Second)
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	nm := mm.(*model)
	if len(nm.curTab().selected) != 1 {
		t.Fatalf("KeySpace did not select exactly one entry: %v", nm.curTab().selected)
	}
}

// TestSpaceSelectsViaKeyRunes locks in the fix: some terminals / IME
// composition deliver a space as KeyRunes{' '} rather than KeySpace. Without
// this branch the selection silently did nothing.
func TestSpaceSelectsViaKeyRunes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &model{active: 0}
	m.panes[0] = &pane{tabs: []*tab{newTab(dir)}, active: 0}
	m.panes[0].current().waitForLoading(2 * time.Second)
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	nm := mm.(*model)
	if len(nm.curTab().selected) != 1 {
		t.Fatalf("Space-as-KeyRunes did not select: %v", nm.curTab().selected)
	}
}

// TestSpaceTogglesOff ensures a second Space on the same row clears the mark.
func TestSpaceTogglesOff(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "test.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &model{active: 0}
	m.panes[0] = &pane{tabs: []*tab{newTab(dir)}, active: 0}
	m.panes[0].current().waitForLoading(2 * time.Second)
	m.Update(tea.KeyMsg{Type: tea.KeySpace})
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	nm := mm.(*model)
	if len(nm.curTab().selected) != 0 {
		t.Fatalf("second Space should clear selection, got %v", nm.curTab().selected)
	}
}
