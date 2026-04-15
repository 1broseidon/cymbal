package lang

import (
	"testing"
)

func TestDefaultRegistryNoPanic(t *testing.T) {
	// Default is built at init time; if we get here it didn't panic.
	if Default == nil {
		t.Fatal("Default registry is nil")
	}
}

func TestForFileExtensions(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		// Existing extensions
		{"main.go", "go"},
		{"app.py", "python"},
		{"index.js", "javascript"},
		{"App.jsx", "javascript"},
		{"index.ts", "typescript"},
		{"App.tsx", "typescript"},
		{"lib.rs", "rust"},
		{"app.rb", "ruby"},
		{"Main.java", "java"},
		{"foo.c", "c"},
		{"foo.h", "c"},
		{"foo.cpp", "cpp"},
		{"foo.cc", "cpp"},
		{"foo.hpp", "cpp"},
		{"Account.cls", "apex"},
		{"MyTrigger.trigger", "apex"},
		{"Program.cs", "csharp"},
		{"main.dart", "dart"},
		{"App.swift", "swift"},
		{"Main.kt", "kotlin"},
		{"script.lua", "lua"},
		{"index.php", "php"},
		{"run.sh", "bash"},
		{"run.bash", "bash"},
		{"run.zsh", "bash"},
		{"Main.scala", "scala"},
		{"config.yaml", "yaml"},
		{"config.yml", "yaml"},
		{"mix.ex", "elixir"},
		{"test.exs", "elixir"},
		{"main.tf", "hcl"},
		{"main.hcl", "hcl"},
		{"schema.proto", "protobuf"},

		// Issue #19 additions
		{"foo.cxx", "cpp"},
		{"foo.hxx", "cpp"},
		{"foo.hh", "cpp"},
		{"module.mjs", "javascript"},
		{"module.cjs", "javascript"},
		{"module.mts", "typescript"},
		{"module.cts", "typescript"},
		{"script.pyw", "python"},
		{"build.kts", "kotlin"},
		{"tasks.rake", "ruby"},
		{"mygem.gemspec", "ruby"},
		{"worksheet.sc", "scala"},
		{"vars.tfvars", "hcl"},

		// Recognition-only (no tree-sitter)
		{"main.zig", "zig"},
		{"config.toml", "toml"},
		{"data.json", "json"},
		{"README.md", "markdown"},
		{"query.sql", "sql"},
		{"module.erl", "erlang"},
		{"Main.hs", "haskell"},
		{"parser.ml", "ocaml"},
		{"parser.mli", "ocaml"},
		{"analysis.r", "r"},
		{"analysis.R", "r"},
		{"script.pl", "perl"},
		{"script.pm", "perl"},
		{"App.vue", "vue"},
		{"App.svelte", "svelte"},

		// Unrecognized
		{"foo.xyz", ""},
		{"foo.txt", ""},
		{"foo", ""},
	}

	for _, tt := range tests {
		got := Default.LangForFile(tt.path)
		if got != tt.want {
			t.Errorf("LangForFile(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestForFileSpecialFilenames(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"Makefile", "make"},
		{"makefile", "make"},
		{"GNUmakefile", "make"},
		{"Dockerfile", "dockerfile"},
		{"Jenkinsfile", "groovy"},
		{"CMakeLists.txt", "cmake"},
		{"src/Makefile", "make"},
		{"docker/Dockerfile", "dockerfile"},
	}

	for _, tt := range tests {
		got := Default.LangForFile(tt.path)
		if got != tt.want {
			t.Errorf("LangForFile(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestSupported(t *testing.T) {
	// Languages with tree-sitter grammars
	for _, name := range []string{"go", "python", "javascript", "typescript", "rust", "ruby", "java", "c", "cpp", "apex", "csharp", "dart", "swift", "kotlin", "lua", "php", "bash", "scala", "yaml", "elixir", "hcl", "protobuf"} {
		if !Default.Supported(name) {
			t.Errorf("Supported(%q) = false, want true", name)
		}
	}

	// Recognition-only languages should NOT be "supported" (no parser)
	for _, name := range []string{"zig", "toml", "json", "markdown", "sql", "erlang", "haskell", "ocaml", "r", "perl", "vue", "svelte", "make", "dockerfile", "groovy", "cmake"} {
		if Default.Supported(name) {
			t.Errorf("Supported(%q) = true, want false (no tree-sitter grammar)", name)
		}
	}

	// Unknown language
	if Default.Supported("brainfuck") {
		t.Error("Supported(brainfuck) = true, want false")
	}
}

func TestTreeSitter(t *testing.T) {
	if Default.TreeSitter("go") == nil {
		t.Error("TreeSitter(go) = nil, want non-nil")
	}
	if Default.TreeSitter("make") != nil {
		t.Error("TreeSitter(make) != nil, want nil")
	}
	if Default.TreeSitter("unknown") != nil {
		t.Error("TreeSitter(unknown) != nil, want nil")
	}
}

func TestKnown(t *testing.T) {
	if !Default.Known("go") {
		t.Error("Known(go) = false, want true")
	}
	if !Default.Known("make") {
		t.Error("Known(make) = false, want true")
	}
	if Default.Known("brainfuck") {
		t.Error("Known(brainfuck) = true, want false")
	}
}

func TestAll(t *testing.T) {
	all := Default.All()
	if len(all) == 0 {
		t.Fatal("All() returned empty")
	}

	// Verify it's a copy
	all[0] = nil
	if Default.All()[0] == nil {
		t.Error("All() returned a reference to internal slice, not a copy")
	}
}

func TestConsistency_ParseableLanguagesHaveTreeSitter(t *testing.T) {
	for _, l := range Default.All() {
		if l.Parseable() && l.TreeSitter == nil {
			t.Errorf("language %q: Parseable() is true but TreeSitter is nil", l.Name)
		}
		if !l.Parseable() && l.TreeSitter != nil {
			t.Errorf("language %q: Parseable() is false but TreeSitter is non-nil", l.Name)
		}
	}
}

func TestConsistency_AllExtensionsResolvable(t *testing.T) {
	for _, l := range Default.All() {
		for _, ext := range l.Extensions {
			got := Default.ForFile("test" + ext)
			if got != l {
				t.Errorf("extension %q should resolve to %q, got %v", ext, l.Name, got)
			}
		}
		for _, fn := range l.Filenames {
			got := Default.ForFile(fn)
			if got != l {
				t.Errorf("filename %q should resolve to %q, got %v", fn, l.Name, got)
			}
		}
	}
}

func TestDuplicateNamePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for duplicate name")
		}
	}()
	NewRegistry(
		Language{Name: "go", Extensions: []string{".go"}},
		Language{Name: "go", Extensions: []string{".go2"}},
	)
}

func TestDuplicateExtensionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for duplicate extension")
		}
	}()
	NewRegistry(
		Language{Name: "lang1", Extensions: []string{".x"}},
		Language{Name: "lang2", Extensions: []string{".x"}},
	)
}

func TestBadExtensionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for extension without dot")
		}
	}()
	NewRegistry(
		Language{Name: "bad", Extensions: []string{"nodot"}},
	)
}
