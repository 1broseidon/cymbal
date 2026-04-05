package apex_test

import (
	"context"
	"testing"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/1broseidon/cymbal/internal/parser/apex"
)

func TestGetLanguage(t *testing.T) {
	lang := apex.GetLanguage()
	if lang == nil {
		t.Fatal("GetLanguage() returned nil")
	}
}

func TestParseClass(t *testing.T) {
	src := []byte("public class AccountService { public void getAccount(Id accountId) {} }")
	parser := sitter.NewParser()
	parser.SetLanguage(apex.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatal(err)
	}
	root := tree.RootNode()
	if root.Type() != "parser_output" && root.Type() != "program" {
		t.Fatalf("unexpected root node type: %s", root.Type())
	}
	if root.ChildCount() == 0 {
		t.Fatal("parsed tree has no children")
	}
}

func TestParseTrigger(t *testing.T) {
	src := []byte("trigger AccountTrigger on Account (before insert) { }")
	parser := sitter.NewParser()
	parser.SetLanguage(apex.GetLanguage())
	tree, err := parser.ParseCtx(context.Background(), nil, src)
	if err != nil {
		t.Fatal(err)
	}
	root := tree.RootNode()
	if root.ChildCount() == 0 {
		t.Fatal("parsed trigger tree has no children")
	}
}
