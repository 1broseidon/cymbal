package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/1broseidon/cymbal/index"
)

func TestSourceSnippetsPreserveContextAndOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.go")
	if err := os.WriteFile(path, []byte("first  \r\n\tTarget() \t\r\n\r\n  Target()\r\nlast\t"), 0o600); err != nil {
		t.Fatal(err)
	}
	locations := []sourceLocation{{path, 4}, {path, 1}, {path, 2}, {path, 5}, {path, 4}, {path, 7}, {path + ".missing", 2}}
	want := []sourceSnippet{
		{Context: []string{"", "  Target()", "last"}, startLine: 3},
		{Context: []string{"first", "\tTarget()"}, startLine: 1},
		{Context: []string{"first", "\tTarget()", ""}, startLine: 1},
		{Context: []string{"  Target()", "last"}, startLine: 4},
		{Context: []string{"", "  Target()", "last"}, startLine: 3},
		{Context: []string{""}, startLine: 7},
		{Context: []string{""}, startLine: 2},
	}
	got := readSourceSnippets(locations, 1)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snippets = %#v, want %#v", got, want)
	}
	if text := got[0].refLine("calls.go", 4).text; text != "Target()" {
		t.Fatalf("call line = %q", text)
	}
	for _, ctx := range []int{0, -1} {
		snippet := readSourceSnippets([]sourceLocation{{path, 2}}, ctx)[0]
		if !reflect.DeepEqual(snippet.Context, []string{"\tTarget()"}) || snippet.startLine != 2 {
			t.Fatalf("context %d: %#v", ctx, snippet)
		}
	}
}

func TestEnrichedRefsShareTextAndJSONContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "calls.go")
	if err := os.WriteFile(path, []byte("before\n\tTarget()\nafter\n\tTarget()\nend\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	refs := []index.RefResult{{File: path, RelPath: "calls.go", Line: 2}, {File: path, RelPath: "calls.go", Line: 4}}
	enriched := enrichRefs(refs, 1)
	lines, groups := dedupRefLines([]refLine{
		enriched[0].sourceSnippet.refLine("calls.go", 2),
		enriched[1].sourceSnippet.refLine("calls.go", 4),
	})
	want := []string{"calls.go (2 sites):", "    before", "  > \tTarget()", "    after"}
	if groups != 1 || !reflect.DeepEqual(lines, want) {
		t.Fatalf("grouped output = %v (%d groups), want %v", lines, groups, want)
	}
	data, err := json.Marshal(enriched)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	plain, _ := json.Marshal(refs)
	var original []map[string]any
	if err := json.Unmarshal(plain, &original); err != nil {
		t.Fatal(err)
	}
	for i, row := range decoded {
		context, ok := row["context"].([]any)
		if !ok || len(context) != 3 || context[1] != "\tTarget()" {
			t.Fatalf("JSON context = %v", row["context"])
		}
		delete(row, "context")
		if !reflect.DeepEqual(row, original[i]) {
			t.Fatalf("JSON fields changed: %v, want %v", row, original[i])
		}
	}
	if err := os.WriteFile(path, []byte("before\n\tChanged()\nafter\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := enrichRefs(refs[:1], 1)[0].Context[1]; !strings.Contains(got, "Changed()") {
		t.Fatalf("a later operation used stale source: %q", got)
	}
}
