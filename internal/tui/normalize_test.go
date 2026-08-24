package tui

import (
	"testing"
)

func TestNormalizeTabPath(t *testing.T) {
	cases := []struct {
		input  string
		expect string
	}{
		{"E:.", "E:\\"},
		{"E:", "E:\\"},
		{"E:\\", "E:\\"},
		{"C:\\Users\\chenwei", "C:\\Users\\chenwei"},
		{"D:\\", "D:\\"},
		{"/home/user", "/home/user"},
		{"", ""},
		{"E:..\\foo", "E:..\\foo"}, // not a root — dot-dot doesn't match trimRight
	}
	for _, c := range cases {
		got := normalizeTabPath(c.input)
		if got != c.expect {
			t.Errorf("normalizeTabPath(%q) = %q, want %q", c.input, got, c.expect)
		}
	}
}
