package cmd

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

// newEmptyResultRepo indexes a function nothing calls (Lonely) and a call to a
// function defined outside the repo (Talker calls fmt.Println).
func newEmptyResultRepo(t *testing.T) string {
	t.Helper()
	repo, db := t.TempDir(), filepath.Join(t.TempDir(), "index.db")
	// Registered after TempDir so it runs first: Windows cannot remove an open index.db.
	t.Cleanup(index.CloseAll)
	writeFile(t, repo, "a.go", "package p\n\nimport \"fmt\"\n\nfunc Lonely() int { return 1 }\n\nfunc Talker() { fmt.Println(\"hi\") }\n")
	if _, err := index.Index(repo, db, index.Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}
	return db
}

// nameCommand is one of the commands that answer for a symbol name, with the
// flags a case needs.
type nameCommand struct {
	name string
	run  func(*cobra.Command, []string) error
	make func(t *testing.T, db string) *cobra.Command
}

func withFlags(build func(string) *cobra.Command, flags ...string) func(*testing.T, string) *cobra.Command {
	return func(t *testing.T, db string) *cobra.Command {
		command := build(db)
		for i := 0; i < len(flags); i += 2 {
			setTestFlag(t, command, flags[i], flags[i+1])
		}
		return command
	}
}

// nameCommands returns impact, trace, refs (both modes), impls and investigate
// with flags (name, value pairs) set on each.
func nameCommands(flags ...string) []nameCommand {
	return []nameCommand{
		{"impact", impactCmd.RunE, withFlags(newImpactTestCommand, flags...)},
		{"trace", traceCmd.RunE, withFlags(newTraceTestCommand, flags...)},
		{"refs", refsCmd.RunE, withFlags(newRefsTestCommand, flags...)},
		{"refs --importers", refsCmd.RunE, withFlags(newRefsTestCommand, append([]string{"importers", "true"}, flags...)...)},
		{"impls", implsCmd.RunE, withFlags(newImplsTestCommand, flags...)},
		{"investigate", investigateCmd.RunE, withFlags(commandWithDB, flags...)},
	}
}

// graphCommands are the nameCommands that render a graph.
var graphCommands = map[string]bool{"impact": true, "trace": true, "impls": true}

// batchListsMissingNames reports whether a batch in this mode keeps an entry
// for a name that failed: investigate --json lists every name, and impls --json
// keys its map by the requested names.
func batchListsMissingNames(command, mode string) bool {
	return mode == "json" && (command == "investigate" || command == "impls")
}

var outputModes = map[string][]string{"text": nil, "json": {"json", "true"}}

// jsonResults decodes the {"version", "results"} envelope and returns results.
func jsonResults(t *testing.T, stdout string) any {
	t.Helper()
	var envelope struct {
		Version string `json:"version"`
		Results any    `json:"results"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil || envelope.Version == "" {
		t.Fatalf("not a JSON envelope (%v): %q", err, stdout)
	}
	return envelope.Results
}

func TestKnownNameWithNoResultsIsAnAnswer(t *testing.T) {
	db := newEmptyResultRepo(t)
	text := map[string][]string{
		"impact":           {"symbol: Lonely", "total_callers: 0"},
		"trace":            {"symbol: Lonely", "edges: 0"},
		"refs":             {"symbol: Lonely", "ref_count: 0"},
		"refs --importers": {"symbol: Lonely", "importer_count: 0"},
		"impls":            {"symbol: Lonely", "implementor_count: 0"},
		"investigate":      {"symbol: Lonely", "# Source"},
	}
	for _, c := range nameCommands() {
		t.Run(c.name, func(t *testing.T) {
			stdout, _, err := captureProcessOutput(t, func() error { return c.run(c.make(t, db), []string{"Lonely"}) })
			if err != nil {
				t.Fatalf("an empty answer must succeed, got %v", err)
			}
			for _, want := range text[c.name] {
				requireOutputContains(t, stdout, want)
			}
		})
	}

	for _, c := range nameCommands("json", "true") {
		t.Run(c.name+" --json", func(t *testing.T) {
			stdout, _, err := captureProcessOutput(t, func() error { return c.run(c.make(t, db), []string{"Lonely"}) })
			if err != nil {
				t.Fatalf("an empty answer must succeed, got %v", err)
			}
			results := jsonResults(t, stdout)
			switch c.name {
			case "impact", "trace":
				payload := results.(map[string]any)
				if rows, ok := payload["results"].([]any); !ok || len(rows) != 0 {
					t.Fatalf("results = %#v, want an empty list", payload["results"])
				}
				count := map[string]string{"impact": "total_callers", "trace": "edges"}[c.name]
				if payload[count] != float64(0) {
					t.Fatalf("%s = %v, want 0", count, payload[count])
				}
			case "refs", "refs --importers", "impls":
				if rows, ok := results.([]any); !ok || len(rows) != 0 {
					t.Fatalf("results = %#v, want an empty list", results)
				}
			case "investigate":
				requireOutputContains(t, stdout, `"result"`)
			}
		})
	}
}

func TestUnknownNameIsSymbolNotFound(t *testing.T) {
	db := newEmptyResultRepo(t)
	for mode, flags := range outputModes {
		for _, c := range nameCommands(flags...) {
			t.Run(c.name+" "+mode, func(t *testing.T) {
				stdout, _, err := captureProcessOutput(t, func() error { return c.run(c.make(t, db), []string{"Nope"}) })
				if !errors.Is(err, errSymbolNotFound) || err.Error() != "symbol not found: Nope" {
					t.Fatalf("err = %v, want symbol not found: Nope", err)
				}
				// investigate --json still lists the name, with its error.
				if c.name == "investigate" && mode == "json" {
					requireOutputContains(t, stdout, `"error": "symbol not found: Nope"`)
				} else if stdout != "" {
					t.Fatalf("an unknown name printed an answer: %q", stdout)
				}
			})
		}
	}

	for _, c := range nameCommands("graph-format", "json") {
		if !graphCommands[c.name] {
			continue
		}
		t.Run(c.name+" --graph", func(t *testing.T) {
			stdout, _, err := captureProcessOutput(t, func() error { return c.run(c.make(t, db), []string{"Nope"}) })
			if !errors.Is(err, errSymbolNotFound) || stdout != "" {
				t.Fatalf("err = %v, stdout %q; want symbol not found and no graph", err, stdout)
			}
		})
	}
}

func TestExternalNameIsKnown(t *testing.T) {
	db := newEmptyResultRepo(t)
	want := map[string][]string{
		"impact":           {"total_callers: 1", "func Talker()"},
		"trace":            {"symbol: Println", "edges: 0"},
		"refs":             {"ref_count: 1", "a.go:7"},
		"refs --importers": {"symbol: Println", "importer_count: 0"},
		"impls":            {"symbol: Println", "implementor_count: 0"},
	}
	for _, c := range nameCommands() {
		t.Run(c.name, func(t *testing.T) {
			stdout, _, err := captureProcessOutput(t, func() error { return c.run(c.make(t, db), []string{"Println"}) })
			if c.name == "investigate" {
				// investigate needs a definition, as show and context do.
				if !errors.Is(err, errSymbolNotFound) {
					t.Fatalf("err = %v, want symbol not found", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, w := range want[c.name] {
				requireOutputContains(t, stdout, w)
			}
		})
	}
}

func TestBatchWithMissingNamePrintsTheRest(t *testing.T) {
	db := newEmptyResultRepo(t)
	for mode, flags := range outputModes {
		for _, c := range nameCommands(flags...) {
			t.Run(c.name+" "+mode, func(t *testing.T) {
				stdout, _, err := captureProcessOutput(t, func() error { return c.run(c.make(t, db), []string{"Talker", "Nope"}) })
				if !errors.Is(err, errSymbolNotFound) || err.Error() != "symbol not found: Nope" {
					t.Fatalf("err = %v, want only symbol not found: Nope", err)
				}
				if mode == "json" {
					jsonResults(t, stdout)
				} else {
					requireOutputContains(t, stdout, "symbol: Talker")
				}
				if batchListsMissingNames(c.name, mode) {
					requireOutputContains(t, stdout, `"error": "symbol not found: Nope"`)
				} else if strings.Contains(stdout, "Nope") {
					t.Fatalf("the answer includes the missing name: %q", stdout)
				}
			})
		}
	}
}

// newImplsRepo indexes an interface that embeds one defined outside the repo
// (Source embeds io.Reader), an interface nothing implements (Quiet) and a type
// that implements nothing (Plain).
func newImplsRepo(t *testing.T) string {
	t.Helper()
	repo, db := t.TempDir(), filepath.Join(t.TempDir(), "index.db")
	// Registered after TempDir so it runs first: Windows cannot remove an open index.db.
	t.Cleanup(index.CloseAll)
	writeFile(t, repo, "a.go", "package p\n\nimport \"io\"\n\ntype Source interface {\n\tio.Reader\n}\n\ntype Quiet interface{ M() }\n\ntype Plain struct{}\n")
	if _, err := index.Index(repo, db, index.Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}
	return db
}

// runImpls runs impls with flags (name, value pairs) on the given names.
func runImpls(t *testing.T, db string, flags []string, names ...string) (string, error) {
	t.Helper()
	command := withFlags(newImplsTestCommand, flags...)(t, db)
	stdout, _, err := captureProcessOutput(t, func() error { return implsCmd.RunE(command, names) })
	return stdout, err
}

func TestImplsAnswersForKnownNames(t *testing.T) {
	db := newImplsRepo(t)
	for _, tc := range []struct {
		name  string
		flags []string
		names []string
		want  []string
	}{
		// An interface defined outside the repo is known through the edges that name it.
		{"external target", nil, []string{"Reader"}, []string{"implementor_count: 1", "Source", "(external)"}},
		{"nothing implements it", nil, []string{"Quiet"}, []string{"symbol: Quiet", "implementor_count: 0"}},
		// A filter narrows the answer; it doesn't make the name unknown.
		{"filtered away", []string{"lang", "java"}, []string{"Reader"}, []string{"symbol: Reader", "implementor_count: 0"}},
		{"--of", []string{"of", "Source"}, nil, []string{"symbol: Source", "edges: 1", "Reader"}},
		{"--of a type that implements nothing", []string{"of", "Plain"}, nil, []string{"symbol: Plain", "edges: 0"}},
		{"--of --json", []string{"of", "Plain", "json", "true"}, nil, []string{`"results": []`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, err := runImpls(t, db, tc.flags, tc.names...)
			if err != nil {
				t.Fatalf("a known name must succeed, got %v", err)
			}
			for _, want := range tc.want {
				requireOutputContains(t, stdout, want)
			}
		})
	}
}

func TestImplsUnknownNamesAreSymbolNotFound(t *testing.T) {
	db := newImplsRepo(t)
	for _, tc := range []struct {
		name, missing string
		flags, names  []string
	}{
		{"--of", "Nope", []string{"of", "Nope"}, nil},
		// impls matches bare names, as impact, trace and refs do: the index
		// records the embedded io.Reader as Reader.
		{"qualified name", "io.Reader", nil, []string{"io.Reader"}},
	} {
		for mode, flags := range map[string][]string{"text": nil, "json": {"json", "true"}, "graph": {"graph-format", "json"}} {
			t.Run(tc.name+" "+mode, func(t *testing.T) {
				stdout, err := runImpls(t, db, append(append([]string{}, tc.flags...), flags...), tc.names...)
				if !errors.Is(err, errSymbolNotFound) || err.Error() != "symbol not found: "+tc.missing || stdout != "" {
					t.Fatalf("err = %v, stdout %q; want symbol not found: %s and no answer", err, stdout, tc.missing)
				}
			})
		}
	}
}
