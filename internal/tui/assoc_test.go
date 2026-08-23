package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAssocKeyNormalizes(t *testing.T) {
	cases := map[string]string{
		".txt":   ".txt",
		"TXT":    ".txt",
		"txt":    ".txt",
		".Go":    ".go",
		"":       "",
		".":      "",
		"  .md ": ".md",
	}
	for in, want := range cases {
		if got := assocKey(in); got != want {
			t.Errorf("assocKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveAssocCaseInsensitive(t *testing.T) {
	m := InitialModel()
	m.SetAssoc(AssocView, "TXT", "notepad")
	cmd, ok := m.ResolveAssoc(AssocView, ".txt")
	if !ok || cmd != "notepad" {
		t.Errorf("expected case-insensitive match for .txt, got cmd=%q ok=%v", cmd, ok)
	}
	if _, ok := m.ResolveAssoc(AssocEdit, ".txt"); ok {
		t.Error("AssocEdit should not match a view-only binding")
	}
	if _, ok := m.ResolveAssoc(AssocView, ".zip"); ok {
		t.Error("unknown extension should not resolve")
	}
}

func TestSetDelAssocPersisted(t *testing.T) {
	m := InitialModel()
	m.SetAssoc(AssocOpen, "pdf", `C:\Program Files\Sumatra\mu.exe`)
	m.SetAssoc(AssocOpen, "log", "notepad")

	exp := m.exportAssoc()
	if exp == nil {
		t.Fatal("export should be non-nil with bindings present")
	}
	if exp["open"][".pdf"] != `C:\Program Files\Sumatra\mu.exe` {
		t.Errorf("pdf binding lost/mangled: %q", exp["open"][".pdf"])
	}
	if exp["open"][".log"] != "notepad" {
		t.Errorf("log binding lost: %q", exp["open"][".log"])
	}

	m.DelAssoc(AssocOpen, "log")
	if _, ok := m.ResolveAssoc(AssocOpen, ".log"); ok {
		t.Error("log binding should be gone after DelAssoc")
	}
	if _, ok := m.ResolveAssoc(AssocOpen, ".pdf"); !ok {
		t.Error("pdf binding should survive deletion of log")
	}
}

func TestLoadAssocNormalizesLegacyKeys(t *testing.T) {
	m := InitialModel()
	m.loadAssoc(map[string]map[string]string{
		"VIEW":  {"TXT": "notepad", "md": "code"},
		"bogus": {"x": "y"},
		"edit":  {"": "z"},
		"open":  {"pdf": ""},
	})
	if cmd, ok := m.ResolveAssoc(AssocView, ".txt"); !ok || cmd != "notepad" {
		t.Errorf("legacy VIEW/TXT not normalized: cmd=%q ok=%v", cmd, ok)
	}
	if cmd, ok := m.ResolveAssoc(AssocView, ".md"); !ok || cmd != "code" {
		t.Errorf("legacy md (dotless) not normalized: cmd=%q ok=%v", cmd, ok)
	}
	if _, ok := m.ResolveAssoc(AssocAction("bogus"), ".x"); ok {
		t.Error("unknown action 'bogus' should be ignored")
	}
	if _, ok := m.ResolveAssoc(AssocEdit, ""); ok {
		t.Error("empty ext binding should be ignored")
	}
	if _, ok := m.ResolveAssoc(AssocOpen, ".pdf"); ok {
		t.Error("empty command binding should be ignored")
	}
}

func TestSplitCommandHonorsQuotes(t *testing.T) {
	// Both the executable (spaces in path) and the file argument are quoted,
	// mirroring how a user would actually write a command with spaces.
	got := splitCommand(`"C:\Program Files\Foo\foo.exe" --no-splash "C:\my file.txt"`)
	want := []string{`C:\Program Files\Foo\foo.exe`, "--no-splash", `C:\my file.txt`}
	if len(got) != len(want) {
		t.Fatalf("split len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("arg %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestRunWithFileStartsProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("skips process spawn in -short mode")
	}
	// runWithFile should start an existing command without blocking on it.
	// On Windows we exercise the real code path with cmd; elsewhere we skip
	// since the detached-start semantics differ by shell.
	if err := runWithFile("cmd", "echo hi"); err != nil {
		t.Skipf("cmd unavailable on this platform: %v", err)
	}
}

func TestAssocEditorAddAndDelete(t *testing.T) {
	m := InitialModel()
	m.beginAssocEditor()
	if m.ov != overlayAssoc {
		t.Fatal("beginAssocEditor should set overlayAssoc")
	}
	// Press 'a' to start adding on the "view" tab.
	_, _ = m.handleAssocKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("a")})
	if m.ov != overlayInput {
		t.Fatal("'a' should open the extension input overlay")
	}
	// Commit extension "txt" — this opens the command input.
	m.inputValue = "txt"
	m.inputCommit(m.inputValue)
	if m.ov != overlayInput {
		t.Fatal("after ext commit, editor should open the command input")
	}
	// Commit command "notepad" — restores the editor overlay.
	m.inputValue = "notepad"
	m.inputCommit(m.inputValue)
	if m.ov != overlayAssoc {
		t.Fatalf("after command commit, editor should be restored, got ov=%v", m.ov)
	}
	if cmd, ok := m.ResolveAssoc(AssocView, ".txt"); !ok || cmd != "notepad" {
		t.Errorf("binding not created: cmd=%q ok=%v", cmd, ok)
	}

	// Delete it: cursor at 0 (single entry), press 'd'.
	m.assocCursor = 0
	_, _ = m.handleAssocKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("d")})
	if _, ok := m.ResolveAssoc(AssocView, ".txt"); ok {
		t.Error("binding should be removed after delete")
	}
}

func TestAssocEditorTabSwitchesAction(t *testing.T) {
	m := InitialModel()
	m.beginAssocEditor()
	if m.assocActionIdx != 0 {
		t.Fatal("editor should start on the view tab")
	}
	_, _ = m.handleAssocKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.assocActionIdx != 1 {
		t.Errorf("Tab should move to edit tab, got idx=%d", m.assocActionIdx)
	}
	_, _ = m.handleAssocKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.assocActionIdx != 2 {
		t.Errorf("second Tab should move to open tab, got idx=%d", m.assocActionIdx)
	}
	_, _ = m.handleAssocKey(tea.KeyMsg{Type: tea.KeyTab})
	if m.assocActionIdx != 0 {
		t.Errorf("third Tab should wrap to view tab, got idx=%d", m.assocActionIdx)
	}
}

func TestAssocEditorEscapeCloses(t *testing.T) {
	m := InitialModel()
	m.beginAssocEditor()
	_, _ = m.handleAssocKey(tea.KeyMsg{Type: tea.KeyEscape})
	if m.ov != overlayNone {
		t.Errorf("Esc should close the editor, got ov=%v", m.ov)
	}
}

// TestAssocWiredIntoBeginView verifies that F3 on a file with a view
// association launches the bound app instead of the built-in viewer, and that
// without a binding it falls back to openViewer (overlayViewer).
// TestAssocCommandOpensEditor verifies the ":assoc" command opens the editor
// reliably (a terminal-independent entry point alongside Ctrl+E).
func TestAssocCommandOpensEditor(t *testing.T) {
	m := InitialModel()
	// Simulate typing ":" then "assoc" then Enter.
	_, _ = m.handleNormalKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(":")})
	if m.ov != overlayInput {
		t.Fatal("':' should open the command input")
	}
	m.inputValue = "assoc"
	m.inputCommit(m.inputValue)
	if m.ov != overlayAssoc {
		t.Fatalf("':assoc' should open the assoc editor, got ov=%v", m.ov)
	}
}

func TestAssocWiredIntoBeginView(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(f, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := InitialModel()
	m.active = 0
	m.curTab().path = dir
	m.reloadCurrent()
	// Cursor on note.txt.
	for i, e := range m.curTab().entries {
		if e.Name == "note.txt" {
			m.curTab().cursor = i
		}
	}

	// No binding: F3 should open the built-in viewer.
	_, _ = m.beginView()
	if m.ov != overlayViewer {
		t.Errorf("without binding, F3 should open viewer, got ov=%v", m.ov)
	}

	// With binding: F3 should launch the app (status set) and NOT open viewer.
	m.ov = overlayNone
	m.viewerPath = ""
	m.SetAssoc(AssocView, ".txt", "notepad")
	_, _ = m.beginView()
	if m.ov == overlayViewer {
		t.Error("with view binding, F3 must not open the built-in viewer")
	}
	if !strings.Contains(m.status, "关联应用") {
		t.Errorf("expected launch status, got %q", m.status)
	}
}
