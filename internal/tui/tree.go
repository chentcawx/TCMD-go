// Package tui — directory tree overlay.
//
// Shows a recursive listing of a directory with per-node counts (subdirs,
// files, total size) and a root-level summary. Stat runs asynchronously in
// a background goroutine so the TUI never freezes on deep or large trees.
package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"tcmd/internal/fs"
)

// treeDir is one node in the directory tree. Children are populated after the
// async stat completes.
type treeDir struct {
	path     string
	name     string
	depth    int // 0 = root
	dirCount int // direct subdirectories only
	fileCount int // direct files only
	size     int64 // recursive total bytes of all files under this node
	children []*treeDir
	loaded   bool // true once the async stat has finished
}

// treeStats is the payload delivered back to the bubbletea event loop.
type treeStats struct {
	root *treeDir
	err  error
}

// buildTree walks dir and returns a treeDir rooted at dir. The walk is
// synchronous here — callers should run this in a goroutine and deliver the
// result via a tea.Cmd so the TUI stays responsive.
func buildTree(dir string) *treeDir {
	info, err := os.Stat(dir)
	if err != nil {
		return &treeDir{path: dir, name: filepath.Base(dir), loaded: true}
	}
	t := &treeDir{
		path:  dir,
		name:  info.Name(),
		depth: 0,
	}
	t.loadChildren()
	return t
}

func (t *treeDir) loadChildren() {
	entries, err := os.ReadDir(t.path)
	if err != nil {
		return
	}
	// Collect only subdirectories; files contribute to the count but aren't
	// recursed into (tree shows directories only).
	var dirs []os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e)
		}
	}
	sort.Slice(dirs, func(i, j int) bool {
		return dirs[i].Name() < dirs[j].Name()
	})
	t.dirCount = len(dirs)
	// Count direct files and accumulate their sizes.
	fileEntries, err := os.ReadDir(t.path)
	if err == nil {
		for _, e := range fileEntries {
			if !e.IsDir() {
				t.fileCount++
				if fi, err := e.Info(); err == nil {
					t.size += fi.Size()
				}
			}
		}
	}
	for _, d := range dirs {
		child := &treeDir{
			path:  filepath.Join(t.path, d.Name()),
			name:  d.Name(),
			depth: t.depth + 1,
		}
		child.loadChildren()
		t.size += child.size // accumulate recursive size
		t.children = append(t.children, child)
	}
	t.loaded = true
}

// totalDirs / totalFiles / totalSize return aggregate counts across the whole
// subtree rooted at t. Used for the root summary line.
func (t *treeDir) totalDirs() int {
	n := t.dirCount
	for _, c := range t.children {
		n += c.totalDirs()
	}
	return n
}

func (t *treeDir) totalFiles() int {
	n := t.fileCount
	for _, c := range t.children {
		n += c.totalFiles()
	}
	return n
}

// Entry returns the fs.Entry-like snapshot for this node (used by tests and
// the render path).
func (t *treeDir) Entry() fs.Entry {
	return fs.Entry{
		Name:  t.name,
		Path:  t.path,
		IsDir: true,
		Size:  t.size,
	}
}

// AsyncTreeStat runs buildTree in a goroutine and delivers the result to ch.
// A context deadline protects against very deep trees stalling forever.
func AsyncTreeStat(dir string, ch chan<- treeStats) {
	done := make(chan *treeDir)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		done <- buildTree(dir)
	}()
	select {
	case root := <-done:
		ch <- treeStats{root: root}
	case <-time.After(10 * time.Second):
		ch <- treeStats{err: fmt.Errorf("目录统计超时（10s），请尝试在较小的目录下查看")}
	}
	wg.Wait()
	close(ch)
}

// fmtNode renders one treeDir as a single display row, with tree-drawing
// prefixes and per-node counts.
func fmtNode(t *treeDir, prefix string) string {
	nameStyle := dirStyle.Render("📁 " + t.name)
	counts := fmt.Sprintf("  %d 目  %d 文件  %s", t.dirCount, t.fileCount, humanSize(t.size))
	return prefix + "├─ " + nameStyle + counts
}

// fmtTree renders the full tree into lines, using box-drawing characters.
// maxW caps each line's display width.
func fmtTree(root *treeDir, maxW int) []string {
	if root == nil {
		return []string{"  （空目录）"}
	}
	var lines []string
	// Root summary line.
	summary := fmt.Sprintf("  📁 %s   共 %d 目录  %d 文件  %s",
		root.name, root.totalDirs(), root.totalFiles(), humanSize(root.size))
	lines = append(lines, titleStyle.Render(truncateDW(summary, maxW)))
	lines = append(lines, "")
	// Recurse.
	renderNode(root, "", &lines, maxW)
	return lines
}

func renderNode(t *treeDir, prefix string, lines *[]string, maxW int) {
	// Choose connector: last child uses └─, others use ├─.
	conn := "├─ "
	if len(t.children) == 0 {
		conn = "└─ "
	}
	// Root has no prefix; render its children with proper branching.
	for i, c := range t.children {
		isLast := i == len(t.children)-1
		childPrefix := prefix
		if prefix == "" {
			// Root level: no prefix, just the connector.
			childPrefix = ""
		} else if isLast {
			childPrefix = prefix + "    "
		} else {
			childPrefix = prefix + "│   "
		}
		connector := conn
		if isLast {
			connector = "└─ "
		} else {
			connector = "├─ "
		}
		line := connector + dirStyle.Render("📁 "+c.name) +
			fmt.Sprintf("  %d 目  %d 文件  %s", c.dirCount, c.fileCount, humanSize(c.size))
		*lines = append(*lines, truncateDW(line, maxW))
		renderNode(c, childPrefix, lines, maxW)
	}
}

// flattenTree returns the visible nodes in pre-order (matching the row order
// produced by fmtTree/renderNode), excluding the root summary line. The
// returned slice is what treeCursor indexes into, and Enter descends into the
// selected node's path.
func flattenTree(root *treeDir) []*treeDir {
	var out []*treeDir
	if root == nil {
		return out
	}
	var walk func(t *treeDir)
	walk = func(t *treeDir) {
		for _, c := range t.children {
			out = append(out, c)
			walk(c)
		}
	}
	walk(root)
	return out
}
