//go:build !windows

package tui

// listDrives returns a single "/" entry on non-Windows systems so the UI has
// something to display; drive-letter selection is a Windows-only concept.
func listDrives() []string {
	return []string{"/"}
}
