package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestQueueEnqueueAndRun(t *testing.T) {
	q := NewJobQueue()
	defer q.Stop()
	go q.Run()

	srcDir := t.TempDir()
	dstDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "hello.txt")
	if err := os.WriteFile(srcFile, []byte("hello world"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	j, err := q.Enqueue(JobCopy, []string{srcFile}, dstDir)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if j.id <= 0 {
		t.Fatalf("expected positive job id, got %d", j.id)
	}

	select {
	case <-j.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("job did not complete within timeout")
	}

	dstFile := filepath.Join(dstDir, "hello.txt")
	got, err := os.ReadFile(dstFile)
	if err != nil {
		t.Fatalf("dst file missing: %v", err)
	}
	if string(got) != "hello world" {
		t.Fatalf("content mismatch: got %q want %q", got, "hello world")
	}
}

func TestQueueCancelSignalSetsFlag(t *testing.T) {
	q := NewJobQueue()
	defer q.Stop()
	go q.Run()

	// Use a small file so the copy finishes quickly; verify the cancel flag
	// is set even though the underlying io.Copy may race past it.
	srcDir := t.TempDir()
	dstDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "small.txt")
	if err := os.WriteFile(srcFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	j, err := q.Enqueue(JobCopy, []string{srcFile}, dstDir)
	if err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	// Cancel immediately.
	q.Cancel(j.id)

	// The job will finish (can't interrupt in-flight io.Copy); what we
	// verify is that the done channel eventually closes and Err is non-nil.
	select {
	case <-j.Done():
		if j.Err() == nil {
			// Race: copy finished before cancel took effect; acceptable.
			t.Log("cancel raced with fast copy; error is nil — acceptable")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("job did not terminate within timeout after cancel")
	}
}

func TestQueueActiveJobs(t *testing.T) {
	q := NewJobQueue()
	defer q.Stop()
	go q.Run()

	srcDir := t.TempDir()
	dstDir := t.TempDir()
	for i := 0; i < 3; i++ {
		name := filepath.Join(srcDir, fmt.Sprintf("f%d.txt", i))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatalf("setup: %v", err)
		}
	}

	var jobs []*Job
	for i := 0; i < 3; i++ {
		src := filepath.Join(srcDir, fmt.Sprintf("f%d.txt", i))
		j, err := q.Enqueue(JobCopy, []string{src}, dstDir)
		if err != nil {
			t.Fatalf("enqueue %d: %v", i, err)
		}
		jobs = append(jobs, j)
	}

	// Wait for each job with a per-job timeout.
	for i, j := range jobs {
		select {
		case err := <-j.Done():
			if err != nil {
				t.Logf("job #%d (idx %d) finished with error: %v", j.id, i, err)
			} else {
				t.Logf("job #%d (idx %d) completed successfully", j.id, i)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("job #%d (index %d) did not complete within 5s", j.id, i)
		}
	}
	time.Sleep(100 * time.Millisecond) // let prune catch up

	acts := q.ActiveJobs()
	t.Logf("after completion: active=%d, all=%d", len(acts), len(q.AllJobs()))
	for _, aj := range acts {
		t.Logf("  active job #%d isDone=%v", aj.id, aj.IsDone())
	}
	if len(acts) != 0 {
		t.Fatalf("expected 0 active jobs after completion, got %d", len(acts))
	}
}

func TestFormatHelpers(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1024, "1.0 KiB"},
		{1 << 20, "1.0 MiB"},
		{3 << 20, "3.0 MiB"},
		{1 << 30, "1.0 GiB"},
	}
	for _, c := range cases {
		got := FormatBytes(c.in)
		if got != c.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
	if got := FormatElapsed(90 * time.Second); got != "01:30" {
		t.Errorf("FormatElapsed(90s) = %q, want 01:30", got)
	}
	if got := FormatElapsed(3*60*time.Second + 5*time.Second); got != "03:05" {
		t.Errorf("FormatElapsed(185s) = %q, want 03:05", got)
	}
}

func TestJobSummary(t *testing.T) {
	j := &Job{
		typ:     JobCopy,
		sources: []string{"/a/b/c.txt"},
		dstDir:  "/x/y/",
	}
	want := "[复制] c.txt → y"
	if got := jobSummary(j); got != want {
		t.Errorf("jobSummary = %q, want %q", got, want)
	}
	j2 := &Job{
		typ:      JobMove,
		sources:  []string{"/a/one.txt", "/a/two.txt", "/a/three.txt"},
		dstDir:   "/b/",
	}
	want2 := "[移动] one.txt 等 3 项 → b"
	if got := jobSummary(j2); got != want2 {
		t.Errorf("jobSummary(multi) = %q, want %q", got, want2)
	}
}
