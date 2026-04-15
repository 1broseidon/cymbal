package lang

import (
	dart "github.com/UserNobody14/tree-sitter-dart/bindings/go"
	apex "github.com/lynxbat/go-tree-sitter-apex"
	sitter "github.com/smacker/go-tree-sitter"
	"github.com/smacker/go-tree-sitter/bash"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/elixir"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/hcl"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/kotlin"
	"github.com/smacker/go-tree-sitter/lua"
	"github.com/smacker/go-tree-sitter/php"
	"github.com/smacker/go-tree-sitter/protobuf"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/scala"
	"github.com/smacker/go-tree-sitter/swift"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
	"github.com/smacker/go-tree-sitter/yaml"
)

// Default is the global language registry used throughout cymbal.
// It is the single source of truth for language names, file extensions,
// special filenames, and tree-sitter grammar availability.
var Default = NewRegistry(
	// ── Languages with tree-sitter grammars ──────────────────────────

	Language{
		Name:       "go",
		Extensions: []string{".go"},
		TreeSitter: golang.GetLanguage(),
	},
	Language{
		Name:       "python",
		Extensions: []string{".py", ".pyw"},
		TreeSitter: python.GetLanguage(),
	},
	Language{
		Name:       "javascript",
		Extensions: []string{".js", ".jsx", ".mjs", ".cjs"},
		TreeSitter: javascript.GetLanguage(),
	},
	Language{
		Name:       "typescript",
		Extensions: []string{".ts", ".tsx", ".mts", ".cts"},
		TreeSitter: typescript.GetLanguage(),
	},
	Language{
		Name:       "rust",
		Extensions: []string{".rs"},
		TreeSitter: rust.GetLanguage(),
	},
	Language{
		Name:       "ruby",
		Extensions: []string{".rb", ".rake", ".gemspec"},
		TreeSitter: ruby.GetLanguage(),
	},
	Language{
		Name:       "java",
		Extensions: []string{".java"},
		TreeSitter: java.GetLanguage(),
	},
	Language{
		Name:       "c",
		Extensions: []string{".c", ".h"},
		TreeSitter: c.GetLanguage(),
	},
	Language{
		Name:       "cpp",
		Extensions: []string{".cpp", ".cc", ".hpp", ".cxx", ".hxx", ".hh"},
		TreeSitter: cpp.GetLanguage(),
	},
	Language{
		Name:       "apex",
		Extensions: []string{".cls", ".trigger"},
		TreeSitter: apex.GetLanguage(),
	},
	Language{
		Name:       "csharp",
		Extensions: []string{".cs"},
		TreeSitter: csharp.GetLanguage(),
	},
	Language{
		Name:       "dart",
		Extensions: []string{".dart"},
		TreeSitter: sitter.NewLanguage(dart.Language()),
	},
	Language{
		Name:       "swift",
		Extensions: []string{".swift"},
		TreeSitter: swift.GetLanguage(),
	},
	Language{
		Name:       "kotlin",
		Extensions: []string{".kt", ".kts"},
		TreeSitter: kotlin.GetLanguage(),
	},
	Language{
		Name:       "lua",
		Extensions: []string{".lua"},
		TreeSitter: lua.GetLanguage(),
	},
	Language{
		Name:       "php",
		Extensions: []string{".php"},
		TreeSitter: php.GetLanguage(),
	},
	Language{
		Name:       "bash",
		Extensions: []string{".sh", ".bash", ".zsh"},
		TreeSitter: bash.GetLanguage(),
	},
	Language{
		Name:       "scala",
		Extensions: []string{".scala", ".sc"},
		TreeSitter: scala.GetLanguage(),
	},
	Language{
		Name:       "yaml",
		Extensions: []string{".yaml", ".yml"},
		TreeSitter: yaml.GetLanguage(),
	},
	Language{
		Name:       "elixir",
		Extensions: []string{".ex", ".exs"},
		TreeSitter: elixir.GetLanguage(),
	},
	Language{
		Name:       "hcl",
		Extensions: []string{".tf", ".hcl", ".tfvars"},
		TreeSitter: hcl.GetLanguage(),
	},
	Language{
		Name:       "protobuf",
		Extensions: []string{".proto"},
		TreeSitter: protobuf.GetLanguage(),
	},

	// ── Recognition-only languages (no tree-sitter grammar) ─────────
	// These are kept for file classification and non-indexing CLI flows.
	// Indexing/parsing code should use lang.Default.Supported to select the
	// parseable subset and must not assume every known language is indexable.

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
