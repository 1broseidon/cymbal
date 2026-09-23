package index

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// bigBody is a declaration header, n numbered lines from line, and a closing brace.
func bigBody(header, line string, n int) string {
	var b strings.Builder
	b.WriteString(header + "\n")
	for i := range n {
		fmt.Fprintf(&b, line+"\n", i)
	}
	b.WriteString("}\n")
	return b.String()
}

func TestInvestigateResolvedCapsEveryTypeKind(t *testing.T) {
	repo, db := t.TempDir(), filepath.Join(t.TempDir(), "index.db")
	// Registered after TempDir so it runs first: Windows cannot remove an open index.db.
	t.Cleanup(CloseAll)
	files := map[string]string{
		"big.go": bigBody("type BigStruct struct {", "\tF%d int", 70) +
			bigBody("func BigFunc() {", "\t_ = %d", 70),
		"Big.swift": bigBody("protocol BigProtocol {", "    func m%d()", 70) +
			bigBody("actor BigActor {", "    func a%d() {}", 70),
		"Big.cs": bigBody("record BigRecord {", "    public int P%d { get; init; }", 70),
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Index(repo, db, Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}

	// Each declaration spans 72 lines; a capped type keeps 60 of them.
	for name, kind := range map[string]string{
		"BigStruct": "struct", "BigProtocol": "protocol", "BigActor": "actor",
		"BigRecord": "record", "BigFunc": "function",
	} {
		t.Run(name, func(t *testing.T) {
			full, err := Investigate(db, name)
			if err != nil {
				t.Fatal(err)
			}
			if full.Symbol.Kind != kind || strings.Contains(full.Source, "more lines") {
				t.Fatalf("Investigate %s: kind %q, want %q, with the full source", name, full.Symbol.Kind, kind)
			}
			resolved, err := InvestigateResolved(db, full.Symbol)
			if err != nil {
				t.Fatal(err)
			}
			capped := strings.Contains(resolved.Source, "... (12 more lines")
			if wantCap := kind != "function"; capped != wantCap {
				t.Fatalf("InvestigateResolved %s capped = %v, want %v:\n%s", name, capped, wantCap, resolved.Source)
			}
		})
	}
}
