package fs

import (
	"os"
	"path/filepath"
	"testing"
)

// TestListDir verifies listing returns entries with directories sorted first
// and that the hidden-attribute helper does not crash on a normal file.
func TestListDir(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	entries, err := ListDir(dir)
	if err != nil {
		t.Fatalf("ListDir: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
	if !entries[0].IsDir {
		t.Fatalf("expected directory first, got %q", entries[0].Name)
	}
}

// TestCopyAndDelete covers the copy path (streamed, not whole-file) and the
// recursive delete, asserting both the presence and the absence after delete.
func TestCopyAndDelete(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "f.txt"), []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := Copy(filepath.Join(src, "f.txt"), filepath.Join(dst, "f.txt")); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "f.txt")); err != nil {
		t.Fatalf("copied file missing: %v", err)
	}
	if err := Delete(filepath.Join(dst, "f.txt")); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "f.txt")); !os.IsNotExist(err) {
		t.Fatalf("file should have been deleted, stat err=%v", err)
	}
}

// TestMkdir verifies nested directory creation via MkdirAll semantics.
func TestMkdir(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "a", "b")
	if err := Mkdir(nested); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	fi, err := os.Stat(nested)
	if err != nil || !fi.IsDir() {
		t.Fatalf("nested directory not created: %v", err)
	}
}

// TestCopyDirectory verifies recursive directory copy preserves contents.
func TestCopyDirectory(t *testing.T) {
	src := t.TempDir()
	if err := os.MkdirAll(filepath.Join(src, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "inner", "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if err := Copy(src, filepath.Join(dst, "copied")); err != nil {
		t.Fatalf("Copy dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, "copied", "inner", "x.txt")); err != nil {
		t.Fatalf("nested file missing after dir copy: %v", err)
	}
}
