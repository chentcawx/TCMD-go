package tui

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// runWithFile launches an external application (appCmd) on a file path. The
// file path is always appended as the final argument. appCmd may be either a
// bare command resolved via PATH (e.g. "code", "notepad") or an absolute/relative
// executable path that may contain spaces (e.g. `C:\Program Files\Foo\foo.exe`).
//
// The process is detached and reaped in a background goroutine so the TUI is
// never blocked waiting on the external program. On failure the error is
// returned so the caller can surface it in the status line.
//
// appCmd is split on whitespace while honoring double quotes, so a command like
// `C:\Program Files\Foo\foo.exe --no-splash` parses into the expected argv.
// The file path is passed through to the OS as-is (no extra quoting), because
// Go's exec.Command quotes arguments with spaces itself on Windows.
func runWithFile(appCmd, file string) error {
	appCmd = strings.TrimSpace(appCmd)
	if appCmd == "" {
		return fmt.Errorf("关联命令为空")
	}
	file = strings.TrimSpace(file)
	if file == "" {
		return fmt.Errorf("文件路径为空")
	}
	args := splitCommand(appCmd)
	if len(args) == 0 {
		return fmt.Errorf("关联命令无法解析: %q", appCmd)
	}
	name := args[0]
	rest := append(args[1:], file)

	if runtime.GOOS == "windows" {
		// Use cmd /c so bare commands resolved via PATH (e.g. "code") and
		// documents work the same as from a Run dialog. exec.Command alone
		// would require an exact executable name with extension on Windows.
		cmd := exec.Command("cmd", "/c", name)
		cmd.Args = append([]string{"cmd", "/c", name}, rest...)
		return startDetached(cmd)
	}
	cmd := exec.Command(name, rest...)
	return startDetached(cmd)
}

// startDetached begins the process and reaps it in the background, returning
// immediately with any start error. The child is intentionally left running
// after tcmd continues — matching Explorer's "open with" behavior.
func startDetached(cmd *exec.Cmd) error {
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() {
		_ = cmd.Wait()
	}()
	return nil
}

// splitCommand tokenizes a command line into argv, honoring double quotes so
// paths with spaces stay a single token. Single quotes are treated literally
// (Windows shells do not honor them).
func splitCommand(s string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case (r == ' ' || r == '\t') && !inQuote:
			flush()
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// assocActionForExt returns a human-readable action label for status messages.
func assocActionLabel(a AssocAction) string {
	switch a {
	case AssocView:
		return "查看(F3)"
	case AssocEdit:
		return "编辑(F4)"
	case AssocOpen:
		return "打开(Enter)"
	default:
		return string(a)
	}
}

// extOf returns the lower-cased extension (with leading dot) of name, or "" if
// name has no extension. Used for association lookups. "FILE.TXT" -> ".txt".
func extOf(name string) string {
	return strings.ToLower(filepath.Ext(name))
}

// baseName returns the final path element of p (mirrors filepath.Base but kept
// local to avoid importing path/filepath at every call site that only needs a
// display name).
func baseName(p string) string {
	return filepath.Base(p)
}
