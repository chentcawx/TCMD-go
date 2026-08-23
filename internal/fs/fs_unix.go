//go:build !windows

package fs

import (
	"path/filepath"
)

// MoveWithLink is the Unix stub: move items without link placeholders. POSIX
// symlinks require no privilege but F11's semantics are Windows-junction
// centric here; the Unix path simply moves and returns the moved paths so
// the UI can reload panes.
func MoveWithLink(sources []string, dstDir string) []string {
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		dst := filepath.Join(dstDir, filepath.Base(s))
		if err := Move(s, dst); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out
}
