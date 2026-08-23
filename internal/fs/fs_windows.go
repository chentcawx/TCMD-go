//go:build windows

package fs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// isHidden reports whether an entry is hidden. On Windows a file is hidden if
// its name starts with a dot (e.g. ".git") or it carries the
// FILE_ATTRIBUTE_HIDDEN flag — matching Explorer's behaviour.
func isHidden(path string, fi os.FileInfo) bool {
	if strings.HasPrefix(fi.Name(), ".") {
		return true
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		return false
	}
	return attrs&windows.FILE_ATTRIBUTE_HIDDEN != 0
}

// CreateJunction creates a directory junction at link -> target. Both paths
// must be absolute. The link must not already exist.
//
// We use cmd /c mklink /J because direct DeviceIoControl with FSCTL_SET_REPARSE_POINT
// has proven unreliable on this Windows build (the call succeeds but the junction
// is non-functional). mklink creates a working junction in all cases.
//
// F11 calls this after Move(src, dst) to preserve the original path as a
// transparent placeholder.
func CreateJunction(link, target string) error {
	if !filepath.IsAbs(link) || !filepath.IsAbs(target) {
		return os.ErrInvalid
	}
	// The link must not already exist (as anything).
	if _, err := os.Lstat(link); err == nil {
		return os.ErrExist
	} else if !os.IsNotExist(err) {
		return err
	}
	// Use mklink /J which reliably creates junctions on all Windows builds.
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J %q -> %q: %v (output: %s)", link, target, err, string(out))
	}
	return nil
}

// MoveWithLink moves every src into dstDir (keeping each item's base name)
// and, on success, creates a junction at the original src pointing to the
// new location. The link acts as a transparent placeholder so callers that
// reference the original path keep working.
//
// Errors are collected per-item: one failing source never aborts the others.
// The returned slice contains the paths of successfully-created links.
func MoveWithLink(sources []string, dstDir string) []string {
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		dst := filepath.Join(dstDir, filepath.Base(s))
		if err := Move(s, dst); err != nil {
			continue
		}
		if err := CreateJunction(s, dst); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out
}
