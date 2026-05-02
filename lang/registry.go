package lang

import (
	tsdart "github.com/1broseidon/cymbal/internal/tsgrammars/tree-sitter-dart/bindings/go"
	tselixir "github.com/1broseidon/cymbal/internal/tsgrammars/tree-sitter-elixir/bindings/go"
	tsswift "github.com/1broseidon/cymbal/internal/tsgrammars/tree-sitter-swift/bindings/go"
	tsprotobuf "github.com/coder3101/tree-sitter-proto/bindings/go"
	tshcl "github.com/tree-sitter-grammars/tree-sitter-hcl/bindings/go"
	tskotlin "github.com/tree-sitter-grammars/tree-sitter-kotlin/bindings/go"
	tslua "github.com/tree-sitter-grammars/tree-sitter-lua/bindings/go"
	tsyaml "github.com/tree-sitter-grammars/tree-sitter-yaml/bindings/go"
	sitter "github.com/tree-sitter/go-tree-sitter"
	tsbash "github.com/tree-sitter/tree-sitter-bash/bindings/go"
	tscsharp "github.com/tree-sitter/tree-sitter-c-sharp/bindings/go"
	tsc "github.com/tree-sitter/tree-sitter-c/bindings/go"
	tscpp "github.com/tree-sitter/tree-sitter-cpp/bindings/go"
	tsgo "github.com/tree-sitter/tree-sitter-go/bindings/go"
	tsjava "github.com/tree-sitter/tree-sitter-java/bindings/go"
	tsjavascript "github.com/tree-sitter/tree-sitter-javascript/bindings/go"
	tsphp "github.com/tree-sitter/tree-sitter-php/bindings/go"
	tspython "github.com/tree-sitter/tree-sitter-python/bindings/go"
	tsruby "github.com/tree-sitter/tree-sitter-ruby/bindings/go"
	tsrust "github.com/tree-sitter/tree-sitter-rust/bindings/go"
	tsscala "github.com/tree-sitter/tree-sitter-scala/bindings/go"
	tstypescript "github.com/tree-sitter/tree-sitter-typescript/bindings/go"
)

// Default is the global language registry used throughout cymbal.
// It is the single source of truth for language names, file extensions,
// special filenames, and tree-sitter grammar availability.
var Default = NewRegistry(
	// ── Languages with tree-sitter grammars ──────────────────────────

	Language{
		Name:       "go",
		Extensions: []string{".go"},
		TreeSitter: sitter.NewLanguage(tsgo.Language()),
	},
	Language{
		Name:       "python",
		Extensions: []string{".py", ".pyw"},
		TreeSitter: sitter.NewLanguage(tspython.Language()),
	},
	Language{
		Name:       "javascript",
		Extensions: []string{".js", ".jsx", ".mjs", ".cjs"},
		TreeSitter: sitter.NewLanguage(tsjavascript.Language()),
	},
	Language{
		Name:       "typescript",
		Extensions: []string{".ts", ".tsx", ".mts", ".cts"},
		TreeSitter: sitter.NewLanguage(tstypescript.LanguageTypescript()),
	},
	Language{
		Name:       "rust",
		Extensions: []string{".rs"},
		TreeSitter: sitter.NewLanguage(tsrust.Language()),
	},
	Language{
		Name:       "ruby",
		Extensions: []string{".rb", ".rake", ".gemspec"},
		TreeSitter: sitter.NewLanguage(tsruby.Language()),
	},
	Language{
		Name:       "java",
		Extensions: []string{".java"},
		TreeSitter: sitter.NewLanguage(tsjava.Language()),
	},
	Language{
		Name:       "c",
		Extensions: []string{".c", ".h"},
		TreeSitter: sitter.NewLanguage(tsc.Language()),
	},
	Language{
		Name:       "cpp",
		Extensions: []string{".cpp", ".cc", ".hpp", ".cxx", ".hxx", ".hh"},
		TreeSitter: sitter.NewLanguage(tscpp.Language()),
	},
	Language{
		Name:       "csharp",
		Extensions: []string{".cs"},
		TreeSitter: sitter.NewLanguage(tscsharp.Language()),
	},
	Language{
		Name:       "dart",
		Extensions: []string{".dart"},
		TreeSitter: sitter.NewLanguage(tsdart.Language()),
	},
	Language{
		Name:       "swift",
		Extensions: []string{".swift"},
		TreeSitter: sitter.NewLanguage(tsswift.Language()),
	},
	Language{
		Name:       "kotlin",
		Extensions: []string{".kt", ".kts"},
		TreeSitter: sitter.NewLanguage(tskotlin.Language()),
	},
	Language{
		Name:       "lua",
		Extensions: []string{".lua"},
		TreeSitter: sitter.NewLanguage(tslua.Language()),
	},
	Language{
		Name:       "php",
		Extensions: []string{".php"},
		TreeSitter: sitter.NewLanguage(tsphp.LanguagePHP()),
	},
	Language{
		Name:       "bash",
		Extensions: []string{".sh", ".bash", ".zsh"},
		TreeSitter: sitter.NewLanguage(tsbash.Language()),
	},
	Language{
		Name:       "scala",
		Extensions: []string{".scala", ".sc"},
		TreeSitter: sitter.NewLanguage(tsscala.Language()),
	},
	Language{
		Name:       "yaml",
		Extensions: []string{".yaml", ".yml"},
		TreeSitter: sitter.NewLanguage(tsyaml.Language()),
	},
	Language{
		Name:       "elixir",
		Extensions: []string{".ex", ".exs"},
		TreeSitter: sitter.NewLanguage(tselixir.Language()),
	},
	Language{
		Name:       "hcl",
		Extensions: []string{".tf", ".hcl", ".tfvars"},
		TreeSitter: sitter.NewLanguage(tshcl.Language()),
	},
	Language{
		Name:       "protobuf",
		Extensions: []string{".proto"},
		TreeSitter: sitter.NewLanguage(tsprotobuf.Language()),
	},

	// ── Recognition-only languages (no tree-sitter grammar) ─────────
	// These are kept for file classification and non-indexing CLI flows.
	// Indexing/parsing code should use lang.Default.Supported to select the
	// parseable subset and must not assume every known language is indexable.

	Language{
		Name:       "apex",
		Extensions: []string{".cls", ".trigger"},
	},
	Language{
		Name:       "zig",
		Extensions: []string{".zig"},
	},
	Language{
		Name:       "toml",
		Extensions: []string{".toml"},
	},
	Language{
		Name:       "json",
		Extensions: []string{".json"},
	},
	Language{
		Name:       "markdown",
		Extensions: []string{".md"},
	},
	Language{
		Name:       "sql",
		Extensions: []string{".sql"},
	},
	Language{
		Name:       "erlang",
		Extensions: []string{".erl"},
	},
	Language{
		Name:       "haskell",
		Extensions: []string{".hs"},
	},
	Language{
		Name:       "ocaml",
		Extensions: []string{".ml", ".mli"},
	},
	Language{
		Name:       "r",
		Extensions: []string{".r", ".R"},
	},
	Language{
		Name:       "perl",
		Extensions: []string{".pl", ".pm"},
	},
	Language{
		Name:       "vue",
		Extensions: []string{".vue"},
	},
	Language{
		Name:       "svelte",
		Extensions: []string{".svelte"},
	},

	// ── Special-filename-only languages ─────────────────────────────

	Language{
		Name:      "make",
		Filenames: []string{"Makefile", "makefile", "GNUmakefile"},
	},
	Language{
		Name:      "dockerfile",
		Filenames: []string{"Dockerfile"},
	},
	Language{
		Name:      "groovy",
		Filenames: []string{"Jenkinsfile"},
	},
	Language{
		Name:      "cmake",
		Filenames: []string{"CMakeLists.txt"},
	},
)
