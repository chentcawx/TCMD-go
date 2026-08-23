package tui

import (
	"os"
	"testing"

	"github.com/charmbracelet/bubbletea"
)

// TestConfigRoundTrip verifies SaveConfig writes JSON that LoadConfig can read
// back with identical content.
func TestConfigRoundTrip(t *testing.T) {
	c := &Config{
		Active: 1,
		Width:  120,
		Height: 30,
		Panes: [2]paneState{
			{Tabs: []string{"C:\\Users\\chenwei", "D:\\Work"}, Active: 1},
			{Tabs: []string{"C:\\Users\\chenwei\\Downloads"}, Active: 0},
		},
	}
	if err := SaveConfig(c); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	defer os.Remove(configPathForTest(t))

	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got == nil {
		t.Fatal("LoadConfig returned nil")
	}
	if got.Active != c.Active || got.Width != c.Width || got.Height != c.Height {
		t.Fatalf("scalars mismatch: got %+v want %+v", got, c)
	}
	for i := 0; i < 2; i++ {
		if got.Panes[i].Active != c.Panes[i].Active {
			t.Fatalf("pane %d active mismatch", i)
		}
		if len(got.Panes[i].Tabs) != len(c.Panes[i].Tabs) {
			t.Fatalf("pane %d tab count mismatch", i)
		}
		for j, tab := range c.Panes[i].Tabs {
			if got.Panes[i].Tabs[j] != tab {
				t.Fatalf("pane %d tab %d mismatch: got %q want %q", i, j, got.Panes[i].Tabs[j], tab)
			}
		}
	}
}

// TestApplyConfigFallback verifies that saved tab paths which no longer exist
// fall back to the home directory instead of crashing startup.
func TestApplyConfigFallback(t *testing.T) {
	m := InitialModel()
	valid := t.TempDir()
	c := &Config{
		Active: 0,
		Panes: [2]paneState{
			{Tabs: []string{valid, "C:\\__does_not_exist__\\x"}, Active: 1},
			{Tabs: []string{valid}, Active: 0},
		},
	}
	m.ApplyConfig(c)
	if got := m.panes[0].tabs[0].path; got != valid {
		t.Fatalf("valid tab path not preserved: got %q want %q", got, valid)
	}
	if got := m.panes[0].tabs[1].path; got != homeDir() {
		t.Fatalf("missing path not fallen back to home: got %q want %q", got, homeDir())
	}
	if m.panes[0].current().path != homeDir() {
		t.Fatalf("active tab should be the fallback: got %q", m.panes[0].current().path)
	}
	if m.panes[0].active != 1 {
		t.Fatalf("pane active not applied: %d", m.panes[0].active)
	}
}

// TestCJKInput verifies IME-composed CJK text is inserted at the rune cursor
// without byte-slicing mojibake, and that the cursor advances by rune count.
func TestCJKInput(t *testing.T) {
	m := InitialModel()
	m.openInput("cmd> ", "", nil)
	// Insert ASCII then CJK as IME composition would deliver it.
	m.handleInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("cd ")})
	m.handleInputKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("文档")})
	if m.inputValue != "cd 文档" {
		t.Fatalf("input value wrong: got %q", m.inputValue)
	}
	if m.inputCursor != 5 { // "cd 文档" = 5 runes
		t.Fatalf("rune cursor wrong: got %d want 5", m.inputCursor)
	}
	// Rendering must include the CJK and not panic / mojibake the cursor block.
	rendered := stripANSI(m.renderInput())
	if !containsRune(rendered, '文') {
		t.Fatalf("rendered input missing CJK: %q", rendered)
	}
}

// TestSanitizeConfigString verifies that control characters (especially null
// bytes) are removed from config strings before saving to JSON.
func TestSanitizeConfigString(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"null bytes", "d:\x00\x00\x00tools", "d:tools"},
		{"mixed control chars", "path\n\x01\x02\x03.exe", "path\n.exe"},
		{"empty", "", ""},
		{"clean", "notepad.exe", "notepad.exe"},
		{"path with nulls", "d:\\tools\\EmEditor\\EmEditor.exe", "d:\\tools\\EmEditor\\EmEditor.exe"},
		{"8 nulls then path", "\x00\x00\x00\x00\x00\x00\x00\x00d:\\tools", "d:\\tools"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeConfigString(tt.in)
			if got != tt.want {
				t.Errorf("sanitizeConfigString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// TestConfigAssocRoundTrip verifies that assoc entries with potentially
// corrupted strings survive a round-trip through JSON marshal + unmarshal.
func TestConfigAssocRoundTrip(t *testing.T) {
	// Simulate what happens if the assoc map somehow gets a string with null bytes.
	m := InitialModel()
	m.SetAssoc(AssocEdit, "txt", "d:\\tools\\EmEditor.exe")
	// Manually inject a null byte into the in-memory map (bypassing SetAssoc
	// sanitization) to test exportAssoc sanitization.
	m.assoc["edit"][".txt"] = "d:\\tools\\EmEditor.exe\x00 corrupt"
	c := &Config{
		Active: 1,
		Panes:  [2]paneState{{Tabs: []string{t.TempDir()}, Active: 0}},
		Assoc:  m.exportAssoc(),
	}
	if err := SaveConfig(c); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	defer os.Remove(configPathForTest(t))

	got, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got == nil {
		t.Fatal("LoadConfig returned nil")
	}
	cmd := got.Assoc["edit"][".txt"]
	if cmd != "d:\\tools\\EmEditor.exe corrupt" {
		t.Errorf("assoc command not sanitized properly: got %q, want %q", cmd, "d:\\tools\\EmEditor.exe corrupt")
	}
	if isValidConfigString(cmd) == false {
		t.Errorf("sanitized string should be valid JSON string: %q", cmd)
	}
}

func containsRune(s string, r rune) bool {
	for _, c := range s {
		if c == r {
			return true
		}
	}
	return false
}

// configPathForTest exposes the resolved config path so tests can clean it up.
func configPathForTest(t *testing.T) string {
	t.Helper()
	p, err := configPath()
	if err != nil {
		t.Fatalf("configPath: %v", err)
	}
	return p
}
