// Package tui — job queue for async copy/move operations.
//
// A job runs in its own goroutine and reports progress via a channel back to
// the bubbletea event loop. The queue drains at most maxWorkers items in
// parallel; everything else waits in the inbound channel until a worker
// becomes free. Cancellation is whole-job: calling Cancel on a running job
// stops it immediately (in-flight files may be left half-written on the
// destination).
package tui

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"tcmd/internal/fs"
)

// JobType distinguishes copy from move so the UI can label the queue entry.
type JobType int

const (
	JobCopy JobType = iota
	JobMove
)

func (t JobType) String() string {
	switch t {
	case JobCopy:
		return "复制"
	case JobMove:
		return "移动"
	default:
		return "?"
	}
}

// Job is one in-flight or pending copy/move operation.
type Job struct {
	id        int64
	typ       JobType
	sources   []string // items being copied/moved
	dstDir    string
	cancel    context.CancelFunc
	done      chan error // closed when the job reaches terminal state
	startedAt time.Time

	// cancelled is set atomically by Cancel(); checked after the IO op finishes.
	cancelled int32 // 0 = not cancelled, 1 = cancelled
	// doneFlag is set atomically when markDone is called; avoids the gotcha
	// that reading from j.done drains the buffer and makes subsequent reads
	// appear as "not done".
	doneFlag int32 // 0 = running, 1 = terminal
	// mu protects err and endTime (terminal-state bookkeeping).
	mu      sync.RWMutex
	err     error
	endTime time.Time
}

// Stats returns elapsed time and final error for a terminal job. Zero values
// are returned for a still-running job (elapsed is approximate).
func (j *Job) Stats() (elapsed time.Duration, err error) {
	elapsed = time.Since(j.startedAt)
	j.mu.RLock()
	err = j.err
	j.mu.RUnlock()
	return
}

// IsDone reports whether the job has reached terminal state.
func (j *Job) IsDone() bool {
	return atomic.LoadInt32(&j.doneFlag) != 0
}

// Done returns the job's done channel so callers can wait on it.
func (j *Job) Done() <-chan error { return j.done }

// Err returns the terminal error, or nil on success. Safe to call before
// IsDone() returns true (returns nil in that case).
func (j *Job) Err() error {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.err
}

// ProgressMsg is sent by the queue worker each time a file transfer boundary
// is reached so the UI can update the progress bar. Bytes are per-file
// granularity; total is 0 when unknown (e.g. directory copy).
type ProgressMsg struct {
	JobID       int64
	DoneBytes   int64
	TotalBytes  int64
	CurrentFile string
}

// DoneMsg is sent when a job reaches terminal state (success or error).
type DoneMsg struct {
	JobID int64
	Err   error
}

// JobQueue is the bounded fan-out pool that runs copy/move jobs concurrently.
// Exported methods are safe for concurrent use.
type JobQueue struct {
	mu      sync.Mutex
	nextID  int64
	jobs    map[int64]*Job
	maxW    int
	enqueue chan *Job
	prune   chan struct{}
	stop    chan struct{}
	stopped chan struct{}
}

const defaultMaxWorkers = 2
const maxConcurrentJobs = 10

// NewJobQueue returns a queue with maxWorkers goroutines draining inbound
// jobs. Call Run in a goroutine; stop it with Stop().
func NewJobQueue() *JobQueue {
	return &JobQueue{
		jobs:    make(map[int64]*Job),
		maxW:    defaultMaxWorkers,
		enqueue: make(chan *Job, maxConcurrentJobs),
		prune:   make(chan struct{}, 1),
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
}

// Run starts the worker pool and the background monitor that prunes done jobs.
// Blocks until Stop() is called.
func (q *JobQueue) Run() {
	defer close(q.stopped)
	workers := make(chan *Job, maxConcurrentJobs)
	for i := 0; i < q.maxW; i++ {
		go q.worker(workers)
	}
	for {
		select {
		case <-q.stop:
			close(workers)
			return
		case j := <-q.enqueue:
			workers <- j
		case <-q.prune:
			q.pruneDone()
		}
	}
}

// Stop signals the queue to finish in-flight jobs and exit. Blocks until all
// workers have returned (up to 30 s to avoid hanging the TUI).
func (q *JobQueue) Stop() {
	select {
	case <-q.stopped:
		return
	default:
	}
	close(q.stop)
	done := make(chan struct{})
	go func() {
		<-q.stopped
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
	}
}

// Enqueue adds a job to the queue and returns it immediately. The caller owns
// the returned *Job and can call Cancel() on it at any time. Returns an error
// if the queue is full (non-blocking — the TUI event loop is never blocked).
func (q *JobQueue) Enqueue(typ JobType, sources []string, dstDir string) (*Job, error) {
	id := atomic.AddInt64(&q.nextID, 1)
	ctx, cancel := context.WithCancel(context.Background())
	j := &Job{
		id:        id,
		typ:       typ,
		sources:   sources,
		dstDir:    dstDir,
		cancel:    cancel,
		done:      make(chan error, 1),
		startedAt: time.Now(),
	}
	q.mu.Lock()
	q.jobs[id] = j
	q.mu.Unlock()
	select {
	case q.enqueue <- j:
		return j, nil
	case <-ctx.Done():
		q.mu.Lock()
		delete(q.jobs, id)
		q.mu.Unlock()
		cancel()
		return nil, fmt.Errorf("已取消")
	default:
		cancel()
		q.mu.Lock()
		delete(q.jobs, id)
		q.mu.Unlock()
		j.err = fmt.Errorf("队列已满（上限 %d 个并发任务），请等待部分任务完成后再试", maxConcurrentJobs)
		j.endTime = time.Now()
		close(j.done)
		return j, j.err
	}
}

// Cancel asks the job to stop after the current file transfer finishes.
func (q *JobQueue) Cancel(jobID int64) {
	q.mu.Lock()
	j := q.jobs[jobID]
	q.mu.Unlock()
	if j == nil {
		return
	}
	atomic.StoreInt32(&j.cancelled, 1)
	j.cancel()
}

// CancelAll cancels every non-terminal job in the queue.
func (q *JobQueue) CancelAll() {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, j := range q.jobs {
		if atomic.LoadInt32(&j.doneFlag) == 0 {
			atomic.StoreInt32(&j.cancelled, 1)
			j.cancel()
		}
	}
}

// ActiveJobs returns a snapshot of non-terminal jobs. Safe for concurrent use.
func (q *JobQueue) ActiveJobs() []*Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]*Job, 0, len(q.jobs))
	for _, j := range q.jobs {
		if atomic.LoadInt32(&j.doneFlag) == 0 {
			out = append(out, j)
		}
	}
	return out
}

// AllJobs returns a snapshot of every tracked job (including terminal ones).
func (q *JobQueue) AllJobs() []*Job {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]*Job, 0, len(q.jobs))
	for _, j := range q.jobs {
		out = append(out, j)
	}
	return out
}

func (q *JobQueue) worker(ch <-chan *Job) {
	for j := range ch {
		q.runOne(j)
	}
}

func (q *JobQueue) runOne(j *Job) {
	var opErr error
	switch j.typ {
	case JobCopy:
		opErr = copyItemsProgress(j.sources, j.dstDir)
	case JobMove:
		opErr = moveItemsProgress(j.sources, j.dstDir)
	}
	// Check cancellation AFTER the op finishes (we can't interrupt an in-flight
	// io.Copy mid-stream without leaving a half-written file on disk).
	if atomic.LoadInt32(&j.cancelled) != 0 {
		opErr = fmt.Errorf("已取消")
	}
	q.markDone(j, opErr)
}

func (q *JobQueue) markDone(j *Job, err error) {
	select {
	case j.done <- err:
	default:
	}
	j.mu.Lock()
	if j.err == nil {
		j.err = err
	}
	j.endTime = time.Now()
	atomic.StoreInt32(&j.doneFlag, 1)
	j.mu.Unlock()
	select {
	case q.prune <- struct{}{}:
	default:
	}
}

func (q *JobQueue) pruneDone() {
	q.mu.Lock()
	defer q.mu.Unlock()
	for id, j := range q.jobs {
		if atomic.LoadInt32(&j.doneFlag) != 0 && time.Since(j.endTime) > 60*time.Second {
			delete(q.jobs, id)
		}
	}
}

func copyItemsProgress(srcs []string, dstDir string) error {
	for _, s := range srcs {
		dst := filepath.Join(dstDir, filepath.Base(s))
		if err := fs.CopyProgress(s, dst, nil); err != nil {
			return err
		}
	}
	return nil
}

func moveItemsProgress(srcs []string, dstDir string) error {
	for _, s := range srcs {
		dst := filepath.Join(dstDir, filepath.Base(s))
		if err := fs.MoveProgress(s, dst, nil); err != nil {
			return err
		}
	}
	return nil
}

// FormatElapsed renders elapsed time as MM:SS or HH:MM:SS.
func FormatElapsed(d time.Duration) string {
	d = d.Round(time.Second)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	if h > 0 {
		return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}

// FormatBytes renders a byte count as human-readable (KiB/MiB/GiB).
func FormatBytes(b int64) string {
	const (
		KiB = 1024
		MiB = KiB * 1024
		GiB = MiB * 1024
	)
	switch {
	case b >= GiB:
		return fmt.Sprintf("%.1f GiB", float64(b)/float64(GiB))
	case b >= MiB:
		return fmt.Sprintf("%.1f MiB", float64(b)/float64(MiB))
	case b >= KiB:
		return fmt.Sprintf("%.1f KiB", float64(b)/float64(KiB))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// jobSummary returns a short human-readable description of the job.
func jobSummary(j *Job) string {
	n := len(j.sources)
	src := ""
	if n > 0 {
		src = filepath.Base(j.sources[0])
		if n > 1 {
			src += fmt.Sprintf(" 等 %d 项", n)
		}
	}
	return fmt.Sprintf("[%s] %s → %s", j.typ, src, filepath.Base(j.dstDir))
}
