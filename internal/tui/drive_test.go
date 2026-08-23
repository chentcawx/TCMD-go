package tui

import (
	"strings"
	"testing"
	tea "github.com/charmbracelet/bubbletea"
)

func TestListDrivesNotEmpty(t *testing.T) {
	drives := listDrives()
	if len(drives) == 0 {
		t.Fatal("listDrives returned empty slice")
	}
	foundC := false
	for _, d := range drives {
		if strings.HasPrefix(d, "C:") {
			foundC = true
		}
	}
	if !foundC {
		t.Logf("drives: %v", drives)
		t.Error("C: drive not found")
	}
}

func TestDrivePickerNavigateAndConfirm(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	m.panes[0].tabs[0] = newTab("C:\\")
	m.panes[1].tabs[0] = newTab("C:\\")

	m.beginDrivePicker()
	if m.ov != overlayDrivePicker {
		t.Fatalf("beginDrivePicker should open drive picker, ov=%d", m.ov)
	}
	if len(m.drives) == 0 {
		t.Skip("no drives available, skipping picker test")
	}
	if m.pickerIndex != 0 {
		t.Fatalf("pickerIndex should start at 0, got %d", m.pickerIndex)
	}

	// Save selected drive before it gets consumed.
	selectedDrive := m.drives[m.pickerIndex]

	m.handleDrivePickerKey(tea.KeyMsg{Type: tea.KeyDown})
	if m.pickerIndex != 1 {
		t.Fatalf("down should move to index 1, got %d", m.pickerIndex)
	}

	m.handleDrivePickerKey(tea.KeyMsg{Type: tea.KeyUp})
	if m.pickerIndex != 0 {
		t.Fatalf("up should move to index 0, got %d", m.pickerIndex)
	}

	m.handleDrivePickerKey(tea.KeyMsg{Type: tea.KeyEnter})
	if m.ov != overlayNone {
		t.Fatalf("enter should close picker, ov=%d", m.ov)
	}
	if m.panes[0].current().path != selectedDrive {
		t.Fatalf("confirm should switch to selected drive, got %s want %s",
			m.panes[0].current().path, selectedDrive)
	}
}

func TestDrivePickerCancel(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	m.panes[0].tabs[0] = newTab("C:\\")

	m.beginDrivePicker()
	m.handleDrivePickerKey(tea.KeyMsg{Type: tea.KeyEscape})
	if m.ov != overlayNone {
		t.Fatalf("esc should close picker, ov=%d", m.ov)
	}
	if len(m.drives) != 0 {
		t.Fatal("drives should be cleared after close")
	}
}

func TestBeginDrivePickerAnchorsCurrentDrive(t *testing.T) {
	m := newTestModel()
	m.width = 80
	m.height = 24
	drives := listDrives()
	if len(drives) < 2 {
		t.Skip("need at least 2 drives for anchor test")
	}
	m.panes[0].tabs[0] = newTab(drives[1])
	m.panes[1].tabs[0] = newTab(drives[1])

	m.beginDrivePicker()
	if m.pickerIndex != 1 {
		t.Fatalf("pickerIndex should anchor to current drive index 1, got %d", m.pickerIndex)
	}
}
