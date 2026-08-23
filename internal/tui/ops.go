package tui

import (
	"path/filepath"
	"strings"

	"tcmd/internal/fs"
)

// copyItems copies every src into dstDir, keeping each item's base name. Errors
// from any single item abort the whole operation so the user is not left with a
// silently partial result.
func copyItems(srcs []string, dstDir string) error {
	for _, s := range srcs {
		dst := filepath.Join(dstDir, filepath.Base(s))
		if err := fs.Copy(s, dst); err != nil {
			return err
		}
	}
	return nil
}

// moveItems moves every src into dstDir, keeping each item's base name.
func moveItems(srcs []string, dstDir string) error {
	for _, s := range srcs {
		dst := filepath.Join(dstDir, filepath.Base(s))
		if err := fs.Move(s, dst); err != nil {
			return err
		}
	}
	return nil
}

// deleteItems removes every path recursively.
func deleteItems(srcs []string) error {
	for _, s := range srcs {
		if err := fs.Delete(s); err != nil {
			return err
		}
	}
	return nil
}

// makeDir creates name under parent.
func makeDir(parent, name string) error {
	return fs.Mkdir(filepath.Join(parent, name))
}

// isBinary reports whether data looks binary rather than text, by scanning for
// a NUL byte (near-certain binary) or a high ratio of non-printable bytes.
func isBinary(data []byte) bool {
	limit := len(data)
	if limit > 8000 {
		limit = 8000
	}
	if limit == 0 {
		return false
	}
	nonPrint := 0
	for i := 0; i < limit; i++ {
		b := data[i]
		if b == 0 {
			return true
		}
		if b < 9 || (b > 13 && b < 32) {
			nonPrint++
		}
	}
	return float64(nonPrint)/float64(limit) > 0.3
}

// splitLines normalises CRLF/CR to LF and splits into lines for the viewer.
func splitLines(data []byte) []string {
	s := string(data)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	return strings.Split(s, "\n")
}
