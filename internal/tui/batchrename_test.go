package tui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"tcmd/internal/fs"
)

// TestComposeName covers the rename grammar: search/replace, prefix/suffix,
// the {n} placeholder, and the default pre-extension counter injection.
func TestComposeName(t *testing.T) {
	m := &model{brStart: 1, brWidth: 3}
	cases := []struct {
		name    string
		prefix  string
		suffix  string
		search  string
		replace string
		counter bool
		orig    string
		idx     int
		want    string
	}{
		{"prefix_suffix", "img_", "_final", "", "", false, "photo.jpg", 0, "img_photo.jpg_final"},
		{"search_replace", "", "", "a", "b", false, "cat.png", 0, "cbt.png"},
		{"counter_default", "", "", "", "", true, "file.txt", 0, "file001.txt"},
		{"counter_placeholder", "n{n}_", "", "", "", true, "x", 2, "n003_x"},
		{"combined", "P_", "_S", "old", "new", true, "old.doc", 1, "P_new002.doc_S"},
	}
	for _, c := range cases {
		m.brPrefix, m.brSuffix, m.brSearch, m.brReplace = c.prefix, c.suffix, c.search, c.replace
		m.brCounter = c.counter
		got := m.composeName(c.orig, c.idx)
		if got != c.want {
			t.Errorf("%s: composeName(%q,%d)=%q want %q", c.name, c.orig, c.idx, got, c.want)
		}
	}
}

// TestPlanRenamesDuplicateTarget refuses when two sources map to one name.
func TestPlanRenamesDuplicateTarget(t *testing.T) {
	dir := t.TempDir()
	srcs := []fs.Entry{
		{Name: "a.txt", Path: filepath.Join(dir, "a.txt")},
		{Name: "b.txt", Path: filepath.Join(dir, "b.txt")},
	}
	targets := []string{"same.txt", "same.txt"}
	_, bad, err := planRenames(dir, srcs, targets)
	if err == nil {
		t.Fatal("expected error for duplicate target")
	}
	if len(bad) != 2 {
		t.Fatalf("expected both indices bad, got %v", bad)
	}
}

// TestPlanRenamesOverwriteExternal refuses when a target collides with an
// existing file that is not part of the rename set.
func TestPlanRenamesOverwriteExternal(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "existing.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	srcs := []fs.Entry{{Name: "src.txt", Path: filepath.Join(dir, "src.txt")}}
	targets := []string{"existing.txt"}
	_, bad, err := planRenames(dir, srcs, targets)
	if err == nil {
		t.Fatal("expected error for external overwrite")
	}
	if len(bad) != 1 {
		t.Fatalf("expected index 0 bad, got %v", bad)
	}
}

// TestPlanRenamesChain allows a safe rename chain (a->b, b->c, c->d) and
// returns ops in an order that never clobbers an un-moved source.
func TestPlanRenamesChain(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte(n), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srcs := []fs.Entry{
		{Name: "a", Path: filepath.Join(dir, "a")},
		{Name: "b", Path: filepath.Join(dir, "b")},
		{Name: "c", Path: filepath.Join(dir, "c")},
	}
	targets := []string{"b", "c", "d"}
	ops, bad, err := planRenames(dir, srcs, targets)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bad) != 0 {
		t.Fatalf("expected no bad indices, got %v", bad)
	}
	// Apply in the planned order and confirm no source is clobbered mid-way.
	for _, op := range ops {
		if _, e := os.Stat(op.old); e != nil {
			t.Fatalf("source missing before rename: %s", op.old)
		}
		if err := os.Rename(op.old, op.new); err != nil {
			t.Fatalf("rename failed: %v", err)
		}
	}
	for _, want := range []string{"b", "c", "d"} {
		if _, e := os.Stat(filepath.Join(dir, want)); e != nil {
			t.Fatalf("expected %s to exist after rename", want)
		}
	}
}

// TestBeginAndCancelBatchRename checks the overlay opens on the selection and
// Escape closes it without changing the filesystem.
func TestBeginAndCancelBatchRename(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"f1.txt", "f2.txt"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := &model{active: 0}
	m.panes[0] = &pane{tabs: []*tab{newTab(dir)}, active: 0}
	m.panes[1] = &pane{tabs: []*tab{newTab(dir)}, active: 0}
	// Wait for async loads to complete.
	for _, p := range m.panes {
		for _, tb := range p.tabs {
			tb.waitForLoading(2 * time.Second)
		}
	}
	m.panes[0].current().selected[filepath.Join(dir, "f1.txt")] = true

	m.beginBatchRename()
	if m.ov != overlayBatchRename {
		t.Fatalf("expected overlayBatchRename, got %v", m.ov)
	}
	if len(m.brFiles) != 1 {
		t.Fatalf("expected 1 file from selection, got %d", len(m.brFiles))
	}

	// Escape closes the overlay.
	m.handleBatchRenameKey(teaKey(tea.KeyEscape))
	if m.ov != overlayNone {
		t.Fatalf("expected overlay closed, got %v", m.ov)
	}
	// Filesystem unchanged.
	if _, e := os.Stat(filepath.Join(dir, "f1.txt")); e != nil {
		t.Fatal("file should still exist after cancel")
	}
}

// helpers to synthesise bubbletea key messages without importing the package
// internals beyond the type alias used above.
func teaKey(t tea.KeyType) tea.KeyMsg {
	return tea.KeyMsg{Type: t}
}
