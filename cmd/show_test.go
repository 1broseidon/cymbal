package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/cymbal/index"
)

func TestIsFilePathWithSymbolSuffix(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// File paths (no colon or numeric suffix)
		{"internal/store.go", true},
		{"store.go", true},
		{"store.go:80-120", true},
		{"store.go:L80-L120", true},
		// File:Symbol — should NOT be a file path (routes to symbol lookup)
		{"store.go:SearchSymbols", false},
		// Symbols starting with L must not be mistaken for line ranges
		{"store.go:Load", false},
		{"internal/store.go:ListFiles", false},
		{"store.go:L", false},
		{"internal/store.go:SearchSymbols", false},
		{"parser.ts:extractRefs", false},
		// Pure symbol names
		{"SearchSymbols", false},
		{"Store.SearchSymbols", false},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := isFilePath(tt.input)
			if got != tt.want {
				t.Errorf("isFilePath(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseSymbolArgFileScoped(t *testing.T) {
	tests := []struct {
		input    string
		wantFile string
		wantSym  string
	}{
		{"store.go:SearchSymbols", "store.go", "SearchSymbols"},
		{"internal/index/store.go:SearchSymbols", "internal/index/store.go", "SearchSymbols"},
		{"parser.ts:extractRefs", "parser.ts", "extractRefs"},
		// Numeric suffix — not a symbol
		{"store.go:80", "", "store.go:80"},
		{"store.go:L80", "", "store.go:L80"},
		// No colon — plain symbol
		{"SearchSymbols", "", "SearchSymbols"},
		// Dot-qualified — not file-scoped
		{"Store.SearchSymbols", "", "Store.SearchSymbols"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			file, sym := parseSymbolArg(tt.input)
			if file != tt.wantFile || sym != tt.wantSym {
				t.Errorf("parseSymbolArg(%q) = (%q, %q), want (%q, %q)",
					tt.input, file, sym, tt.wantFile, tt.wantSym)
			}
		})
	}
}

func TestFlexResolveFileHintBehaviour(t *testing.T) {
	defer index.CloseAll()

	repoDir := t.TempDir()
	mustWrite := func(rel, content string) {
		t.Helper()
		path := filepath.Join(repoDir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite("alpha/dup.go", "package alpha\nfunc Dup() {}\n")
	mustWrite("beta/dup.go", "package beta\nfunc Dup() {}\n")
	mustWrite("delta/store.go", "package delta\nfunc Col() {}\n")
	mustWrite("zeta/loader.go", "package zeta\nfunc Load() {}\n")
	mustWrite("epsilon/mystore.go", "package epsilon\nfunc Col() {}\n")

	dbPath := filepath.Join(t.TempDir(), "test.db")
	if _, err := index.Index(repoDir, dbPath, index.Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}

	// The file hint wins when the name is defined in multiple files.
	res, err := flexResolve(dbPath, "alpha/dup.go:Dup")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 1 || res.Results[0].RelPath != "alpha/dup.go" {
		t.Errorf("file hint should narrow to alpha/dup.go; got %+v", res.Results)
	}

	// A hint matching no file falls back to the global result set.
	res, err = flexResolve(dbPath, "nosuch/file.go:Dup")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 2 {
		t.Errorf("unmatched hint should fall back to all definitions; got %+v", res.Results)
	}

	// Pinned: hint matching is HasSuffix/Contains, so a bare basename hint
	// also matches longer basenames (store.go matches mystore.go). If this
	// tightens to exact-basename matching, update the show help text too.
	res, err = flexResolve(dbPath, "store.go:Col")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Results) != 2 {
		t.Errorf("suffix-collision behaviour changed: want both store.go and mystore.go matches, got %+v", res.Results)
	}

	// A symbol starting with L resolves through the file hint end to end
	// (would fail if isFilePath mistook :Load for a line range).
	if !isFilePath("zeta/loader.go:Load") {
		res, err = flexResolve(dbPath, "zeta/loader.go:Load")
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Results) != 1 || res.Results[0].RelPath != filepath.Join("zeta", "loader.go") {
			t.Errorf("L-symbol file hint should resolve; got %+v", res.Results)
		}
	} else {
		t.Error("isFilePath must route zeta/loader.go:Load to symbol lookup")
	}

	// Symbol missing everywhere: the payload error names both the symbol and
	// the file hint.
	if _, err := buildShowSymbolPayload(dbPath, "alpha/dup.go:NoSuch", 0, false, nil, nil); err == nil ||
		!strings.Contains(err.Error(), "NoSuch") || !strings.Contains(err.Error(), "alpha/dup.go") {
		t.Errorf("scoped not-found error should name symbol and hint; got %v", err)
	}
}

func TestRepoBoundFilePathRejectsOutsideRepo(t *testing.T) {
	defer index.CloseAll()

	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\nfunc inside() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	if _, err := index.Index(repoDir, dbPath, index.Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}

	outside := filepath.Join(t.TempDir(), "secret.go")
	if err := os.WriteFile(outside, []byte("package secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := repoBoundFilePath(dbPath, outside)
	if err == nil || !strings.Contains(err.Error(), "outside repository") {
		t.Fatalf("expected outside-repository error, got %v", err)
	}
}

func TestRepoBoundFilePathRejectsSymlinkEscape(t *testing.T) {
	defer index.CloseAll()

	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "main.go"), []byte("package main\nfunc inside() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	if _, err := index.Index(repoDir, dbPath, index.Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(t.TempDir(), "secret.go")
	if err := os.WriteFile(target, []byte("package secret\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	linkPath := filepath.Join(repoDir, "leak.go")
	if err := os.Symlink(target, linkPath); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}

	_, err := repoBoundFilePath(dbPath, linkPath)
	if err == nil || !strings.Contains(err.Error(), "outside repository") {
		t.Fatalf("expected symlink escape error, got %v", err)
	}
}
