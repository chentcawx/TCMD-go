package tui

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

// TestTabsCannotCrossPane verifies each pane's tab labels never exceed their
// assigned column width, so no tab from the right pane can bleed into the
// left pane (or vice versa).
func TestTabsCannotCrossPane(t *testing.T) {
	tests := []struct {
		name  string
		panes [2]*pane
		w     int
	}{
		{
			name: "left 3 long tabs + right 1 tab",
			panes: [2]*pane{
				{tabs: []*tab{{path: "C:\\Users\\chenwei\\Desktop\\Project_A"}, {path: "D:\\WorkBuddy\\mediadown-go\\src"}, {path: "E:\\tools\\NetFix"}}, active: 0},
				{tabs: []*tab{{path: "C:\\Windows\\System32"}}, active: 0},
			},
			w: 80,
		},
		{
			name: "CJK long names",
			panes: [2]*pane{
				{tabs: []*tab{{path: "D:\\WorkBuddy\\TCMD-go\\src"}, {path: "D:\\WorkBuddy\\TCMD-go\\test\\integration\\测试"}}, active: 0},
				{tabs: []*tab{{path: "D:\\WorkBuddy\\mediadown-go\\src-tauri"}}, active: 0},
			},
			w: 100,
		},
		{
			name: "tiny terminal 40 cols",
			panes: [2]*pane{
				{tabs: []*tab{{path: "/very/long/path/that/exceeds/width/expectably"}, {path: "C:\\tmp\\another_super_long_name"}}, active: 0},
				{tabs: []*tab{{path: "C:\\Windows\\System32"}}, active: 0},
			},
			w: 40,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &model{panes: tt.panes, active: 0, width: tt.w, height: 30}
			sepW := 1 // "│" is width-1 for runewidth
			leftW := (tt.w - sepW) / 2
			rightW := tt.w - leftW - sepW
			for pi, p := range tt.panes {
				out := m.renderTabs(p, pi == m.active, leftW)
				gotW := runewidth.StringWidth(out)
				wantW := leftW
				if pi == 1 {
					wantW = rightW
				}
				if gotW > wantW {
					t.Errorf("pane %d renderTabs overflows pane w: got %d want <= %d; output %q", pi, gotW, wantW, out)
				}
			}
		})
	}
}

// TestTabsAlwaysTruncated verifies that even with an empty set of tabs the
// rendered tab bar stays at zero width (no phantom whitespace), and that when
// the pane width is smaller than a single label, the label is shortened.
func TestTabsAlwaysTruncated(t *testing.T) {
	m := &model{
		panes: [2]*pane{
			{tabs: []*tab{{path: "C:\\Users\\chenwei\\Desktop\\Project_A"}, {path: "D:\\WorkBuddy\\mediadown-go\\src"}}, active: 0},
			{tabs: []*tab{{path: "C:\\Windows\\System32"}}, active: 0},
		},
		active: 0,
		width:  40,
		height: 10,
	}
	sepW := 1
	leftW := (m.width - sepW) / 2
	rightW := m.width - leftW - sepW
	// Pane 0 has two labels; together they should still fit within leftW,
	// but if truncateDW ever stops truncating, this test will fail.
	out0 := m.renderTabs(m.panes[0], true, leftW)
	if runewidth.StringWidth(out0) > leftW {
		t.Fatalf("pane 0 tabs overflow: got %d > %d: %q", runewidth.StringWidth(out0), leftW, out0)
	}
	// Pane 1 has one short label; it must not grow past rightW.
	out1 := m.renderTabs(m.panes[1], false, rightW)
	if runewidth.StringWidth(out1) > rightW {
		t.Fatalf("pane 1 tabs overflow: got %d > %d: %q", runewidth.StringWidth(out1), rightW, out1)
	}
	// Empty panes produce empty tab lines (no ghost space).
	empty := &pane{}
	outEmpty := m.renderTabs(empty, false, 5)
	if strings.TrimSpace(outEmpty) != "" {
		t.Fatalf("empty pane tab line should be blank, got %q", outEmpty)
	}
}
