//go:build windows

package fs_test

import (
	"os"
	"path/filepath"
	"testing"

	"tcmd/internal/fs"
)

func TestMoveWithLinkMovesAndLeavesPlaceholder(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "moved")
	dst := filepath.Join(root, "dest")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		t.Fatal(err)
	}

	links := fs.MoveWithLink([]string{src}, dst)
	if len(links) != 1 {
		t.Fatalf("expected 1 link, got %d", len(links))
	}
	link := links[0]
	if link != src {
		t.Fatalf("link path mismatch: got %q want %q", link, src)
	}
	// The symlink target is absolute (contains a drive letter on Windows).
	// Note: os.Readlink only works for symbolic links, not junctions. On Windows,
	// junctions appear as directories but without ModeDir set via Lstat.
	// We verify the junction works by reading a file through it.
	target := filepath.Join(dst, "moved")
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("target missing: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(target, "a.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("target content mismatch: %q err=%v", data, err)
	}
	// Verify the original path is accessible (junction redirects to target)
	data, err = os.ReadFile(filepath.Join(src, "a.txt"))
	if err != nil || string(data) != "hello" {
		t.Fatalf("read via junction failed: %q err=%v", data, err)
	}
}

func TestMoveWithLinkPartialSuccess(t *testing.T) {
	root := t.TempDir()
	good := filepath.Join(root, "good")
	bad := filepath.Join(root, "bad")
	dst := filepath.Join(root, "dest")
	for _, d := range []string{good, bad, dst} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// bad will be removed before MoveWithLink runs so the move fails,
	// but the good source should still succeed.
	if err := os.Remove(bad); err != nil {
		t.Fatal(err)
	}

	links := fs.MoveWithLink([]string{good, bad}, dst)
	if len(links) != 1 {
		t.Fatalf("expected 1 successful link, got %d", len(links))
	}
}

func TestMoveWithLinkEmptySources(t *testing.T) {
	links := fs.MoveWithLink([]string{}, "anywhere")
	if len(links) != 0 {
		t.Fatalf("expected empty result, got %d", len(links))
	}
}
