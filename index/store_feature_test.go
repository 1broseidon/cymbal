package index

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/1broseidon/cymbal/symbols"
)

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store, dbPath
}

func insertTestSymbols(t *testing.T, store *Store) {
	t.Helper()
	now := time.Now()

	// File 1: Go file with functions and a struct
	fid1, err := store.UpsertFile("/repo/main.go", "main.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid1, []symbols.Symbol{
		{Name: "main", Kind: "function", File: "/repo/main.go", StartLine: 1, EndLine: 5, Language: "go"},
		{Name: "HandleRequest", Kind: "function", File: "/repo/main.go", StartLine: 7, EndLine: 20, Language: "go"},
		{Name: "Server", Kind: "struct", File: "/repo/main.go", StartLine: 22, EndLine: 30, Language: "go"},
		{Name: "Config", Kind: "struct", File: "/repo/main.go", StartLine: 32, EndLine: 40, Language: "go"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// File 2: Python file with classes
	fid2, err := store.UpsertFile("/repo/app.py", "app.py", "python", "hash2", now, 200)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid2, []symbols.Symbol{
		{Name: "Application", Kind: "class", File: "/repo/app.py", StartLine: 1, EndLine: 50, Language: "python"},
		{Name: "handle_request", Kind: "function", File: "/repo/app.py", StartLine: 10, EndLine: 20, Language: "python"},
		{Name: "Config", Kind: "class", File: "/repo/app.py", StartLine: 52, EndLine: 70, Language: "python"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestFeatureStoreFTS5Search(t *testing.T) {
	store, _ := newTestStore(t)
	insertTestSymbols(t, store)

	// FTS5 prefix search for "Handle"
	results, err := store.SearchSymbols("Handle", "", "", false, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected FTS5 search to find symbols matching 'Handle'")
	}

	found := false
	for _, r := range results {
		if r.Name == "HandleRequest" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find HandleRequest via FTS5 prefix search")
	}
}

func TestFeatureStoreExactSearch(t *testing.T) {
	store, _ := newTestStore(t)
	insertTestSymbols(t, store)

	results, err := store.SearchSymbols("HandleRequest", "", "", true, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly 1 result for exact match, got %d", len(results))
	}
	if results[0].Name != "HandleRequest" {
		t.Errorf("expected HandleRequest, got %s", results[0].Name)
	}
}

func TestFeatureStoreKindFilter(t *testing.T) {
	store, _ := newTestStore(t)
	insertTestSymbols(t, store)

	// Search for all functions only
	results, err := store.SearchSymbols("main", "function", "", true, 50)
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range results {
		if r.Kind != "function" {
			t.Errorf("expected kind 'function', got %q for %s", r.Kind, r.Name)
		}
	}

	// Search for structs
	results, err = store.SearchSymbols("Config", "struct", "", true, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 struct named Config (Go), got %d", len(results))
	}
	if results[0].Kind != "struct" {
		t.Errorf("expected struct, got %s", results[0].Kind)
	}
}

func TestFeatureStoreLanguageFilter(t *testing.T) {
	store, _ := newTestStore(t)
	insertTestSymbols(t, store)

	// Search Config in Go only
	results, err := store.SearchSymbols("Config", "", "go", true, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 Go Config, got %d", len(results))
	}
	if results[0].Language != "go" {
		t.Errorf("expected language go, got %s", results[0].Language)
	}

	// Search Config in Python only
	results, err = store.SearchSymbols("Config", "", "python", true, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 Python Config, got %d", len(results))
	}
	if results[0].Language != "python" {
		t.Errorf("expected language python, got %s", results[0].Language)
	}
}

func TestFeatureStoreCaseInsensitiveSearch(t *testing.T) {
	store, _ := newTestStore(t)
	insertTestSymbols(t, store)

	// Search with different casing
	results, err := store.SearchSymbolsCI("handlerequest", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected case-insensitive search to find HandleRequest")
	}
	if results[0].Name != "HandleRequest" {
		t.Errorf("expected HandleRequest, got %s", results[0].Name)
	}

	// Also try uppercase
	results, err = store.SearchSymbolsCI("HANDLEREQUEST", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected case-insensitive search to find HandleRequest with UPPERCASE")
	}
}

func TestFeatureStoreEmptyResults(t *testing.T) {
	store, _ := newTestStore(t)
	insertTestSymbols(t, store)

	// Search for something that doesn't exist
	results, err := store.SearchSymbols("NonExistentSymbolXYZ123", "", "", true, 50)
	if err != nil {
		t.Fatal(err)
	}
	if results == nil {
		// nil is acceptable for "no rows" in Go, but we verify it doesn't error
		results = []SymbolResult{}
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

func TestFeatureStoreTextSearch(t *testing.T) {
	store, _ := newTestStore(t)

	// Create a real file with searchable content
	dir := t.TempDir()
	testFile := filepath.Join(dir, "search_test.go")
	content := `package main

// UniqueMarkerXYZ is a special function
func UniqueMarkerXYZ() {
	fmt.Println("hello world")
}
`
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	fid, err := store.UpsertFile(testFile, "search_test.go", "go", "hash_search", now, int64(len(content)))
	if err != nil {
		t.Fatal(err)
	}
	_ = fid

	// Use the store's AllFiles to verify it's indexed
	files, err := store.AllFiles("")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
}

func TestFeatureStoreFileSymbols(t *testing.T) {
	store, _ := newTestStore(t)
	insertTestSymbols(t, store)

	results, err := store.FileSymbols("/repo/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 symbols in main.go, got %d", len(results))
	}

	// Verify they're ordered by start_line
	for i := 1; i < len(results); i++ {
		if results[i].StartLine < results[i-1].StartLine {
			t.Error("expected symbols ordered by start_line")
		}
	}
}

func TestFeatureStoreDeleteStalePaths(t *testing.T) {
	store, _ := newTestStore(t)
	insertTestSymbols(t, store)

	// Pretend only main.go still exists
	current := map[string]struct{}{
		"/repo/main.go": {},
	}
	deleted, err := store.DeleteStalePaths(current)
	if err != nil {
		t.Fatal(err)
	}
	if deleted != 1 {
		t.Errorf("expected 1 stale file deleted, got %d", deleted)
	}

	// Verify app.py symbols are gone
	results, err := store.FileSymbols("/repo/app.py")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 symbols for deleted file, got %d", len(results))
	}
}

func TestFeatureStoreImportsAndRefs(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/main.go", "main.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}

	err = store.InsertImports(fid, []symbols.Import{
		{RawPath: "fmt", Language: "go"},
		{RawPath: "net/http", Language: "go"},
	})
	if err != nil {
		t.Fatal(err)
	}

	err = store.InsertRefs(fid, []symbols.Ref{
		{Name: "Println", Line: 10, Language: "go"},
		{Name: "ListenAndServe", Line: 15, Language: "go"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Verify imports
	imports, err := store.FileImports("/repo/main.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(imports) != 2 {
		t.Fatalf("expected 2 imports, got %d", len(imports))
	}

	// Verify refs
	refs, err := store.FindReferences("Println", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 {
		t.Fatalf("expected 1 reference to Println, got %d", len(refs))
	}
	if refs[0].Line != 10 {
		t.Errorf("expected ref on line 10, got %d", refs[0].Line)
	}
}

func TestFeatureStoreChildSymbolsFileScoped(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	// Two files with a type named "Tables" — simulates Java + Kotlin collision from issue #9.
	fid1, err := store.UpsertFile("/repo/Tables.java", "Tables.java", "java", "h1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid1, []symbols.Symbol{
		{Name: "Tables", Kind: "class", File: "/repo/Tables.java", StartLine: 1, EndLine: 20, Language: "java"},
		{Name: "USERS", Kind: "field", File: "/repo/Tables.java", StartLine: 3, EndLine: 3, Parent: "Tables", Language: "java"},
		{Name: "ORDERS", Kind: "field", File: "/repo/Tables.java", StartLine: 4, EndLine: 4, Parent: "Tables", Language: "java"},
	})
	if err != nil {
		t.Fatal(err)
	}

	fid2, err := store.UpsertFile("/repo/Tables.kt", "Tables.kt", "kotlin", "h2", now, 50)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid2, []symbols.Symbol{
		{Name: "Tables", Kind: "object", File: "/repo/Tables.kt", StartLine: 1, EndLine: 10, Language: "kotlin"},
		{Name: "users", Kind: "field", File: "/repo/Tables.kt", StartLine: 3, EndLine: 3, Parent: "Tables", Language: "kotlin"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Unscoped: returns members from both files.
	all, err := store.ChildSymbols("Tables", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("unscoped ChildSymbols: expected 3 members, got %d", len(all))
	}

	// Scoped to Java file: only Java members.
	java, err := store.ChildSymbols("Tables", 50, "/repo/Tables.java")
	if err != nil {
		t.Fatal(err)
	}
	if len(java) != 2 {
		t.Errorf("Java-scoped ChildSymbols: expected 2 members, got %d", len(java))
	}
	for _, m := range java {
		if m.File != "/repo/Tables.java" {
			t.Errorf("Java-scoped member %q came from %s", m.Name, m.File)
		}
	}

	// Scoped to Kotlin file: only Kotlin members.
	kt, err := store.ChildSymbols("Tables", 50, "/repo/Tables.kt")
	if err != nil {
		t.Fatal(err)
	}
	if len(kt) != 1 {
		t.Errorf("Kotlin-scoped ChildSymbols: expected 1 member, got %d", len(kt))
	}
	if len(kt) > 0 && kt[0].Name != "users" {
		t.Errorf("Kotlin-scoped member: expected 'users', got %q", kt[0].Name)
	}
}

// ---------------------------------------------------------------------------
// TestFeatureStoreDependsGraph
// ---------------------------------------------------------------------------

// insertDependsFixture sets up a small multi-file project:
//
//	main.go        imports config (go)
//	config/config.go  imports util (go)
//	util/util.go   (go, no indexed imports)
//	app.py         imports util (python)
//
// Expected edges:
//
//	main.go -> config/config.go
//	config/config.go -> util/util.go
//	app.py -> util/util.go
func insertDependsFixture(t *testing.T, store *Store) {
	t.Helper()
	now := time.Now()

	fmain, err := store.UpsertFile("/repo/main.go", "main.go", "go", "h1", now, 10)
	if err != nil {
		t.Fatal(err)
	}
	fconf, err := store.UpsertFile("/repo/config/config.go", "config/config.go", "go", "h2", now, 10)
	if err != nil {
		t.Fatal(err)
	}
	futil, err := store.UpsertFile("/repo/util/util.go", "util/util.go", "go", "h3", now, 10)
	if err != nil {
		t.Fatal(err)
	}
	fapp, err := store.UpsertFile("/repo/app.py", "app.py", "python", "h4", now, 10)
	if err != nil {
		t.Fatal(err)
	}

	// main.go imports "github.com/example/repo/config"
	if err := store.InsertImports(fmain, []symbols.Import{
		{RawPath: "github.com/example/repo/config", Language: "go"},
	}); err != nil {
		t.Fatal(err)
	}
	// config/config.go imports "github.com/example/repo/util"
	if err := store.InsertImports(fconf, []symbols.Import{
		{RawPath: "github.com/example/repo/util", Language: "go"},
	}); err != nil {
		t.Fatal(err)
	}
	// util/util.go has no imports
	_ = futil
	// app.py imports "from util import helper"
	if err := store.InsertImports(fapp, []symbols.Import{
		{RawPath: "from util import helper", Language: "python"},
	}); err != nil {
		t.Fatal(err)
	}
}

// edgeExists returns true if the given from->to edge is present in g.Edges.
func edgeExists(g *DependsGraph, from, to string) bool {
	for _, e := range g.Edges {
		if e.From == from && e.To == to {
			return true
		}
	}
	return false
}

// nodeExists returns true if a node with the given ID is present in g.Nodes.
func nodeExists(g *DependsGraph, id string) bool {
	for _, n := range g.Nodes {
		if n.ID == id {
			return true
		}
	}
	return false
}

func TestFeatureStoreDependsGraph_BasicEdges(t *testing.T) {
	store, _ := newTestStore(t)
	insertDependsFixture(t, store)

	g, err := store.BuildDependsGraph(DependsQuery{})
	if err != nil {
		t.Fatal(err)
	}

	// Should have at least 4 nodes.
	if len(g.Nodes) < 4 {
		t.Errorf("expected >= 4 nodes, got %d", len(g.Nodes))
	}

	// Expected edges.
	wantEdges := [][2]string{
		{"main.go", "config/config.go"},
		{"config/config.go", "util/util.go"},
		{"app.py", "util/util.go"},
	}
	for _, e := range wantEdges {
		if !edgeExists(g, e[0], e[1]) {
			t.Errorf("expected edge %s -> %s, not found in %v", e[0], e[1], g.Edges)
		}
	}
}

func TestFeatureStoreDependsGraph_NodeLanguage(t *testing.T) {
	store, _ := newTestStore(t)
	insertDependsFixture(t, store)

	g, err := store.BuildDependsGraph(DependsQuery{})
	if err != nil {
		t.Fatal(err)
	}

	for _, n := range g.Nodes {
		if n.Language == "" {
			t.Errorf("node %q has empty language", n.ID)
		}
	}
}

func TestFeatureStoreDependsGraph_ScopeFilter(t *testing.T) {
	store, _ := newTestStore(t)
	insertDependsFixture(t, store)

	// Scope to "config/" -- only edges where from starts with "config/"
	g, err := store.BuildDependsGraph(DependsQuery{Scope: "config/"})
	if err != nil {
		t.Fatal(err)
	}

	for _, e := range g.Edges {
		if !hasPrefix(e.From, "config/") {
			t.Errorf("scope filter violated: edge from=%q should have prefix 'config/'", e.From)
		}
	}

	if !edgeExists(g, "config/config.go", "util/util.go") {
		t.Error("expected edge config/config.go -> util/util.go under scope 'config/'")
	}
	if edgeExists(g, "main.go", "config/config.go") {
		t.Error("edge main.go -> config/config.go should not appear under scope 'config/'")
	}
}

func TestFeatureStoreDependsGraph_NoSelfLoops(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/self.go", "self.go", "go", "hs", now, 5)
	if err != nil {
		t.Fatal(err)
	}
	// Import whose key matches self.go
	if err := store.InsertImports(fid, []symbols.Import{
		{RawPath: "github.com/x/self", Language: "go"},
	}); err != nil {
		t.Fatal(err)
	}

	g, err := store.BuildDependsGraph(DependsQuery{})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range g.Edges {
		if e.From == e.To {
			t.Errorf("self-loop detected: %s -> %s", e.From, e.To)
		}
	}
}

func TestFeatureStoreDependsGraph_EmptyDB(t *testing.T) {
	store, _ := newTestStore(t)

	g, err := store.BuildDependsGraph(DependsQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(g.Nodes) != 0 || len(g.Edges) != 0 {
		t.Errorf("expected empty graph for empty DB, got %d nodes %d edges", len(g.Nodes), len(g.Edges))
	}
}

func TestFeatureStoreDependsGraph_CycleDetection(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fa, err := store.UpsertFile("/repo/a.go", "a.go", "go", "ha", now, 5)
	if err != nil {
		t.Fatal(err)
	}
	fb, err := store.UpsertFile("/repo/b.go", "b.go", "go", "hb", now, 5)
	if err != nil {
		t.Fatal(err)
	}

	// a.go imports b; b.go imports a -> cycle
	if err := store.InsertImports(fa, []symbols.Import{{RawPath: "github.com/x/b", Language: "go"}}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertImports(fb, []symbols.Import{{RawPath: "github.com/x/a", Language: "go"}}); err != nil {
		t.Fatal(err)
	}

	g, err := store.BuildDependsGraph(DependsQuery{})
	if err != nil {
		t.Fatal(err)
	}

	if len(g.Cycles) == 0 {
		t.Error("expected at least one cycle to be detected between a.go and b.go")
	}
}

func TestFeatureStoreDependsGraph_SortedOutput(t *testing.T) {
	store, _ := newTestStore(t)
	insertDependsFixture(t, store)

	g, err := store.BuildDependsGraph(DependsQuery{})
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i < len(g.Nodes); i++ {
		if g.Nodes[i].ID < g.Nodes[i-1].ID {
			t.Errorf("nodes not sorted: %q before %q", g.Nodes[i-1].ID, g.Nodes[i].ID)
		}
	}
	for i := 1; i < len(g.Edges); i++ {
		prev := g.Edges[i-1]
		cur := g.Edges[i]
		if cur.From < prev.From || (cur.From == prev.From && cur.To < prev.To) {
			t.Errorf("edges not sorted: (%s->%s) before (%s->%s)", prev.From, prev.To, cur.From, cur.To)
		}
	}
}

// hasPrefix is a helper to keep test code readable.
func hasPrefix(s, prefix string) bool {
	return len(s) >= len(prefix) && s[:len(prefix)] == prefix
}
