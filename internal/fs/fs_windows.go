//go:build windows

package fs

import (
	"os"
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
