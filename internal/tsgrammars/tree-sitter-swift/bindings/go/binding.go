package tree_sitter_swift

// #cgo CFLAGS: -std=c11 -fPIC
// #include "../../src/parser.c"
// #undef TOKEN_COUNT
// #include "../../src/scanner.c"
import "C"

import "unsafe"

// Language returns the tree-sitter Language for this grammar.
func Language() unsafe.Pointer {
	return unsafe.Pointer(C.tree_sitter_swift())
}
