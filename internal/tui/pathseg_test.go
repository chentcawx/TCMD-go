package tui

import (
	"testing"
)

func TestSplitPathSegments(t *testing.T) {
	cases := []struct {
		path   string
		expect []string
	}{
		{`C:\Users\chenwei\Documents`, []string{"C:", "Users", "chenwei", "Documents"}},
		{`/home/user/docs`, []string{"home", "user", "docs"}},
		{`C:\`, []string{"C:"}},
		{`C:\Windows`, []string{"C:", "Windows"}},
		{`D:\`, []string{"D:"}},
	}
	for _, c := range cases {
		got := splitPathSegments(c.path)
		if len(got) != len(c.expect) {
			t.Fatalf("splitPathSegments(%q) = %v (len=%d), want %v (len=%d)", c.path, got, len(got), c.expect, len(c.expect))
		}
		for i := range got {
			if got[i] != c.expect[i] {
				t.Fatalf("splitPathSegments(%q)[%d] = %q, want %q", c.path, i, got[i], c.expect[i])
			}
		}
	}
}

func TestPathPrefixAtSegment(t *testing.T) {
	cases := []struct {
		path   string
		segIdx int
		expect string
	}{
		{`C:\Users\chenwei\Documents`, 0, `C:`},
		{`C:\Users\chenwei\Documents`, 1, `C:\Users`},
		{`C:\Users\chenwei\Documents`, 2, `C:\Users\chenwei`},
		{`C:\Users\chenwei\Documents`, 3, `C:\Users\chenwei\Documents`},
		{`/home/user/docs`, 0, `/home`},
		{`/home/user/docs`, 1, `/home/user`},
		{`/home/user/docs`, 2, `/home/user/docs`},
		{`C:\`, 0, `C:\`},
		{`C:\Windows`, 1, `C:\Windows`},
		{`C:\Users\chenwei\Documents`, 5, ``}, // out of range
	}
	for _, c := range cases {
		got := pathPrefixAtSegment(c.path, c.segIdx)
		if got != c.expect {
			t.Fatalf("pathPrefixAtSegment(%q, %d) = %q, want %q", c.path, c.segIdx, got, c.expect)
		}
	}
}
