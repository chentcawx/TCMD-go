//go:build windows

package tui

import (
	"os/exec"
	"runtime"
	"syscall"
	"unsafe"
)

// shellOpen launches path with the operating system's default association for
// the "open" verb — the exact same action Explorer performs when you
// double-click a file or right-click and choose "打开". This is the genuine
// system verb (via ShellExecuteW), not a `cmd /c start` approximation, so it
// honors per-extension handlers, UAC elevation prompts, and per-user defaults
// correctly. The call is synchronous but ShellExecute returns immediately after
// spawning the target; we do not wait on the launched program.
func shellOpen(path string) error {
	verb := syscall.StringToUTF16Ptr("open")
	file := syscall.StringToUTF16Ptr(path)
	// ShellExecuteW(hwnd, lpVerb, lpFile, lpParameters, lpDirectory, nShowCmd)
	// hwnd=0: no owner window (acceptable for the open verb). SW_SHOWNORMAL=1.
	r, _, err := shellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(verb)),
		uintptr(unsafe.Pointer(file)),
		0,
		0,
		1,
	)
	// A return value > 32 indicates success; <= 32 is an error code.
	if r <= 32 {
		if err == nil {
			err = syscall.EINVAL
		}
		return err
	}
	return nil
}

// shell32.dll — ShellExecuteW is the system entry point for the open/explore/
// print/… verbs and is what Explorer's context menu ultimately calls.
var shellExecuteW = syscall.NewLazyDLL("shell32.dll").NewProc("ShellExecuteW")

// openCmdTerminal launches a standalone Command Prompt already positioned in
// dir. `cmd /k cd /d dir` keeps the window open after the cd so the user can
// keep working. The process is detached so the TUI is never blocked.
func openCmdTerminal(dir string) error {
	cmd := exec.Command("cmd", "/c", "start", "cmd", "/k", "cd", "/d", dir)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// writeClipboard copies text to the Windows clipboard using a hidden child
// process (powershell -NoProfile -Command with Clip). This avoids pulling in a
// CGO clipboard dependency and stays pure-Go. Failures are non-fatal: the
// terminal still opens, only the clipboard step is skipped.
func writeClipboard(text string) error {
	ps := `$input | clip`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
	cmd.Stdin = stringReader(text)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// ensure runtime is referenced (kept for symmetry with the unix variant that
// branches on it); harmless on windows.
var _ = runtime.GOOS
