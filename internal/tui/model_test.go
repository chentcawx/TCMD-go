package tui

import (
	"os"
	"path/filepath"
	"testing"
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

	m.upDir()

	cur := m.panes[0].current()
	if cur.path != parent {
		t.Fatalf("expected current path %q, got %q", parent, cur.path)
	}
	if cur.cursor < 0 || cur.cursor >= len(cur.entries) {
		t.Fatalf("cursor out of range: %d (entries=%d)", cur.cursor, len(cur.entries))
	}
	if cur.entries[cur.cursor].Name != "leaf_dir" {
		t.Fatalf("cursor not on exited dir: got %q want leaf_dir", cur.entries[cur.cursor].Name)
	}
}
