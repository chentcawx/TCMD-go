//go:build windows

package tui

import (
	"syscall"
	"unsafe"
)

// listDrives enumerates all logical drives on the current Windows system and
// returns them sorted by letter. Each entry is a root path like "C:\".
//
// We use GetLogicalDriveStringsW (kernel32.dll) which is the standard,
// CGO-free way to enumerate drives from a pure-Go program. The result is a
// single null-terminated string buffer: "C:\0D:\0E:\0\0".
func listDrives() []string {
	// First call to get the required buffer size.
	bufSize := getLogicalDriveStrings(0, nil)
	if bufSize == 0 {
		return nil
	}
	// Allocate buffer (each drive string is at most 4 wchar units).
	buf := make([]uint16, bufSize)
	n := getLogicalDriveStrings(bufSize, &buf[0])
	if n == 0 {
		return nil
	}
	// Parse the null-separated strings.
	var drives []string
	start := 0
	for i := uint32(0); i < n; i++ {
		if buf[i] == 0 {
			if uintptr(i) > uintptr(start) {
				s := syscall.UTF16ToString(buf[start:int(i)])
				drives = append(drives, s)
			}
			start = int(i) + 1
		}
	}
	// Sort by drive letter for deterministic display order.
	sortStrings(drives)
	return drives
}

// getLogicalDriveStrings is the Windows API declaration via LazyDLL.
var getLogicalDriveStrings = func() func(uint32, *uint16) uint32 {
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetLogicalDriveStringsW")
	return func(n uint32, buf *uint16) uint32 {
		r, _, _ := proc.Call(uintptr(n), uintptr(unsafe.Pointer(buf)))
		return uint32(r)
	}
}()

// sortStrings sorts a string slice in place (ASCII/Unicode codepoint order,
// which for drive letters is the same as alphabetical).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
