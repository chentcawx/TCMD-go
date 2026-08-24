//go:build windows

package fs_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"tcmd/internal/fs"
)

// TestJunctionIsDir verifies that junctions are correctly identified as
// directories by fs.ListDir. This catches regressions where a junction's
// mode bits don't include os.ModeDir, causing the UI to treat it as a file.
//
// In short mode we skip the mklink invocation to avoid spawning a visible
// Command Prompt window during automated test runs.
func TestJunctionIsDir(t *testing.T) {
	if testing.Short() {
		t.Skip("skips mklink in -short mode")
	}
	root := t.TempDir()
	real := filepath.Join(root, "real")
	link := filepath.Join(root, "link")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Create junction via mklink /J.
	if err := createJunction(link, real); err != nil {
		t.Fatalf("mklink /J failed: %v", err)
	}

	entries, err := fs.ListDir(root)
	if err != nil {
		t.Fatalf("ListDir failed: %v", err)
	}
	// Find the junction entry.
	var linkEntry *fs.Entry
	for i := range entries {
		if entries[i].Name == "link" {
			linkEntry = &entries[i]
			break
		}
	}
	if linkEntry == nil {
		t.Fatalf("junction 'link' not found in directory listing")
	}
	if !linkEntry.IsDir {
		t.Errorf("junction should be identified as IsDir=true, got false")
	}
}

// createJunction runs mklink /J and returns any error.
func createJunction(link, target string) error {
	out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput()
	if err != nil {
		return fmt.Errorf("mklink /J %q -> %q: %v (output: %s)", link, target, err, string(out))
	}
	return nil
}
