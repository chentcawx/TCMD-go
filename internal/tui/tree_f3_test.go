package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// TestF3TreeLoadingFlow reproduces the user-reported "stuck on 加载中" bug at
// the message-routing level: pressing F3 on a directory must return a tea.Cmd
// that, once executed and fed back through Update, flips treeLoading to false
// and populates treeRoot. Previously the Cmd was swallowed by beginView()'s
// caller, so treeStats never reached Update and the overlay hung.
func TestF3TreeLoadingFlow(t *testing.T) {
	dir := t.TempDir()
	// Create nested structure: dir/a (empty subdir), dir/file.txt
	if err := os.MkdirAll(filepath.Join(dir, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := InitialModel()
	m.active = 0
	m.curTab().path = dir
	m.reloadCurrent()

	// Simulate F3 on the directory itself via the public entry point. We call
	// openTreeView directly with dir (the same thing beginView does for a
	// directory entry) to exercise the async stat + message routing.
	cmd := m.openTreeView(dir)
	if cmd == nil {
		t.Fatal("openTreeView must return a non-nil tea.Cmd (the async stat trigger)")
	}
	if !m.treeLoading {
		t.Fatal("treeLoading should be true immediately after opening the tree")
	}

	// Execute the one-shot Cmd to obtain the treeStats message.
	msg := cmd()
	// Feed it back through Update — this is what bubbletea does internally.
	updated, _ := m.Update(msg)
	m2 := updated.(*model)

	if m2.treeLoading {
		t.Fatal("treeLoading must be false after treeStats is processed by Update")
	}
	if m2.treeRoot == nil {
		t.Fatal("treeRoot must be populated after treeStats is processed")
	}
	if m2.treeRoot.dirCount != 1 {
		t.Errorf("expected root to have 1 subdir, got %d", m2.treeRoot.dirCount)
	}
	if m2.treeRoot.fileCount != 1 {
		t.Errorf("expected root to have 1 direct file, got %d", m2.treeRoot.fileCount)
	}
	if m2.treeRoot.totalFiles() != 1 {
		t.Errorf("expected 1 total file in subtree, got %d", m2.treeRoot.totalFiles())
	}
}

// TestF3TreeViewIsScrollOnly verifies that the F3 tree overlay is now a
// scroll-only viewer (no cursor navigation, no Enter-to-enter). Keys ↑↓ PgUp
// PgDown Home End should only move treeScroll; Enter/Backspace are no-ops.
func TestF3TreeViewIsScrollOnly(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "inner.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create many dirs so treeFlat is longer than visibleLines (h=24 => 20).
	for i := 0; i < 25; i++ {
		name := filepath.Join(dir, fmt.Sprintf("d%02d", i))
		if err := os.MkdirAll(name, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	m := InitialModel()
	m.active = 0
	cmd := m.openTreeView(dir)
	msg := cmd()
	updated, _ := m.Update(msg)
	m = updated.(*model)
	if m.treeRoot == nil {
		t.Fatal("treeRoot not populated")
	}
	if len(m.treeFlat) == 0 {
		t.Fatal("treeFlat should have entries")
	}
	initialScroll := m.treeScroll
	t.Logf("treeFlat len=%d height=%d visibleLines=%d initialScroll=%d ov=%d", len(m.treeFlat), m.height, m.height-4, initialScroll, m.ov)

	// First jump to bottom so we have room to scroll up.
	_, _ = m.handleTreeViewKey(tea.KeyMsg{Type: tea.KeyEnd})
	endScroll := m.treeScroll
	if endScroll <= 0 {
		t.Fatalf("End should scroll to bottom (endScroll=%d), treeFlat len=%d", endScroll, len(m.treeFlat))
	}

	// Up arrow should scroll up by 1 from bottom.
	_, _ = m.handleTreeViewKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.treeScroll != endScroll-1 {
		t.Fatalf("up should decrement scroll from %d: got %d", endScroll, m.treeScroll)
	}

	// Down arrow should restore to bottom.
	_, _ = m.handleTreeViewKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.treeScroll != endScroll {
		t.Fatalf("down should restore scroll to %d: got %d", endScroll, m.treeScroll)
	}

	// Home jumps to top.
	_, _ = m.handleTreeViewKey(tea.KeyMsg{Type: tea.KeyHome})
	if m.treeScroll != 0 {
		t.Fatalf("Home should reset scroll to 0, got %d", m.treeScroll)
	}

	// End jumps to bottom again.
	_, _ = m.handleTreeViewKey(tea.KeyMsg{Type: tea.KeyEnd})
	maxScroll := len(m.treeFlat) - (m.height - 4)
	if maxScroll < 0 {
		maxScroll = 0
	}
	if m.treeScroll != maxScroll {
		t.Fatalf("End should set scroll to max (%d), got %d", maxScroll, m.treeScroll)
	}

	// PageUp from bottom should move up one screenful.
	_, _ = m.handleTreeViewKey(tea.KeyMsg{Type: tea.KeyPgUp})
	if m.treeScroll >= maxScroll {
		t.Fatalf("PgUp should decrease scroll from %d, got %d", maxScroll, m.treeScroll)
	}

	// Enter on a node is now a no-op (scroll-only mode).
	before := m.treeScroll
	_, cmdEnter := m.handleTreeViewKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmdEnter != nil {
		t.Fatal("Enter should return nil Cmd in scroll-only mode")
	}
	if m.treeScroll != before {
		t.Fatalf("Enter should not change scroll, before=%d after=%d", before, m.treeScroll)
	}

	// Backspace on root (no history) is a no-op.
	_, cmdBS := m.handleTreeViewKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if cmdBS != nil {
		t.Fatal("Backspace on root should return nil Cmd")
	}
}

