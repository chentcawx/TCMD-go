package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestF3TreeIndentation(t *testing.T) {
	dir := t.TempDir()
	// Create: dir/a/b/c (deep nesting), dir/x (sibling)
	if err := os.MkdirAll(filepath.Join(dir, "a", "b", "c"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "file.txt"), []byte("hi"), 0o644)

	root := buildTree(dir)
	lines := fmtTree(root, 100)
	t.Logf("=== fmtTree output (%d lines) ===", len(lines))
	for i, line := range lines {
		t.Logf("  [%d] %q", i, line)
	}

	// Verify structure via flattenTree
	flat := flattenTree(root)
	t.Logf("=== flattenTree (%d nodes) ===", len(flat))
	for i, n := range flat {
		t.Logf("  [%d] depth=%d name=%q path=%q", i, n.depth, n.name, n.path)
	}

	expected := map[string]int{"a": 1, "b": 2, "c": 3, "x": 1}
	for _, n := range flat {
		if want, ok := expected[n.name]; ok {
			if n.depth != want {
				t.Errorf("node %q: depth=%d want %d", n.name, n.depth, want)
			}
		}
	}

	// Verify indentation in rendered lines:
	// root-level children (a, x): a is first (├─), x is last (└─)
	// b is the only child of a → └─ with prefix │
	// c is the only child of b → └─ with prefix │   + four spaces
	indentRe := map[string]string{
		"a": "├─ 📁 a",           // root-level first child
		"b": "│   └─ 📁 b",       // only child of a
		"c": "│       └─ 📁 c",   // only child of b (│   + four spaces = 7 spaces)
		"x": "└─ 📁 x",           // root-level last child
	}
	for _, line := range lines {
		for name, wantSubstr := range indentRe {
			if strings.Contains(line, "📁 "+name) {
				if !strings.Contains(line, wantSubstr) {
					t.Errorf("line for %q does not contain %q, got: %q", name, wantSubstr, line)
				}
			}
		}
	}
}
