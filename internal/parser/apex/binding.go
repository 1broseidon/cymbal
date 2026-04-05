package apex

//#include "tree_sitter/parser.h"
//TSLanguage *tree_sitter_apex();
import "C"
import (
	"unsafe"

	sitter "github.com/smacker/go-tree-sitter"
)

// GetLanguage returns the Apex tree-sitter language.
// Uses the tree-sitter-sfapex grammar (https://github.com/aheber/tree-sitter-sfapex).
func GetLanguage() *sitter.Language {
	ptr := unsafe.Pointer(C.tree_sitter_apex())
	return sitter.NewLanguage(ptr)
}
