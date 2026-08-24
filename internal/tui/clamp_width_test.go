package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"tcmd/internal/fs"
)

// TestViewRowsStayWithinWidth is the regression test for the "non-maximized
// window" symptom: the right pane's tab labels appeared to bleed into the
// left pane. The root cause was bubbletea reporting a content width (via
// tea.WindowSizeMsg) that was one or two cells wider than the actual
// drawable area of a non-maximized Windows Terminal, so lipgloss rendered
// rows that exceeded the terminal's true width, the terminal wrapped them,
// and the right pane's tabs smeared onto the left pane's tabs row.
//
// View() must always clamp every row to m.width, regardless of whether
// renderPane / lipgloss.JoinHorizontal already produced an on-budget row.
func TestViewRowsStayWithinWidth(t *testing.T) {
	for _, w := range []int{40, 60, 80, 100, 120, 140, 160, 180, 200} {
		m := &model{
			panes: [2]*pane{
				{
					tabs: []*tab{
						{path: `C:\Users\chenwei\..codewhale`},
						{path: `C:\Users\chenwei\.codewhale`},
						{path: `C:\Users\chenwei\.codewhale`, entries: fakeEntries("file", 5)},
					},
					active: 2,
				},
				{
					tabs: []*tab{
						{path: `C:\Users`, entries: append(fakeEntries("user", 5),
							fs.Entry{Name: "会议记录——2026年最终版交付文档.md", Path: "x", IsDir: false, Size: 12345})},
						{path: `C:\Users\.auto-coder`},
					},
					active: 0,
				},
			},
			active: 1,
			width:  w,
			height: 30,
		}
		out := m.View()
		// Inspect raw ANSI output (clampRowWidth preserves ANSI sequences;
		// lipgloss.Width counts only visible cells, which is what we care
		// about for "fits on terminal" assertions).
		for i, line := range strings.Split(out, "\n") {
			dw := lipgloss.Width(line)
			if dw > w {
				t.Fatalf("width=%d row %d: line display width %d > %d\nline=%q", w, i, dw, w, line)
			}
			// Second oracle: the TRUE painted width in an East-Asian
			// terminal counts ambiguous glyphs (em dash '—', full-width
			// punctuation) as 2 cells. lipgloss.Width under-counts those,
			// so it would miss an em-dash overflow. Strip ANSI then measure
			// with runewidth — this is the width the terminal actually
			// paints, and the one that triggers the phantom wrap line.
			painted := runewidth.StringWidth(stripANSI(line))
			if painted > w {
				t.Fatalf("width=%d row %d: painted width %d > %d (em-dash/font overflow would wrap)\nline=%q", w, i, painted, w, line)
			}
		}
	}
}

// TestClampRowWidthLeavesAnsiIntact: when clamping kicks in, the leftover
// ANSI escape sequences must not bleed onto the next rendered row. A naive
// byte-level truncate would leave a dangling reset/color code that wipes
// the style of every following row, producing black-on-white background
// stripes across the rest of the screen. truncateDW cuts by display width
// and leaves escape sequences alone, so this is naturally safe — keep this
// assertion as a guard against regressions to a byte-level clamp.
func TestClampRowWidthLeavesAnsiIntact(t *testing.T) {
	styled := "\x1b[44m" + strings.Repeat("a", 50) + "\x1b[0m"
	out := clampRowWidth(styled, 20)
	// The trimmed line must close its color reset (so the next row starts
	// with normal styling). If clampRowWidth ever evolves to byte-truncate
	// we want this test to catch the missing reset before it ships.
	if !strings.HasSuffix(out, "\x1b[0m") {
		t.Fatalf("clampRowWidth dropped trailing ANSI reset; got %q", out)
	}
	if w := lipgloss.Width(out); w > 20 {
		t.Fatalf("clamped line still visually wider than max: %d", w)
	}
}

// TestTruncateDWPreservesAnsi is the regression test for the actual bug the
// user reported: a long tab bar (3 styled tabs) being truncated in a narrow
// pane dropping the trailing reset, which made the rest of the screen — and
// especially the right pane after JoinHorizontal — inherit the active tab's
// background colour, looking like "right pane tabs bleeding into the left
// pane". truncateDW must keep every complete escape sequence intact and
// close the trailing one with \x1b[0m when truncating mid-stream.
func TestTruncateDWPreservesAnsi(t *testing.T) {
	// Build a styled tab bar similar to what renderTabs produces.
	styled := "\x1b[1;37;44m users \x1b[0m \x1b[37m .codewhale \x1b[0m \x1b[1;37;44m .codewhale \x1b[0m \x1b[37m .codewhale \x1b[0m"

	// Pass 1: row already fits — must not gain a synthetic reset.
	out := truncateDW(styled, 200)
	if strings.HasSuffix(out, "\x1b[0m\x1b[0m") {
		t.Fatalf("truncateDW synthesised a duplicate reset: %q", out)
	}

	// Pass 2: truncation kicks in. Must always end with a reset
	// so the next rendered row does not inherit background colour.
	for _, max := range []int{5, 10, 20, 30, 40, 60} {
		out := truncateDW(styled, max)
		if w := lipgloss.Width(out); w > max {
			t.Fatalf("max=%d: output still wider than budget: %d (%q)", max, w, out)
		}
		if !strings.HasSuffix(out, "\x1b[0m") {
			t.Fatalf("max=%d: missing trailing reset; output=%q", max, out)
		}
	}

	// Pass 3: even ANSI sequences that themselves exceed max must stay whole
	// — we cannot half-copy an escape.
	colouredOnly := "\x1b[1;37;44m" + strings.Repeat("z", 4) + "\x1b[0m"
	out = truncateDW(colouredOnly, 2) // only room for the ANSI block, no visible runes
	if w := lipgloss.Width(out); w > 2 {
		t.Fatalf("ANSI-only output exceeded budget; cells=%d out=%q", w, out)
	}
}

// TestTruncateDWEmptyAndTiny covers the lowest-rung edge cases: empty input,
// max<=0, max=1, and truncation leaving zero visible runes.
func TestTruncateDWEmptyAndTiny(t *testing.T) {
	if got := truncateDW("", 10); got != "" {
		t.Fatalf("empty input should return empty string; got %q", got)
	}
	if got := truncateDW("hello", 0); got != "" {
		t.Fatalf("max=0 should drop everything; got %q", got)
	}
	if got := truncateDW("hello", 1); lipgloss.Width(got) > 1 {
		t.Fatalf("max=1 should leave at most one cell; got %q (cells=%d)", got, lipgloss.Width(got))
	}
}
