//go:build !windows

package fs_test

import (
	"path/filepath"
	"testing"

	"tcmd/internal/fs"
)

func TestMoveWithLinkStub(t *testing.T) {
	// On non-Windows, MoveWithLink moves but does not create symlinks; the
	// return is the list of successfully-moved original paths.
	root := t.TempDir()
	src := filepath.Join(root, "moved")
	dst := filepath.Join(root, "dest")
	if err := fs.Mkdir(src); err != nil {
		t.Fatal(err)
	}
	if err := fs.Mkdir(dst); err != nil {
		t.Fatal(err)
	}
	links := fs.MoveWithLink([]string{src}, dst)
	if len(links) != 1 || links[0] != src {
		t.Fatalf("got %v", links)
	}
}
