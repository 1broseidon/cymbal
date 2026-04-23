package index

import (
	"fmt"
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

func TestStoreCloseCheckpointsWAL(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now()
	if _, err := store.UpsertFile("/repo/main.go", "main.go", "go", "hash", now, 100); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	if info, err := os.Stat(dbPath + "-wal"); err == nil && info.Size() != 0 {
		t.Fatalf("expected WAL to be checkpointed and truncated, size=%d", info.Size())
	}
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
	results, err := store.SearchSymbols("Handle", "", "", false, false, 50)
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

	results, err := store.SearchSymbols("HandleRequest", "", "", true, false, 50)
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
	results, err := store.SearchSymbols("main", "function", "", true, false, 50)
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range results {
		if r.Kind != "function" {
			t.Errorf("expected kind 'function', got %q for %s", r.Kind, r.Name)
		}
	}

	// Search for structs
	results, err = store.SearchSymbols("Config", "struct", "", true, false, 50)
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
	results, err := store.SearchSymbols("Config", "", "go", true, false, 50)
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
	results, err = store.SearchSymbols("Config", "", "python", true, false, 50)
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

func TestFeatureStoreExactIgnoreCase(t *testing.T) {
	store, _ := newTestStore(t)
	insertTestSymbols(t, store)

	// Case-insensitive exact match via the new ignoreCase flag.
	results, err := store.SearchSymbols("handlerequest", "", "", true, true, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected ignoreCase exact search to find HandleRequest")
	}
	if results[0].Name != "HandleRequest" {
		t.Errorf("expected HandleRequest, got %s", results[0].Name)
	}

	// Ensure case-sensitive exact does NOT find the lowercase variant.
	strict, err := store.SearchSymbols("handlerequest", "", "", true, false, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(strict) != 0 {
		t.Errorf("expected case-sensitive exact to return 0 results, got %d", len(strict))
	}
}

func TestFeatureStoreEmptyResults(t *testing.T) {
	store, _ := newTestStore(t)
	insertTestSymbols(t, store)

	// Search for something that doesn't exist
	results, err := store.SearchSymbols("NonExistentSymbolXYZ123", "", "", true, false, 50)
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

func TestFeatureStoreTextSearchRespectsGlobalLimit(t *testing.T) {
	defer CloseAll()

	store, dbPath := newTestStore(t)
	dir := t.TempDir()
	now := time.Now()

	for i := 0; i < 3; i++ {
		filePath := filepath.Join(dir, fmt.Sprintf("file%d.go", i))
		content := fmt.Sprintf("package main\nfunc Match%d() { println(\"needle\") }\n", i)
		if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := store.UpsertFile(filePath, filepath.Base(filePath), "go", fmt.Sprintf("hash%d", i), now, int64(len(content))); err != nil {
			t.Fatal(err)
		}
	}

	results, err := TextSearch(dbPath, "needle", "go", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected exactly 2 results, got %d", len(results))
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
