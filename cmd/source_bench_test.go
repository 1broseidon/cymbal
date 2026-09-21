package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/cymbal/index"
)

func BenchmarkEnrichRefs(b *testing.B) {
	path := filepath.Join(b.TempDir(), "callers.go")
	if err := os.WriteFile(path, []byte(strings.Repeat("\tTarget()\n", 2000)), 0o600); err != nil {
		b.Fatal(err)
	}
	refs := make([]index.RefResult, 20)
	for i := range refs {
		refs[i] = index.RefResult{File: path, RelPath: "callers.go", Line: 100 + i*90}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		rows := enrichRefs(refs, 1)
		if len(rows) != 20 || len(rows[0].Context) != 3 {
			b.Fatal("missing reference context")
		}
	}
}
