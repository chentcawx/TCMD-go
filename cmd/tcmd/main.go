// Command tcmd is a Total-Commander-style dual-pane file manager written in Go.
//
// Design goals (from the project brief):
//   - Dual-pane layout with multiple tabs per pane, mirroring Total Commander.
//   - Zero web controls — a native terminal TUI (bubbletea), keeping resource
//     usage low and the binary small.
//   - Runs on both Windows 32-bit (GOARCH=386) and 64-bit (GOARCH=amd64).
package main

import (
	"fmt"
	"os"

	"github.com/charmbracelet/bubbletea"

	"tcmd/internal/tui"
)

func main() {
	// AltScreen gives tcmd a full-window canvas like a desktop file manager;
	// without it the panes would fight with the shell scrollback.
	m := tui.InitialModel()
	// Restore the last session (open tabs/paths per pane) if a config exists.
	if cfg, err := tui.LoadConfig(); err == nil {
		m.ApplyConfig(cfg)
	}
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tcmd 错误:", err)
		os.Exit(1)
	}
	// Teardown: drain and stop the job queue so in-flight copies don't leak.
	m.QueueStop()
}
