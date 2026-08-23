// Package fs provides cross-platform filesystem primitives for the tcmd file
// manager: directory listing, copy, move, delete, mkdir, and selection size.
//
// It deliberately avoids any GUI/web dependency — pure stdlib plus a thin
// per-OS hidden-attribute helper (fs_windows.go / fs_unix.go) so the same
// code builds for both GOARCH=386 and amd64 on Windows.
package fs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// copyBufSize is the chunk used while streaming file copies, so a multi-GB
// file never spikes heap by being read whole.
const copyBufSize = 32 * 1024

// ProgressFunc is called during a copy/move operation to report real-time
// progress. doneBytes/totalBytes are 0 when total is unknown (e.g. recursive
// directory copy where stat-all-upfront would be too expensive). currentPath is
// the path currently being processed. The callback runs on a background
// goroutine; callers must be safe for concurrent use (e.g. send to a channel
// rather than mutating shared state directly).
type ProgressFunc func(doneBytes, totalBytes int64, currentPath string)

// Entry is one directory entry with the fields tcmd's UI needs.
type Entry struct {
	Name     string
	Path     string
	IsDir    bool
	Size     int64
	ModTime  time.Time
	Mode     os.FileMode
	IsHidden bool
}

// ListDir returns the entries of dir, sorted directories-first then by
// case-insensitive name. Failures to open/read the directory abort the whole
// listing rather than silently returning a partial result.
func ListDir(dir string) ([]Entry, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, fmt.Errorf("打开目录失败 %q: %w", dir, err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			// 目录句柄关闭失败属非致命，记录而非静默忽略。
			fmt.Fprintf(os.Stderr, "[fs] 关闭目录失败 %q: %v\n", dir, cerr)
		}
	}()

	infos, err := f.Readdir(-1)
	if err != nil {
		return nil, fmt.Errorf("读取目录失败 %q: %w", dir, err)
	}

	entries := make([]Entry, 0, len(infos))
	for _, fi := range infos {
		full := filepath.Join(dir, fi.Name())
		entries = append(entries, Entry{
			Name:     fi.Name(),
			Path:     full,
			IsDir:    fi.IsDir(),
			Size:     fi.Size(),
			ModTime:  fi.ModTime(),
			Mode:     fi.Mode(),
			IsHidden: isHidden(full, fi),
		})
	}
	sortEntries(entries)
	return entries, nil
}

// sortEntries orders directories before files, then by case-insensitive name —
// matching the conventional dual-pane-file-manager layout.
func sortEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

// CopyProgress copies src to dst with per-file progress reporting via cb.
// See Copy for semantics; this variant is the async-friendly counterpart used
// by the job queue so the UI can show real-time progress.
func CopyProgress(src, dst string, cb ProgressFunc) error {
	fi, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat 源失败 %q: %w", src, err)
	}
	if fi.IsDir() {
		return copyDirProgress(src, dst, cb)
	}
	return copyFileProgress(src, dst, fi.Mode(), cb)
}

func copyFileProgress(src, dst string, mode os.FileMode, cb ProgressFunc) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开源文件失败 %q: %w", src, err)
	}
	defer in.Close()

	total := fiSize(src)
	if cb != nil {
		cb(0, total, src)
	}

	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return fmt.Errorf("创建目标目录失败 %q: %w", filepath.Dir(dst), err)
	}
	out, err := os.Create(dst)
	if err != nil {
		return fmt.Errorf("创建目标文件失败 %q: %w", dst, err)
	}
	defer func() {
		if cerr := out.Close(); cerr != nil {
			fmt.Fprintf(os.Stderr, "[fs] 关闭目标文件失败 %q: %v\n", dst, cerr)
		}
	}()

	buf := make([]byte, copyBufSize)
	written, err := io.CopyBuffer(out, in, buf)
	if err != nil {
		return fmt.Errorf("复制内容失败 %q -> %q: %w", src, dst, err)
	}
	if cb != nil {
		cb(written, total, src)
	}
	// 尽力复制权限位；权限错误不应中断复制（Windows 上部分位无意义）。
	if err := os.Chmod(dst, mode); err != nil {
		fmt.Fprintf(os.Stderr, "[fs] 设置权限失败 %q: %v\n", dst, err)
	}
	return nil
}

func copyDirProgress(src, dst string, cb ProgressFunc) error {
	if cb != nil {
		cb(0, 0, src)
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return fmt.Errorf("创建目标目录失败 %q: %w", dst, err)
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("读取源目录失败 %q: %w", src, err)
	}
	for _, e := range entries {
		s := filepath.Join(src, e.Name())
		d := filepath.Join(dst, e.Name())
		if err := CopyProgress(s, d, cb); err != nil {
			return err
		}
	}
	return nil
}

// Copy copies src to dst, recursing into directories. Existing files at dst are
// overwritten. Large files are streamed in copyBufSize chunks. This is the
// synchronous counterpart used by the legacy sync path; use CopyProgress for
// async jobs that need progress reporting.
func Copy(src, dst string) error {
	return CopyProgress(src, dst, nil)
}

// MoveProgress moves src to dst with per-file progress reporting. See Move for
// semantics; same-file-volume renames are instantaneous and report no progress.
func MoveProgress(src, dst string, cb ProgressFunc) error {
	if filepath.VolumeName(src) == filepath.VolumeName(dst) {
		if err := os.Rename(src, dst); err == nil {
			return nil
		}
		// rename 可能因打开句柄/权限失败，回退复制+删除。
	}
	if err := CopyProgress(src, dst, cb); err != nil {
		return err
	}
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("移动后清理源失败 %q: %w", src, err)
	}
	return nil
}

// fiSize returns the file size from os.Stat, or 0 on error. Used as the
// "totalBytes" hint for progress callbacks.
func fiSize(path string) int64 {
	if fi, err := os.Stat(path); err == nil {
		return fi.Size()
	}
	return 0
}

// Move moves src to dst. On the same volume it is a cheap rename; across
// volumes it falls back to copy-then-delete so the operation always succeeds.
func Move(src, dst string) error {
	if filepath.VolumeName(src) == filepath.VolumeName(dst) {
		if err := os.Rename(src, dst); err == nil {
			return nil
		}
		// rename 可能因打开句柄/权限失败，回退复制+删除。
	}
	if err := Copy(src, dst); err != nil {
		return err
	}
	if err := os.RemoveAll(src); err != nil {
		return fmt.Errorf("移动后清理源失败 %q: %w", src, err)
	}
	return nil
}

// Delete removes path recursively. The caller must confirm with the user
// before invoking — tcmd's UI does this.
func Delete(path string) error {
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("删除失败 %q: %w", path, err)
	}
	return nil
}

// Mkdir creates dir (and any necessary parents).
func Mkdir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建目录失败 %q: %w", dir, err)
	}
	return nil
}

// SelectedSize returns the sum of file sizes among entries. Directory sizes are
// not recursed in this fast path (the UI shows "<DIR>" for them); a full
// recursive computation can be added later as a background job.
func SelectedSize(entries []Entry) int64 {
	var total int64
	for _, e := range entries {
		if !e.IsDir {
			total += e.Size
		}
	}
	return total
}
