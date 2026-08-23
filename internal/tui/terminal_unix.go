//go:build !windows

package tui

import (
	"os/exec"
	"runtime"
)

// openCmdTerminal launches a standalone terminal emulator already positioned in
// dir. On Linux/macOS we prefer the platform shell; if no GUI terminal is found
// we fall back to the user's login shell in the current PTY (best effort).
func openCmdTerminal(dir string) error {
	var bin string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		bin = "open"
		args = []string{"-a", "Terminal", dir}
	default: // linux and others
		if _, err := exec.LookPath("x-terminal-emulator"); err == nil {
			bin = "x-terminal-emulator"
			args = []string{"--working-directory", dir}
		} else if _, err := exec.LookPath("gnome-terminal"); err == nil {
			bin = "gnome-terminal"
			args = []string{"--working-directory", dir}
		} else {
			// No GUI terminal: run the login shell in the current PTY.
			shell := "sh"
			if s := execPath(); s != "" {
				shell = s
			}
			cmd := exec.Command(shell, "-c", "cd "+shellQuote(dir)+" && exec "+shell)
			if err := cmd.Start(); err != nil {
				return err
			}
			go func() { _ = cmd.Wait() }()
			return nil
		}
	}
	cmd := exec.Command(bin, args...)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// shellOpen launches path with the platform's default association, mirroring
// the Windows ShellExecute "open" verb (the system action behind a
// double-click / right-click "打开" in a file manager). On macOS `open` and on
// Linux `xdg-open` resolve the per-type default handler. The launched program
// is detached so the TUI is never blocked.
func shellOpen(path string) error {
	var bin string
	switch runtime.GOOS {
	case "darwin":
		bin = "open"
	default: // linux and others
		bin = "xdg-open"
	}
	cmd := exec.Command(bin, path)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// writeClipboard copies text to the system clipboard on Linux/macOS via a
// hidden child process (xclip / pbcopy). Failures are non-fatal.
func writeClipboard(text string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("pbcopy")
	case "linux":
		cmd = exec.Command("xclip", "-selection", "clipboard")
	default:
		return nil
	}
	cmd.Stdin = stringReader(text)
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
