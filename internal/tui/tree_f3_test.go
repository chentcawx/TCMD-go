package tui

import (
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

// TestF3TreeEnterNavigation verifies Enter descends into the node under the
// cursor (pushing the current path onto the history stack) and Backspace
// returns to the previous directory — the core navigation the overlay promises
// in its hint line.
func TestF3TreeEnterNavigation(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	// Put a marker file inside sub so the descendant tree has something.
	if err := os.WriteFile(filepath.Join(sub, "inner.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := InitialModel()
	m.active = 0

	// Open tree at the top dir.
	cmd := m.openTreeView(dir)
	msg := cmd()
	updated, _ := m.Update(msg)
	m = updated.(*model)
	if m.treeRoot == nil {
		t.Fatal("treeRoot not populated")
	}
	// treeFlat[0] should be the "sub" child.
	if len(m.treeFlat) != 1 {
		t.Fatalf("expected 1 visible node (sub), got %d", len(m.treeFlat))
	}
	if m.treeFlat[0].path != sub {
		t.Fatalf("expected flattened node to be %q, got %q", sub, m.treeFlat[0].path)
	}

	// Press Enter on "sub" — should re-stat sub and push dir onto history.
	_, cmd2 := m.handleTreeViewKey(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd2 == nil {
		t.Fatal("Enter on a node must return a non-nil Cmd (re-stat of the subtree)")
	}
	// cmd2 is the openTreeView Cmd for sub; execute and feed back.
	msg2 := cmd2()
	updated2, _ := m.Update(msg2)
	m = updated2.(*model)
	if m.treePath != sub {
		t.Fatalf("after Enter, treePath should be %q, got %q", sub, m.treePath)
	}
	if len(m.treeHistory) != 1 || m.treeHistory[0] != dir {
		t.Fatalf("history should contain the parent dir, got %v", m.treeHistory)
	}
	if m.treeRoot.totalFiles() != 1 {
		t.Errorf("sub should report 1 total file, got %d", m.treeRoot.totalFiles())
	}

	// Press Backspace — should return to dir.
	_, cmd3 := m.handleTreeViewKey(tea.KeyMsg{Type: tea.KeyBackspace})
	if cmd3 == nil {
		t.Fatal("Backspace must return a non-nil Cmd (re-stat of parent)")
	}
	msg3 := cmd3()
	updated3, _ := m.Update(msg3)
	m = updated3.(*model)
	if m.treePath != dir {
		t.Fatalf("after Backspace, treePath should be %q, got %q", dir, m.treePath)
	}
	if len(m.treeHistory) != 0 {
		t.Errorf("history should be empty after returning to root, got %v", m.treeHistory)
	}
}
