package main

import (
	"os"

	"github.com/1broseidon/cymbal/cmd"
	"github.com/1broseidon/cymbal/index"
)

func main() {
	// CloseAll flushes WAL and releases SQLite handles. Deferred here so it
	// fires on both success and error paths — PersistentPostRun is skipped
	// when RunE returns an error.
	defer index.CloseAll()
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
