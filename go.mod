module github.com/1broseidon/cymbal

go 1.25.9

require (
	github.com/UserNobody14/tree-sitter-dart v0.0.0-20240901045915-8197a3123420
	github.com/alex-pinkus/tree-sitter-swift v0.0.0-20260428021725-c354345348cf
	github.com/coder3101/tree-sitter-proto v0.0.0-20250826173151-a1fbf36c3029
	github.com/mattn/go-sqlite3 v1.14.37
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.9
	github.com/tree-sitter-grammars/tree-sitter-hcl v1.2.0
	github.com/tree-sitter-grammars/tree-sitter-kotlin v1.1.0
	github.com/tree-sitter-grammars/tree-sitter-lua v0.5.0
	github.com/tree-sitter-grammars/tree-sitter-yaml v0.7.2
	github.com/tree-sitter/go-tree-sitter v0.25.0
	github.com/tree-sitter/tree-sitter-bash v0.25.1
	github.com/tree-sitter/tree-sitter-c v0.24.2
	github.com/tree-sitter/tree-sitter-c-sharp v0.23.5
	github.com/tree-sitter/tree-sitter-cpp v0.23.4
	github.com/tree-sitter/tree-sitter-elixir v0.3.5
	github.com/tree-sitter/tree-sitter-go v0.25.0
	github.com/tree-sitter/tree-sitter-java v0.23.5
	github.com/tree-sitter/tree-sitter-javascript v0.25.0
	github.com/tree-sitter/tree-sitter-php v0.24.2
	github.com/tree-sitter/tree-sitter-python v0.25.0
	github.com/tree-sitter/tree-sitter-ruby v0.23.1
	github.com/tree-sitter/tree-sitter-rust v0.24.2
	github.com/tree-sitter/tree-sitter-scala v0.26.0
	github.com/tree-sitter/tree-sitter-typescript v0.23.2
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/mattn/go-pointer v0.0.1 // indirect
)

replace github.com/tree-sitter/tree-sitter-elixir => github.com/elixir-lang/tree-sitter-elixir v0.3.5

replace github.com/UserNobody14/tree-sitter-dart => ./internal/tsgrammars/tree-sitter-dart

replace github.com/alex-pinkus/tree-sitter-swift => ./internal/tsgrammars/tree-sitter-swift
