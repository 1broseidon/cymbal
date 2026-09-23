package main

import (
	"os"

	"github.com/1broseidon/cymbal/cmd"
	"github.com/1broseidon/cymbal/index"
)

func main() {
	// CloseAll flushes WAL and releases SQLite handles. It runs before exiting
	// on both paths: PersistentPostRun is skipped when RunE returns an error,
	// and os.Exit skips deferred calls.
	err := cmd.Execute()
	index.CloseAll()
	if err != nil {
		os.Exit(1)
	}
}
