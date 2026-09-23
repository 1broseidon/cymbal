//go:build !windows

package walker

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// Opening a FIFO blocks until a writer appears, so the walk must not sniff one.
func TestWalkerSkipsFIFO(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0644); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tool"), []byte("#!/bin/sh\n"), 0644); err != nil {
		t.Fatal(err)
	}

	done := make(chan []FileEntry, 1)
	go func() {
		files, _ := Walk(dir, 2, nil)
		done <- files
	}()
	select {
	case files := <-done:
		if len(files) != 1 || files[0].RelPath != "tool" {
			t.Errorf("files = %+v, want only tool", files)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("walk blocked on a FIFO")
	}
}
