// Package tui — application-wide constants.
//
// Centralizing magic numbers here makes them easy to find, tune, and document.
package tui

import "time"

// Job queue tuning.
const (
	// defaultMaxWorkers is the number of concurrent copy/move workers.
	defaultMaxWorkers = 2
	// maxConcurrentJobs is the enqueue channel capacity; jobs beyond this are
	// rejected immediately (non-blocking) so the TUI event loop never stalls.
	maxConcurrentJobs = 10
)

// Viewer limits.
const (
	// maxViewBytes is the hard cap on built-in text viewer input. Files larger
	// than this are rejected to prevent OOM on very large text files.
	maxViewBytes = 5 * 1024 * 1024 // 5 MiB
)

// Tree overlay timeout.
const (
	// treeStatTimeout is the maximum time AsyncTreeStat will wait before
	// returning a timeout error. Deeply nested directories should not hang the
	// TUI indefinitely.
	treeStatTimeout = 10 * time.Second
)

