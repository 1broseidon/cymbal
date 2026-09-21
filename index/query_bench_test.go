package index

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/1broseidon/cymbal/symbols"
)

func benchmarkStore(b *testing.B) *Store {
	b.Helper()
	s, err := OpenStore(filepath.Join(b.TempDir(), "index.db"))
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { s.Close() })
	return s
}

func BenchmarkImpactCallSites(b *testing.B) {
	for _, sites := range []int{20, 1000} {
		b.Run(fmt.Sprintf("sites=%d", sites), func(b *testing.B) {
			s := benchmarkStore(b)
			id, err := s.UpsertFile("/repo/callers.go", "callers.go", "go", "hash", time.Now(), 100)
			if err != nil {
				b.Fatal(err)
			}
			var defs []symbols.Symbol
			var refs []symbols.Ref
			for i := range 20 {
				start := i*100 + 1
				defs = append(defs, symbols.Symbol{Name: fmt.Sprintf("Caller%d", i), Kind: "function", StartLine: start, EndLine: start + 99, Language: "go"})
				for j := range sites / 20 {
					refs = append(refs, symbols.Ref{Name: "Target", Line: start + j + 1, Language: "go", Kind: symbols.RefKindCall})
				}
			}
			if err := s.InsertSymbols(id, defs); err != nil {
				b.Fatal(err)
			}
			if err := s.InsertRefs(id, refs); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				rows, err := s.FindImpact("Target", 1, 100)
				if err != nil || len(rows) != 20 {
					b.Fatalf("impact: %d callers, %v", len(rows), err)
				}
			}
		})
	}
}

func BenchmarkGraphUnrelatedSymbols(b *testing.B) {
	for _, unrelated := range []int{0, 10000} {
		b.Run(fmt.Sprintf("unrelated=%d", unrelated), func(b *testing.B) {
			s := benchmarkStore(b)
			id, err := s.UpsertFile("/repo/main.go", "main.go", "go", "hash", time.Now(), 100)
			if err != nil {
				b.Fatal(err)
			}
			defs := []symbols.Symbol{{Name: "Root", Kind: "function", StartLine: 1, EndLine: 2, Language: "go"}}
			for i := range unrelated {
				defs = append(defs, symbols.Symbol{Name: fmt.Sprintf("Unrelated%d", i), Kind: "function", StartLine: 10 + i*2, EndLine: 11 + i*2, Language: "go"})
			}
			if err := s.InsertSymbols(id, defs); err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				g, err := s.BuildGraph(GraphQuery{Symbol: "Root", Direction: GraphDirectionDown, Depth: 1})
				if err != nil || len(g.Nodes) != 1 {
					b.Fatalf("graph: %v, %v", g, err)
				}
			}
		})
	}
}
