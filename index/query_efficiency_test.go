package index

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/1broseidon/cymbal/symbols"
)

func TestImpactBatchedEnclosuresPreserveTraversal(t *testing.T) {
	s, _ := newTestStore(t)
	add := func(path, language string, defs []symbols.Symbol, refs []symbols.Ref) int64 {
		t.Helper()
		id, err := s.UpsertFile("/repo/"+path, path, language, "hash", time.Now(), 100)
		if err != nil {
			t.Fatal(err)
		}
		for i := range defs {
			defs[i].Language = language
		}
		for i := range refs {
			refs[i].Language = language
			refs[i].Kind = symbols.RefKindCall
		}
		if err := s.InsertSymbols(id, defs); err != nil {
			t.Fatal(err)
		}
		if err := s.InsertRefs(id, refs); err != nil {
			t.Fatal(err)
		}
		return id
	}
	id := add("main.go", "go", []symbols.Symbol{
		{Name: "Target", StartLine: 1, EndLine: 4},
		{Name: "Outer", StartLine: 10, EndLine: 100},
		{Name: "Inner", StartLine: 20, EndLine: 30},
		{Name: "First", StartLine: 40, EndLine: 45},
		{Name: "Tied", StartLine: 40, EndLine: 45},
	}, []symbols.Ref{
		{Name: "Target", Line: 2}, {Name: "Target", Line: 15},
		{Name: "Target", Line: 21}, {Name: "Target", Line: 22},
		{Name: "Target", Line: 42}, {Name: "Target", Line: 101},
	})
	add("bridge_test.go", "go", []symbols.Symbol{{Name: "TestBridge", StartLine: 1, EndLine: 10}}, []symbols.Ref{{Name: "Target", Line: 3}})
	add("caller.go", "go", []symbols.Symbol{{Name: "RealCaller", StartLine: 1, EndLine: 10}}, []symbols.Ref{{Name: "TestBridge", Line: 3}})
	add("caller.py", "python", []symbols.Symbol{{Name: "PyCaller", StartLine: 1, EndLine: 10}}, []symbols.Ref{{Name: "Target", Line: 3}})
	intervals, err := s.enclosingSymbols(id)
	if err != nil {
		t.Fatal(err)
	}
	for line := 1; line <= 102; line++ {
		want, _ := s.EnclosingSymbol("/repo/main.go", line)
		if got := intervals.at(line); got != want {
			t.Fatalf("enclosure at %d = %q, want %q", line, got, want)
		}
	}
	rows, truncated, err := s.findImpactInLangs("Target", []string{"go"}, 2, 4, true, NewClassifier(nil))
	if err != nil || truncated {
		t.Fatalf("impact: truncated=%v, err=%v", truncated, err)
	}
	want := map[string]int{"Outer": 15, "Inner": 21, "First": 42, "RealCaller": 3}
	got := map[string]int{}
	for _, row := range rows {
		got[row.Caller] = row.Line
		if row.Caller == "RealCaller" && (row.Depth != 2 || row.Symbol != "TestBridge") {
			t.Fatalf("lost hidden test traversal: %+v", row)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("callers = %v, want %v", got, want)
	}
	if _, err := s.db.Exec("UPDATE symbols SET name = 'Renamed' WHERE name = 'Inner'"); err != nil {
		t.Fatal(err)
	}
	rows, err = s.FindImpactScoped("Target", "go", 1, 100)
	if err != nil || !impactContainsCaller(rows, "Renamed") || impactContainsCaller(rows, "Inner") {
		t.Fatalf("later traversal reused stale enclosures: %+v, %v", rows, err)
	}
}

func TestGraphMetadataBatchesKeepAllRelevantDefinitions(t *testing.T) {
	s, _ := newTestStore(t)
	id, err := s.UpsertFile("/repo/a.go", "a.go", "go", "hash", time.Now(), 100)
	if err != nil {
		t.Fatal(err)
	}
	defs := []symbols.Symbol{{Name: "Unrelated", StartLine: 1, EndLine: 2, Language: "go"}}
	var trace []TraceResult
	for i := range 1100 {
		name := fmt.Sprintf("Callee%04d", i)
		defs = append(defs, symbols.Symbol{Name: name, StartLine: 10 + i, EndLine: 10 + i, Language: "go"})
		trace = append(trace, TraceResult{Caller: "Root", Callee: name})
	}
	// Exact duplicates collapse, while language, position and nested definitions
	// remain distinct. Depth ordering must survive the name-restricted query.
	defs = append(defs,
		symbols.Symbol{Name: "Root", StartLine: 3, EndLine: 4, Language: "go"},
		symbols.Symbol{Name: "Root", StartLine: 3, EndLine: 4, Language: "go"},
		symbols.Symbol{Name: "Root", StartLine: 2, EndLine: 4, Language: "go", Depth: 1},
		symbols.Symbol{Name: "Root", StartLine: 3, EndLine: 4, Language: "python"},
	)
	if err := s.InsertSymbols(id, defs); err != nil {
		t.Fatal(err)
	}
	metas, err := s.symbolMetas(graphSymbolNames("Root", trace, []ImpactResult{{Caller: "CallerOnly", Symbol: "Root"}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(metas) != 1101 || len(metas["Unrelated"]) != 0 || len(metas["Callee1099"]) != 1 {
		t.Fatalf("metadata did not cover exactly the traversed names: %d names", len(metas))
	}
	want := []graphSymbolMeta{{path: "a.go", language: "go", startLine: 3}, {path: "a.go", language: "python", startLine: 3}, {path: "a.go", language: "go", startLine: 2}}
	if !reflect.DeepEqual(metas["Root"], want) {
		t.Fatalf("root definitions = %#v, want %#v", metas["Root"], want)
	}
}
