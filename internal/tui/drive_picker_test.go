package tui

import (
	"strings"
	"testing"
)

func TestDrivePickerRenderedLines(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	m.drives = []string{"C:\\", "D:\\", "E:\\"}
	m.pickerIndex = 0
	m.ov = overlayDrivePicker

	out := m.renderDrivePicker()
	lines := strings.Split(out, "\n")
	t.Logf("Total lines: %d", len(lines))
	for i, line := range lines {
		t.Logf("line[%d]: %q", i, line)
	}
}
