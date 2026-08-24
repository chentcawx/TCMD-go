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

// pathPrefixAtSegment must always return a canonical 4-char Windows root
// ("X:\") when segIdx=0 and the path has only one segment. This prevents
// legacy malformed variants like "E:" or "E:." from leaking into the UI.
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
		// Windows drive root must be canonical "X:\" (4 chars).
		{`C:\`, 0, `C:\`},
		{`D:\`, 0, `D:\`},
		{`E:\`, 0, `E:\`},
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

// TestPathPrefixAtSegmentRootIsCanonical verifies that double-click on the
// drive letter segment of a root path always yields the canonical form "X:\"
// (3 chars), not a malformed variant like "E:" or "E:.".
func TestPathPrefixAtSegmentRootIsCanonical(t *testing.T) {
	for _, drive := range []string{"C:\\", "D:\\", "E:\\"} {
		got := pathPrefixAtSegment(drive, 0)
		if len(got) != 3 {
			t.Fatalf("pathPrefixAtSegment(%q, 0) = %q (len=%d), want 3 chars", drive, got, len(got))
		}
		if got != drive {
			t.Fatalf("pathPrefixAtSegment(%q, 0) = %q, want %q", drive, got, drive)
		}
	}
}
