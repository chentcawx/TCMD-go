//go:build !windows

package fs

import (
	"os"
	"strings"
)

// isHidden reports whether an entry is hidden. On Unix-like systems that means
// a leading dot; Windows uses the FILE_ATTRIBUTE_HIDDEN flag (see
// fs_windows.go).
func isHidden(path string, fi os.FileInfo) bool {
	return strings.HasPrefix(fi.Name(), ".")
}
