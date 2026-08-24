package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// TestNewTabAtFocusesChild checks that newTabAt positions the cursor on the
// named entry (the directory we just left) instead of resetting to row 0.
func TestNewTabAtFocusesChild(t *testing.T) {
	dir := t.TempDir()
	// Two sibling directories so the listing is not trivially a single row.
	if err := os.Mkdir(filepath.Join(dir, "aaaa"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "target_dir"), 0o755); err != nil {
		t.Fatal(err)
	}
	tab := newTabAt(dir, "target_dir")
	// Wait for async load to complete.
	if !tab.waitForLoading(2 * time.Second) {
		t.Fatal("timeout waiting for tab to load")
	}
	if tab.cursor < 0 || tab.cursor >= len(tab.entries) {
		t.Fatalf("cursor out of range: %d (entries=%d)", tab.cursor, len(tab.entries))
	}
	if tab.entries[tab.cursor].Name != "target_dir" {
		t.Fatalf("cursor not on child: got %q want target_dir", tab.entries[tab.cursor].Name)
	}
}

// TestNewTabAtMissingChildFallsBackToTop ensures a non-matching focus name
// (e.g. the child was deleted) degrades to the first row rather than panicking.
func TestNewTabAtMissingChildFallsBackToTop(t *testing.T) {
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "real"), 0o755)
	tab := newTabAt(dir, "does_not_exist")
	if tab.cursor != 0 {
		t.Fatalf("expected cursor to fall back to 0, got %d", tab.cursor)
	}
}

// TestUpDirCursorOnExitedDir exercises the full upDir navigation: after going
// up from a sub-directory, the current path is the parent and the cursor rests
// on the directory we just left.
func TestUpDirCursorOnExitedDir(t *testing.T) {
	parent := t.TempDir()
	leaf := filepath.Join(parent, "leaf_dir")
	if err := os.Mkdir(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &model{active: 0}
	m.panes[0] = &pane{tabs: []*tab{newTab(leaf)}, active: 0}
	m.panes[1] = &pane{tabs: []*tab{newTab(parent)}, active: 0}
	// Wait for async loads to complete.
	for _, p := range m.panes {
		for _, tb := range p.tabs {
			if !tb.waitForLoading(2 * time.Second) {
				t.Fatal("timeout waiting for tab to load")
			}
		}
	}

	m.upDir()

	cur := m.panes[0].current()
	if cur.path != parent {
		t.Fatalf("expected current path %q, got %q", parent, cur.path)
	}
	// Wait for async reload after upDir.
	if !cur.waitForLoading(2 * time.Second) {
		t.Fatal("timeout waiting for upDir reload")
	}
	if cur.cursor < 0 || cur.cursor >= len(cur.entries) {
		t.Fatalf("cursor out of range: %d (entries=%d)", cur.cursor, len(cur.entries))
	}
	if cur.entries[cur.cursor].Name != "leaf_dir" {
		t.Fatalf("cursor not on exited dir: got %q want leaf_dir", cur.entries[cur.cursor].Name)
	}
}

// TestAsyncReloadCompletes verifies that asyncReload eventually populates
// t.entries and clears the loading flag.
func TestAsyncReloadCompletes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	tabs := newTab(dir)
	if !tabs.waitForLoading(2 * time.Second) {
		t.Fatal("timeout waiting for async reload")
	}
	if tabs.loading {
		t.Error("tab should not be loading after timeout")
	}
	if len(tabs.entries) == 0 {
		t.Error("expected entries after load, got 0")
	}
}

// TestAsyncReloadPreservesCursor verifies that cursor position is preserved
// across an async reload when the focused entry still exists.
func TestAsyncReloadPreservesCursor(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tabs := newTab(dir)
	tabs.waitForLoading(2 * time.Second)
	// Set cursor to middle entry.
	tabs.cursor = 1
	// Trigger a second async reload (simulating Ctrl+R).
	tabs.asyncReload()
	if !tabs.waitForLoading(2 * time.Second) {
		t.Fatal("timeout waiting for second reload")
	}
	if tabs.cursor != 1 {
		t.Fatalf("cursor should be preserved: got %d want 1", tabs.cursor)
	}
}

// TestAsyncReloadClampCursor verifies cursor is clamped when entries shrink.
func TestAsyncReloadClampCursor(t *testing.T) {
	dir := t.TempDir()
	// Create 3 files.
	for _, n := range []string{"a.txt", "b.txt", "c.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tabs := newTab(dir)
	tabs.waitForLoading(2 * time.Second)
	// Set cursor to last entry.
	tabs.cursor = 2
	// Delete two files so only one remains.
	os.Remove(filepath.Join(dir, "b.txt"))
	os.Remove(filepath.Join(dir, "c.txt"))
	// Trigger reload.
	tabs.asyncReload()
	if !tabs.waitForLoading(2 * time.Second) {
		t.Fatal("timeout waiting for reload after deletion")
	}
	// Cursor should be clamped to 0 (only one entry remains).
	if tabs.cursor != 0 {
		t.Fatalf("cursor should be clamped to 0, got %d", tabs.cursor)
	}
}

// TestUpdateDrainsReloadOnKeyMsg covers the bug class that motivated the
// drain-on-every-message design: an async reload lands on t._reloadCh RIGHT
// AFTER a load goroutine completes and BEFORE Update runs again. Before
// the fix, only the very first reloadTickMsg would ever drain; now any
// message (here a WindowSizeMsg stand-in via handleKey's caller) MUST
// drain, otherwise key-driven reloads would still hang.
func TestUpdateDrainsReloadOnKeyMsg(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &model{active: 0}
	m.panes[0] = &pane{tabs: []*tab{newTab(dir)}, active: 0}
	// Give the goroutine a head start so the result is on the channel by
	// the time we send a WindowSizeMsg — simulates the user re-sizing the
	// window right after launching tcmd.
	time.Sleep(150 * time.Millisecond)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if m.panes[0].current().loading {
		t.Fatal("loading flag should be false after a WindowSizeMsg-driven drain")
	}
	if len(m.panes[0].current().entries) == 0 {
		t.Fatal("entries should be populated after the drain")
	}
}

// TestReloadTickSelfPerpetuates verifies that processing reloadTickMsg
// schedules the next tick cmd. If the reschedule is missing the chain
// dies after one tick — and the async reload that lands >50ms after
// startup would never be drained.
func TestReloadTickSelfPerpetuates(t *testing.T) {
	m := &model{active: 0}
	_, cmd := m.Update(reloadTickMsg{})
	if cmd == nil {
		t.Fatal("reloadTickMsg must reschedule the next tick cmd")
	}
	// The rescheduled cmd is itself a Tick — it would block on a timer
	// for ~50ms before producing another reloadTickMsg. We can't usefully
	// inspect what it produces synchronously here, but verifying the
	// non-nil return is enough to lock in the self-perpetuation contract.
}

// TestReloadTickDrainsPendingReload verifies that processing reloadTickMsg
// also drains any t._reloadCh that has a result pending — the original
// purpose of the tick.
func TestReloadTickDrainsPendingReload(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &model{active: 0}
	m.panes[0] = &pane{tabs: []*tab{newTab(dir)}, active: 0}
	// Give the goroutine a generous head start.
	time.Sleep(100 * time.Millisecond)
	m.Update(reloadTickMsg{})
	if m.panes[0].current().loading {
		t.Fatal("reloadTickMsg must drain a pending reload — tab is still loading")
	}
	if len(m.panes[0].current().entries) == 0 {
		t.Fatal("entries must be populated after reloadTickMsg")
	}
}

// TestDrainHandlesNilPane verifies drainReloadResults tolerates partial
// models (some panes nil) that the tests build. In production the model is
// always fully built, but we don't want every test to have to wire both
// panes.
func TestDrainHandlesNilPane(t *testing.T) {
	m := &model{}
	// panes[0] and panes[1] are nil — drain should not panic.
	m.drainReloadResults()
}
