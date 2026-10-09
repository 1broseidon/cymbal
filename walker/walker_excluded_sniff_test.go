package walker

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// TestWalkerCountsLargeSniffedFilesExcluded guards the regression where a
// too-large extensionless script -- an oversized file whose language is sniffed
// from its `#!` line -- was dropped by a worker without being recorded in
// WalkStats, so FilesExcluded and BytesExcluded undercounted the files and
// bytes the walk left behind. Both an oversized file with an extension
// (dropped while walking) and an oversized extensionless script (dropped while
// sniffing) must be counted, and BytesExcluded must equal the sum of both
// files' sizes.
func TestWalkerCountsLargeSniffedFilesExcluded(t *testing.T) {
	dir := t.TempDir()

	// Both oversized files are just over defaultMaxSourceFileBytes so they are
	// skipped by default; the small file must still be walked.
	payload := bytes.Repeat([]byte("x"), int(defaultMaxSourceFileBytes)+1)
	oversized := []struct {
		name    string
		content []byte
		mode    os.FileMode
	}{
		{name: "big.go", content: append([]byte("package p\n"), payload...), mode: 0o644},
		{name: "bigscript", content: append([]byte("#!/bin/sh\n"), payload...), mode: 0o755},
	}

	for _, f := range oversized {
		if err := os.WriteFile(filepath.Join(dir, f.name), f.content, f.mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "small.go"), []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	files, stats, err := WalkWithOptions(dir, 2, nil, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 1 || files[0].RelPath != "small.go" {
		t.Fatalf("expected only small.go to be walked, got %+v", files)
	}

	var wantBytes int64
	for _, f := range oversized {
		wantBytes += int64(len(f.content))
	}
	if stats.FilesExcluded != len(oversized) {
		t.Errorf("FilesExcluded: want %d, got %d", len(oversized), stats.FilesExcluded)
	}
	if stats.BytesExcluded != wantBytes {
		t.Errorf("BytesExcluded: want %d, got %d", wantBytes, stats.BytesExcluded)
	}
}
