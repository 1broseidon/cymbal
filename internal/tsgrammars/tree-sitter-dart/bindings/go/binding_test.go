package tree_sitter_dart_test

import (
	"testing"

	tree_sitter_dart "github.com/1broseidon/cymbal/internal/tsgrammars/tree-sitter-dart/bindings/go"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

func TestCanLoadGrammar(t *testing.T) {
	language := tree_sitter.NewLanguage(tree_sitter_dart.Language())
	if language == nil {
		t.Errorf("Error loading Dart grammar")
	}
}
