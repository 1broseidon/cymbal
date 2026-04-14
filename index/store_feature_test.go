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

// ---------- Dead symbol detection tests ----------

func TestFeatureStoreDeadSymbolsBasic(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	// Insert a Go file with functions.
	fid, err := store.UpsertFile("/repo/main.go", "main.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "main", Kind: "function", File: "/repo/main.go", StartLine: 1, EndLine: 5, Language: "go"},
		{Name: "helperFunc", Kind: "function", File: "/repo/main.go", StartLine: 7, EndLine: 15, Language: "go"},
		{Name: "usedFunc", Kind: "function", File: "/repo/main.go", StartLine: 17, EndLine: 25, Language: "go"},
		{Name: "ExportedUnused", Kind: "function", File: "/repo/main.go", StartLine: 27, EndLine: 35, Language: "go"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Add a ref for usedFunc — it should NOT appear as dead.
	err = store.InsertRefs(fid, []symbols.Ref{
		{Name: "usedFunc", Line: 3, Language: "go"},
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := store.FindDeadSymbols(DeadSymbolQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	// Check that "main" (entry point) is excluded.
	for _, r := range results {
		if r.Name == "main" {
			t.Error("entry point 'main' should not be reported as dead")
		}
	}

	// Check that "usedFunc" (has refs) is excluded.
	for _, r := range results {
		if r.Name == "usedFunc" {
			t.Error("'usedFunc' has references and should not be reported as dead")
		}
	}

	// Check that "helperFunc" (unexported, no refs) IS reported.
	found := false
	for _, r := range results {
		if r.Name == "helperFunc" {
			found = true
			if r.Confidence != "high" {
				t.Errorf("expected 'high' confidence for unexported Go function, got %q", r.Confidence)
			}
			break
		}
	}
	if !found {
		t.Error("expected 'helperFunc' to be reported as dead")
	}

	// Check that "ExportedUnused" is reported with medium confidence.
	found = false
	for _, r := range results {
		if r.Name == "ExportedUnused" {
			found = true
			if r.Confidence != "medium" {
				t.Errorf("expected 'medium' confidence for exported Go function, got %q", r.Confidence)
			}
			break
		}
	}
	if !found {
		t.Error("expected 'ExportedUnused' to be reported as dead")
	}
}

func TestFeatureStoreDeadSymbolsEntryPointExclusions(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/main.go", "main.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "main", Kind: "function", File: "/repo/main.go", StartLine: 1, EndLine: 5, Language: "go"},
		{Name: "Main", Kind: "function", File: "/repo/main.go", StartLine: 7, EndLine: 10, Language: "go"},
		{Name: "init", Kind: "function", File: "/repo/main.go", StartLine: 12, EndLine: 15, Language: "go"},
		{Name: "Init", Kind: "function", File: "/repo/main.go", StartLine: 17, EndLine: 20, Language: "go"},
		{Name: "TestMain", Kind: "function", File: "/repo/main.go", StartLine: 22, EndLine: 25, Language: "go"},
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := store.FindDeadSymbols(DeadSymbolQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range results {
		switch r.Name {
		case "main", "init", "TestMain":
			t.Errorf("Go entry point %q should be excluded from dead code results", r.Name)
		}
	}

	// Main/Init are not Go entry points and should remain candidates.
	foundMain := false
	foundInitCap := false
	for _, r := range results {
		if r.Name == "Main" {
			foundMain = true
		}
		if r.Name == "Init" {
			foundInitCap = true
		}
	}
	if !foundMain {
		t.Error("expected Go function Main to remain eligible")
	}
	if !foundInitCap {
		t.Error("expected Go function Init to remain eligible")
	}
}

func TestFeatureStoreDeadSymbolsEntryPointLanguageAware(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	javaFile, err := store.UpsertFile("/repo/App.java", "App.java", "java", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSymbols(javaFile, []symbols.Symbol{
		{Name: "Main", Kind: "method", Parent: "App", File: "/repo/App.java", StartLine: 1, EndLine: 10, Language: "java"},
		{Name: "helper", Kind: "method", Parent: "App", File: "/repo/App.java", StartLine: 12, EndLine: 20, Language: "java"},
	}); err != nil {
		t.Fatal(err)
	}

	results, err := store.FindDeadSymbols(DeadSymbolQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range results {
		if r.Language == "java" && r.Name == "Main" {
			t.Fatal("expected Java Main entry method to be excluded")
		}
	}
}

func TestFeatureStoreDeadSymbolsTestFunctionExclusion(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/calc_test.go", "calc_test.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "TestAdd", Kind: "function", File: "/repo/calc_test.go", StartLine: 1, EndLine: 10, Language: "go"},
		{Name: "BenchmarkAdd", Kind: "function", File: "/repo/calc_test.go", StartLine: 12, EndLine: 20, Language: "go"},
		{Name: "helperInTest", Kind: "function", File: "/repo/calc_test.go", StartLine: 22, EndLine: 30, Language: "go"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Default: test functions excluded.
	results, err := store.FindDeadSymbols(DeadSymbolQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Name == "TestAdd" || r.Name == "BenchmarkAdd" {
			t.Errorf("test function %q should be excluded by default", r.Name)
		}
	}
	// helperInTest is in a _test.go file, so it should also be excluded.
	for _, r := range results {
		if r.Name == "helperInTest" {
			t.Errorf("'helperInTest' in test file should be excluded by default")
		}
	}

	// With IncludeTests: test functions included.
	results, err = store.FindDeadSymbols(DeadSymbolQuery{IncludeTests: true, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	foundTest := false
	for _, r := range results {
		if r.Name == "TestAdd" {
			foundTest = true
			break
		}
	}
	if !foundTest {
		t.Error("expected 'TestAdd' to be included when IncludeTests=true")
	}
}

func TestFeatureStoreDeadSymbolsPythonDunderExclusion(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/app.py", "app.py", "python", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "__init__", Kind: "method", File: "/repo/app.py", StartLine: 1, EndLine: 5, Language: "python"},
		{Name: "__str__", Kind: "method", File: "/repo/app.py", StartLine: 7, EndLine: 10, Language: "python"},
		{Name: "_private_func", Kind: "function", File: "/repo/app.py", StartLine: 12, EndLine: 20, Language: "python"},
		{Name: "public_func", Kind: "function", File: "/repo/app.py", StartLine: 22, EndLine: 30, Language: "python"},
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := store.FindDeadSymbols(DeadSymbolQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range results {
		if r.Name == "__init__" || r.Name == "__str__" {
			t.Errorf("Python dunder method %q should be excluded", r.Name)
		}
	}

	// _private_func should be reported with high confidence.
	found := false
	for _, r := range results {
		if r.Name == "_private_func" {
			found = true
			if r.Confidence != "high" {
				t.Errorf("expected 'high' confidence for private Python function, got %q", r.Confidence)
			}
		}
	}
	if !found {
		t.Error("expected '_private_func' to be reported as dead")
	}

	// public_func should be medium confidence.
	found = false
	for _, r := range results {
		if r.Name == "public_func" {
			found = true
			if r.Confidence != "medium" {
				t.Errorf("expected 'medium' confidence for public Python function, got %q", r.Confidence)
			}
		}
	}
	if !found {
		t.Error("expected 'public_func' to be reported as dead")
	}
}

func TestFeatureStoreDeadSymbolsMethodConfidence(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/handler.go", "handler.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "ServeHTTP", Kind: "method", File: "/repo/handler.go", StartLine: 1, EndLine: 10, Parent: "Handler", Language: "go"},
		{Name: "privateHelper", Kind: "method", File: "/repo/handler.go", StartLine: 12, EndLine: 20, Parent: "Handler", Language: "go"},
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := store.FindDeadSymbols(DeadSymbolQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range results {
		if r.Name == "ServeHTTP" && r.Confidence != "low" {
			t.Errorf("exported Go method should have 'low' confidence, got %q", r.Confidence)
		}
		if r.Name == "privateHelper" && r.Confidence != "medium" {
			t.Errorf("unexported Go method should have 'medium' confidence, got %q", r.Confidence)
		}
	}
}

func TestFeatureStoreDeadSymbolsKindFilter(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/main.go", "main.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "unusedFunc", Kind: "function", File: "/repo/main.go", StartLine: 1, EndLine: 10, Language: "go"},
		{Name: "unusedStruct", Kind: "struct", File: "/repo/main.go", StartLine: 12, EndLine: 20, Language: "go"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Filter by kind=function — should only return functions.
	results, err := store.FindDeadSymbols(DeadSymbolQuery{Kind: "function", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Kind != "function" {
			t.Errorf("expected only functions when kind filter is set, got %q", r.Kind)
		}
	}
	if len(results) == 0 {
		t.Error("expected at least one dead function")
	}
}

func TestFeatureStoreDeadSymbolsLanguageFilter(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid1, err := store.UpsertFile("/repo/main.go", "main.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid1, []symbols.Symbol{
		{Name: "goFunc", Kind: "function", File: "/repo/main.go", StartLine: 1, EndLine: 10, Language: "go"},
	})
	if err != nil {
		t.Fatal(err)
	}

	fid2, err := store.UpsertFile("/repo/app.py", "app.py", "python", "hash2", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid2, []symbols.Symbol{
		{Name: "py_func", Kind: "function", File: "/repo/app.py", StartLine: 1, EndLine: 10, Language: "python"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Filter by language=go — should only return Go symbols.
	results, err := store.FindDeadSymbols(DeadSymbolQuery{Language: "go", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Language != "go" {
			t.Errorf("expected only Go symbols when language filter is set, got %q", r.Language)
		}
	}
}

func TestFeatureStoreDeadSymbolsMinConfidence(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/main.go", "main.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid, []symbols.Symbol{
		// unexported function → high confidence
		{Name: "helperFunc", Kind: "function", File: "/repo/main.go", StartLine: 1, EndLine: 10, Language: "go"},
		// exported function → medium confidence
		{Name: "ExportedFunc", Kind: "function", File: "/repo/main.go", StartLine: 12, EndLine: 20, Language: "go"},
		// exported method → low confidence
		{Name: "ServeHTTP", Kind: "method", File: "/repo/main.go", StartLine: 22, EndLine: 30, Parent: "Handler", Language: "go"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// MinConfidence=high — should only return high confidence.
	results, err := store.FindDeadSymbols(DeadSymbolQuery{MinConfidence: "high", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Confidence != "high" {
			t.Errorf("with min-confidence=high, got result with confidence %q: %s", r.Confidence, r.Name)
		}
	}
	if len(results) != 1 || results[0].Name != "helperFunc" {
		t.Errorf("expected only 'helperFunc' with high confidence, got %d results", len(results))
	}

	// MinConfidence=medium — should return high and medium.
	results, err = store.FindDeadSymbols(DeadSymbolQuery{MinConfidence: "medium", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Confidence == "low" {
			t.Errorf("with min-confidence=medium, should not include low confidence: %s", r.Name)
		}
	}

	// MinConfidence=low (or empty) — should return all.
	results, err = store.FindDeadSymbols(DeadSymbolQuery{MinConfidence: "low", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results with min-confidence=low, got %d", len(results))
	}
}

func TestFeatureStoreDeadSymbolsLimit(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/main.go", "main.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	// Insert 10 unreferenced symbols.
	var syms []symbols.Symbol
	for i := range 10 {
		syms = append(syms, symbols.Symbol{
			Name:      fmt.Sprintf("func%d", i),
			Kind:      "function",
			File:      "/repo/main.go",
			StartLine: i*10 + 1,
			EndLine:   i*10 + 8,
			Language:  "go",
		})
	}
	err = store.InsertSymbols(fid, syms)
	if err != nil {
		t.Fatal(err)
	}

	// Limit to 3.
	results, err := store.FindDeadSymbols(DeadSymbolQuery{Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) > 3 {
		t.Errorf("expected at most 3 results, got %d", len(results))
	}
}

func TestFeatureStoreDeadSymbolsExcludesFieldsAndConstructors(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/model.go", "model.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "New", Kind: "constructor", File: "/repo/model.go", StartLine: 1, EndLine: 5, Language: "go"},
		{Name: "Name", Kind: "field", File: "/repo/model.go", StartLine: 7, EndLine: 7, Parent: "User", Language: "go"},
		{Name: "Active", Kind: "getter", File: "/repo/model.go", StartLine: 9, EndLine: 12, Language: "go"},
		{Name: "SetActive", Kind: "setter", File: "/repo/model.go", StartLine: 14, EndLine: 17, Language: "go"},
		{Name: "ROLE_ADMIN", Kind: "enum_member", File: "/repo/model.go", StartLine: 19, EndLine: 19, Language: "go"},
		{Name: "orphanFunc", Kind: "function", File: "/repo/model.go", StartLine: 21, EndLine: 30, Language: "go"},
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := store.FindDeadSymbols(DeadSymbolQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	excluded := map[string]bool{"New": true, "Name": true, "Active": true, "SetActive": true, "ROLE_ADMIN": true}
	for _, r := range results {
		if excluded[r.Name] {
			t.Errorf("kind %q symbol %q should be excluded from dead code results", r.Kind, r.Name)
		}
	}

	// orphanFunc should be reported.
	found := false
	for _, r := range results {
		if r.Name == "orphanFunc" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'orphanFunc' to be reported as dead")
	}
}

func TestFeatureStoreDeadSymbolsDartPrivate(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/lib/widget.dart", "lib/widget.dart", "dart", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "_PrivateWidget", Kind: "class", File: "/repo/lib/widget.dart", StartLine: 1, EndLine: 20, Language: "dart"},
		{Name: "PublicWidget", Kind: "class", File: "/repo/lib/widget.dart", StartLine: 22, EndLine: 40, Language: "dart"},
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := store.FindDeadSymbols(DeadSymbolQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range results {
		if r.Name == "_PrivateWidget" && r.Confidence != "high" {
			t.Errorf("expected 'high' confidence for private Dart symbol, got %q", r.Confidence)
		}
		if r.Name == "PublicWidget" && r.Confidence != "medium" {
			t.Errorf("expected 'medium' confidence for public Dart symbol, got %q", r.Confidence)
		}
	}
}

func TestFeatureStoreDeadSymbolsRubyLowConfidence(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/app.rb", "app.rb", "ruby", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "unused_method", Kind: "function", File: "/repo/app.rb", StartLine: 1, EndLine: 10, Language: "ruby"},
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := store.FindDeadSymbols(DeadSymbolQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Fatal("expected at least one result for Ruby")
	}
	if results[0].Confidence != "low" {
		t.Errorf("expected 'low' confidence for Ruby symbol due to metaprogramming, got %q", results[0].Confidence)
	}
}

func TestFeatureStoreDeadSymbolsJSTestFileExclusion(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	// Symbols in a test file should be excluded by default.
	fid, err := store.UpsertFile("/repo/src/utils.test.js", "src/utils.test.js", "javascript", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	err = store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "testHelper", Kind: "function", File: "/repo/src/utils.test.js", StartLine: 1, EndLine: 10, Language: "javascript"},
	})
	if err != nil {
		t.Fatal(err)
	}

	results, err := store.FindDeadSymbols(DeadSymbolQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range results {
		if r.Name == "testHelper" {
			t.Error("symbol in .test.js file should be excluded by default")
		}
	}
}

func TestFeatureStoreDeadSymbolsConstructorNameLanguageAware(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	goFile, err := store.UpsertFile("/repo/main.go", "main.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSymbols(goFile, []symbols.Symbol{
		{Name: "initialize", Kind: "function", File: "/repo/main.go", StartLine: 1, EndLine: 10, Language: "go"},
		{Name: "constructor", Kind: "function", File: "/repo/main.go", StartLine: 12, EndLine: 20, Language: "go"},
	}); err != nil {
		t.Fatal(err)
	}

	jsFile, err := store.UpsertFile("/repo/app.js", "app.js", "javascript", "hash2", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSymbols(jsFile, []symbols.Symbol{
		{Name: "constructor", Kind: "method", Parent: "Widget", File: "/repo/app.js", StartLine: 1, EndLine: 10, Language: "javascript"},
	}); err != nil {
		t.Fatal(err)
	}

	rubyFile, err := store.UpsertFile("/repo/app.rb", "app.rb", "ruby", "hash3", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSymbols(rubyFile, []symbols.Symbol{
		{Name: "initialize", Kind: "method", Parent: "Widget", File: "/repo/app.rb", StartLine: 1, EndLine: 10, Language: "ruby"},
	}); err != nil {
		t.Fatal(err)
	}

	results, err := store.FindDeadSymbols(DeadSymbolQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	foundGoInitialize := false
	foundGoConstructor := false
	for _, r := range results {
		switch {
		case r.Language == "go" && r.Name == "initialize":
			foundGoInitialize = true
		case r.Language == "go" && r.Name == "constructor":
			foundGoConstructor = true
		case r.Language == "javascript" && r.Name == "constructor":
			t.Error("JS method constructor should be excluded")
		case r.Language == "ruby" && r.Name == "initialize":
			t.Error("Ruby method initialize should be excluded")
		}
	}

	if !foundGoInitialize {
		t.Error("Go function named initialize should not be excluded")
	}
	if !foundGoConstructor {
		t.Error("Go function named constructor should not be excluded")
	}
}

func TestFeatureStoreDeadSymbolsParentNotAlwaysMethod(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/main.go", "main.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSymbols(fid, []symbols.Symbol{
		// Simulate a parser that emitted a nested function with parent metadata.
		{Name: "nestedHelper", Kind: "function", Parent: "outerFunc", File: "/repo/main.go", StartLine: 1, EndLine: 10, Language: "go"},
	}); err != nil {
		t.Fatal(err)
	}

	results, err := store.FindDeadSymbols(DeadSymbolQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	for _, r := range results {
		if r.Name == "nestedHelper" {
			if r.Confidence != "high" {
				t.Fatalf("expected nested Go function to remain function-classified (high), got %q", r.Confidence)
			}
			return
		}
	}
	t.Fatal("expected nestedHelper in dead symbols")
}

func TestFeatureStoreDeadSymbolsInvalidMinConfidence(t *testing.T) {
	store, _ := newTestStore(t)

	_, err := store.FindDeadSymbols(DeadSymbolQuery{MinConfidence: "bogus", Limit: 10})
	if err == nil {
		t.Fatal("expected error for invalid min confidence")
	}
}

func TestFeatureStoreDeadSymbolsDefaultExcludesVariablesAndConstants(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/main.go", "main.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "helper", Kind: "function", File: "/repo/main.go", StartLine: 1, EndLine: 10, Language: "go"},
		{Name: "tmpVar", Kind: "variable", Parent: "helper", File: "/repo/main.go", StartLine: 3, EndLine: 3, Language: "go"},
		{Name: "flag", Kind: "constant", File: "/repo/main.go", StartLine: 12, EndLine: 12, Language: "go"},
	}); err != nil {
		t.Fatal(err)
	}

	results, err := store.FindDeadSymbols(DeadSymbolQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	foundFunc := false
	for _, r := range results {
		if r.Kind == "variable" || r.Kind == "constant" {
			t.Fatalf("did not expect %s in default dead-symbol results", r.Kind)
		}
		if r.Name == "helper" {
			foundFunc = true
		}
	}
	if !foundFunc {
		t.Fatal("expected helper function in default dead-symbol results")
	}
}

func TestFeatureStoreDeadSymbolsVariableKindIsLowConfidence(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/main.go", "main.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "tmpVar", Kind: "variable", Parent: "helper", File: "/repo/main.go", StartLine: 3, EndLine: 3, Language: "go"},
	}); err != nil {
		t.Fatal(err)
	}

	results, err := store.FindDeadSymbols(DeadSymbolQuery{Kind: "variable", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly one variable result, got %d", len(results))
	}
	if results[0].Confidence != "low" {
		t.Fatalf("expected low confidence for variable kind, got %q", results[0].Confidence)
	}
}

func TestFeatureStoreDeadSymbolsRefMatchLanguageScoped(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	goFile, err := store.UpsertFile("/repo/main.go", "main.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSymbols(goFile, []symbols.Symbol{
		{Name: "sharedName", Kind: "function", File: "/repo/main.go", StartLine: 1, EndLine: 10, Language: "go"},
	}); err != nil {
		t.Fatal(err)
	}

	jsFile, err := store.UpsertFile("/repo/app.js", "app.js", "javascript", "hash2", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRefs(jsFile, []symbols.Ref{{Name: "sharedName", Line: 1, Language: "javascript"}}); err != nil {
		t.Fatal(err)
	}

	results, err := store.FindDeadSymbols(DeadSymbolQuery{Language: "go", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, r := range results {
		if r.Name == "sharedName" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected Go sharedName to remain dead; JS ref should not mark it alive")
	}
}

func TestFeatureStoreDeadSymbolsTypeLikeKindsDefaultIncludedAfterRefFixes(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/main.go", "main.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "UnusedType", Kind: "struct", File: "/repo/main.go", StartLine: 1, EndLine: 5, Language: "go"},
		{Name: "UsedType", Kind: "struct", File: "/repo/main.go", StartLine: 7, EndLine: 11, Language: "go"},
		{Name: "callUsedType", Kind: "function", File: "/repo/main.go", StartLine: 13, EndLine: 20, Language: "go"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRefs(fid, []symbols.Ref{
		{Name: "UsedType", Line: 14, Language: "go"},
		{Name: "callUsedType", Line: 18, Language: "go"},
	}); err != nil {
		t.Fatal(err)
	}

	results, err := store.FindDeadSymbols(DeadSymbolQuery{Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	foundUnusedType := false
	foundUsedType := false
	for _, r := range results {
		if r.Name == "UnusedType" {
			foundUnusedType = true
		}
		if r.Name == "UsedType" {
			foundUsedType = true
		}
	}
	if !foundUnusedType {
		t.Fatal("expected unreferenced type-like symbol to be included by default")
	}
	if foundUsedType {
		t.Fatal("expected referenced type-like symbol to be excluded")
	}
}

func TestFeatureStoreDeadSymbolsMinConfidenceNormalized(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/main.go", "main.go", "go", "hash1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "helperFunc", Kind: "function", File: "/repo/main.go", StartLine: 1, EndLine: 10, Language: "go"},
		{Name: "ExportedFunc", Kind: "function", File: "/repo/main.go", StartLine: 12, EndLine: 20, Language: "go"},
	}); err != nil {
		t.Fatal(err)
	}

	results, err := store.FindDeadSymbols(DeadSymbolQuery{MinConfidence: " HIGH ", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 || results[0].Name != "helperFunc" {
		t.Fatalf("expected only helperFunc with normalized HIGH filter, got %+v", results)
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
