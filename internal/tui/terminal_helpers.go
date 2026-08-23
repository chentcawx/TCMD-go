package tui

import (
	"io"
	"os"
	"strings"
)

// stringReader returns an io.Reader over s so it can be fed to a child
// process's stdin (used to pipe text into the clipboard utility).
func stringReader(s string) io.Reader {
	return strings.NewReader(s)
}

// shellQuote single-quotes a path for safe interpolation into a shell -c
// string on POSIX systems.
func shellQuote(p string) string {
	return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
}

// execPath returns the user's login shell, falling back to /bin/sh.
func execPath() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	return "/bin/sh"
}
