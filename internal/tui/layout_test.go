package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
	"tcmd/internal/fs"
)

func stripANSI(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		if r == 0x1b {
			in = true
			continue
		}
		if in {
			if r == 'm' {
				in = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func fakeEntries(prefix string, n int) []fs.Entry {
	out := make([]fs.Entry, 0, n)
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("%s_%03d", prefix, i)
		out = append(out, fs.Entry{Name: name, Path: name, IsDir: i%3 == 0, Size: int64(i) * 100})
	}
	return out
}

func cjkEntries(prefix string, n int) []fs.Entry {
	out := make([]fs.Entry, 0, n)
	cjk := []string{"文档", "报告", "资料", "项目", "图片", "音乐", "视频", "代码", "配置", "缓存"}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("%s%s_%d", prefix, cjk[i%len(cjk)], i)
		out = append(out, fs.Entry{Name: name, Path: name, IsDir: i%4 == 0, Size: int64(i) * 1024})
	}
	return out
}

// TestFormatEntryAlign checks that an ASCII filename and a CJK filename land
// their size column at the SAME display column (CJK = 2 cells, not 1).
func TestFormatEntryAlign(t *testing.T) {
	m := InitialModel()
	const w = 49
	ascii := fs.Entry{Name: "LEFT_015", Path: "x", IsDir: false, Size: 1500}
	cjk := fs.Entry{Name: "左文档_0", Path: "x", IsDir: false, Size: 1500}
	la := stripANSI(m.formatEntry(ascii, false, false, true, w))
	lc := stripANSI(m.formatEntry(cjk, false, false, true, w))
	sa := strings.Index(la, "1.5 KB")
	sc := strings.Index(lc, "1.5 KB")
	if sa < 0 || sc < 0 {
		t.Fatalf("size token not found: ascii=%q cjk=%q", la, lc)
	}
	// Compare DISPLAY columns, not byte indices: CJK is 3 bytes but 2 cells.
	dispA := runewidth.StringWidth(la[:sa])
	dispC := runewidth.StringWidth(lc[:sc])
	if dispA != dispC {
		t.Fatalf("CJK size column misaligned: ascii col=%d cjk col=%d\n ascii=%q\n cjk =%q", dispA, dispC, la, lc)
	}
}

// TestSeparatorStable verifies the vertical separator stays at one display
// column regardless of left-pane CJK content (the original jitter bug).
func TestSeparatorStable(t *testing.T) {
	m := InitialModel()
	m.width = 100
	m.height = 30
	m.panes[0].current().entries = cjkEntries("左", 20)
	m.panes[1].current().entries = fakeEntries("RIGHT", 6)
	out := stripANSI(m.View())
	want := -1
	for i, line := range strings.Split(out, "\n") {
		idx := strings.Index(line, "│")
		if idx < 0 {
			continue
		}
		col := runewidth.StringWidth(line[:idx])
		if want < 0 {
			want = col
		}
		if col != want {
			t.Fatalf("separator jittered: row %d col=%d (want %d) line=%q", i, col, want, line)
		}
	}
}

// TestLongNameNoOverflow is the regression test for the ".autocodertools"
// bug: a filename longer than the column must be truncated to the column
// width, never left to overflow. Without truncation the row widens the pane,
// shoving the separator and the other pane sideways ("显示异常").
func TestLongNameNoOverflow(t *testing.T) {
	m := InitialModel()
	m.width = 100
	m.height = 30
	m.panes[0].current().entries = []fs.Entry{
		{Name: strings.Repeat("a", 200), Path: "x", IsDir: false, Size: 100},
		{Name: strings.Repeat("文", 100), Path: "y", IsDir: false, Size: 200},
	}
	m.panes[1].current().entries = fakeEntries("RIGHT", 6)
	out := stripANSI(m.View())
	want := -1
	for i, line := range strings.Split(out, "\n") {
		idx := strings.Index(line, "│")
		if idx < 0 {
			continue
		}
		leftW := runewidth.StringWidth(line[:idx])
		if want < 0 {
			want = leftW
		}
		if leftW != want {
			t.Fatalf("row %d left-pane width jittered: %d (want %d) line=%q", i, leftW, want, line)
		}
		// every rendered line must fit within the terminal width
		if runewidth.StringWidth(line) > m.width {
			rightW := runewidth.StringWidth(line[idx+len("│"):])
			t.Logf("row %d: fullDW=%d leftW=%d rightW=%d line=%q", i, runewidth.StringWidth(line), leftW, rightW, line)
			t.Fatalf("row %d exceeds terminal width: %d > %d", i, runewidth.StringWidth(line), m.width)
		}
	}
}

// TestSeparatorWidthCountsInLayout locks in the fix for the 1-cell overflow
// bug: the vertical separator "│" is an ambiguous-width glyph that runewidth
// reports as 2 cells on CJK-locale terminals (not 1). The pane split must
// reserve the real separator width so left + sep + right == terminal width.
// If the code ever reverts to assuming a 1-cell separator, this test fails
// because the composed row would be one cell too wide and overflow.
func TestSeparatorWidthCountsInLayout(t *testing.T) {
	m := InitialModel()
	m.width = 100
	m.height = 30
	m.panes[0].current().entries = fakeEntries("LEFT", 6)
	m.panes[1].current().entries = fakeEntries("RIGHT", 6)
	out := stripANSI(m.View())
	sepW := runewidth.RuneWidth('│')
	if sepW < 1 {
		sepW = 1
	}
	want := m.width - sepW // left + right == width - separator
	for i, line := range strings.Split(out, "\n") {
		idx := strings.Index(line, "│")
		if idx < 0 {
			continue
		}
		leftW := runewidth.StringWidth(line[:idx])
		rightW := runewidth.StringWidth(line[idx+len("│"):])
		if leftW+rightW != want {
			t.Fatalf("row %d: left(%d)+right(%d)=%d, want %d (sepW=%d) line=%q",
				i, leftW, rightW, leftW+rightW, want, sepW, line)
		}
	}
}

// TestViewNeverExceedsHeight is the regression test for the non-maximized
// window bug: in a non-maximized Windows Terminal, the reported height can
// be 1-2 rows larger than the usable content area. If the View() output
// exceeds that usable area, the terminal scrolls and pushes the tab bar
// off-screen, shifting every subsequent row. This test asserts the View()
// output stays at or below the reported height for every realistic size.
func TestViewNeverExceedsHeight(t *testing.T) {
	for _, h := range []int{20, 24, 25, 30, 40, 50} {
		m := InitialModel()
		m.width = 120
		m.height = h
		m.panes[0].current().entries = cjkEntries("左", 100)
		m.panes[1].current().entries = fakeEntries("RIGHT", 100)
		out := stripANSI(m.View())
		lines := strings.Count(out, "\n") + 1
		if lines > h {
			t.Fatalf("height=%d: View() produced %d lines (must be <= h)", h, lines)
		}
	}
}
