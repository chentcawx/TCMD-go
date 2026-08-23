package tui

import (
	"fmt"
	"testing"
	"time"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDbg4LoopDebug(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	t2 := &tab{
		path:     "C:\root",
		offset:   0,
		cursor:   0,
		entries:  makeDirEntries(10),
		selected: make(map[string]bool),
	}
	m.panes[0].tabs[0] = t2
	m.active = 0

	baseX := 2
	baseY := rowList
	maxDx := min(doubleClickTolerance, baseX)
	
	for dx := -maxDx; dx <= maxDx; dx++ {
		if dx == 0 { continue }
		m.lastClickT = time.Time{}
		m.lastClickX = 0
		m.lastClickY = 0
		cur := m.panes[0].current()
		cur.path = "C:\root"
		cur.cursor = 0

		m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: baseX, Y: baseY})
		fmt.Printf("dx=%d after 1st: path=%s cursor=%d\n", dx, m.panes[0].current().path, m.panes[0].current().cursor)

		m.handleMouse(tea.MouseMsg{Button: tea.MouseButtonLeft, Action: tea.MouseActionPress, X: baseX + dx, Y: baseY})
		fmt.Printf("dx=%d after 2nd: path=%s cursor=%d\n", dx, m.panes[0].current().path, m.panes[0].current().cursor)
	}
}
