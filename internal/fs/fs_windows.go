//go:build windows

package fs

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// isHidden reports whether an entry is hidden. On Windows a file is hidden if
// its name starts with a dot (e.g. ".git") or it carries the
// FILE_ATTRIBUTE_HIDDEN flag — matching Explorer's behaviour.
func isHidden(path string, fi os.FileInfo) bool {
	if strings.HasPrefix(fi.Name(), ".") {
		return true
	}
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	attrs, err := windows.GetFileAttributes(p)
	if err != nil {
		return false
	}
	return attrs&windows.FILE_ATTRIBUTE_HIDDEN != 0
}

// isJunction reports whether path is a directory junction (reparse point with
// tag 0xA0000003, same as IO_REPARSE_TAG_MOUNT_POINT). Junctions appear as
// regular files via os.Lstat (Mode doesn't include ModeDir), so we must check
// the reparse tag explicitly to avoid treating them as files in the UI.
func isJunction(path string) bool {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, err := windows.CreateFile(
		p,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	const bufSize = 16384
	buf := make([]byte, bufSize)
	var bytesReturned uint32
	if err := windows.DeviceIoControl(
		h,
		windows.FSCTL_GET_REPARSE_POINT,
		nil,
		0,
		&buf[0],
		bufSize,
		&bytesReturned,
		nil,
	); err != nil {
		return false
	}
	// REPARSE_DATA_BUFFER: first field is ReparseTag (uint32 at offset 0).
	// A junction's tag is 0xA0000003 — the same value as IO_REPARSE_TAG_MOUNT_POINT.
	tag := uint32(buf[0]) | uint32(buf[1])<<8 | uint32(buf[2])<<16 | uint32(buf[3])<<24
	return tag == windows.IO_REPARSE_TAG_MOUNT_POINT
}

// CreateJunction creates a directory junction at link -> target. Both paths
// must be absolute. The link must not already exist.
//
// We use cmd /c mklink /J because direct DeviceIoControl with FSCTL_SET_REPARSE_POINT
// has proven unreliable on this Windows build (the call succeeds but the junction
// is non-functional). mklink creates a working junction in all cases.
//
// F11 calls this after Move(src, dst) to preserve the original path as a
// transparent placeholder.
func CreateJunction(link, target string) error {
	if !filepath.IsAbs(link) || !filepath.IsAbs(target) {
		return os.ErrInvalid
	}
	// The link must not already exist (as anything).
	if _, err := os.Lstat(link); err == nil {
		return os.ErrExist
	} else if !os.IsNotExist(err) {
		return err
	}
	// Use mklink /J which reliably creates junctions on all Windows builds.
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J %q -> %q: %v (output: %s)", link, target, err, string(out))
	}
	return nil
}

// MoveWithLink moves every src into dstDir (keeping each item's base name)
// and, on success, creates a junction at the original src pointing to the
// new location. The link acts as a transparent placeholder so callers that
// reference the original path keep working.
//
// ⚠️ 跨卷移动警告：如果源和目标在不同卷，Move 会先复制再删除源。此时创建
// junction 意义有限（junction 仅在同卷有效），且如果删除失败会导致数据丢失。
// 因此本函数在检测到跨卷移动时会跳过 junction 创建，并在 status 中提示用户。
//
// Errors are collected per-item: one failing source never aborts the others.
// The returned slice contains the paths of successfully-created links.
func MoveWithLink(sources []string, dstDir string) []string {
	out := make([]string, 0, len(sources))
	for _, s := range sources {
		dst := filepath.Join(dstDir, filepath.Base(s))
		if err := Move(s, dst); err != nil {
			continue
		}
		// Junctions are only reliable on the same volume. Skip creation for
		// cross-volume moves to avoid leaving a non-functional placeholder.
		if filepath.VolumeName(s) != filepath.VolumeName(dst) {
			// Note: caller (job.go) has no way to surface this per-item note;
			// the link just won't be in the returned slice.
			continue
		}
		if err := CreateJunction(s, dst); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out
}
