package parser

import (
	"context"
	"fmt"
	"os"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"

	"github.com/1broseidon/cymbal/lang"
	"github.com/1broseidon/cymbal/symbols"
)

// SupportedLanguage returns true if tree-sitter can parse this language.
// It delegates to the unified language registry.
func SupportedLanguage(l string) bool {
	return lang.Default.Supported(l)
}

// ParseFile parses a source file and extracts symbols, imports, and refs.
func ParseFile(filePath, l string) (*symbols.ParseResult, error) {
	tsLang := lang.Default.TreeSitter(l)
	if tsLang == nil {
		return nil, fmt.Errorf("unsupported language: %s", l)
	}

	src, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	return ParseSource(src, filePath, l, tsLang)
}

// ParseBytes parses source bytes (already read) and extracts symbols, imports, and refs.
// Use this when you already have the file contents to avoid a redundant ReadFile.
func ParseBytes(src []byte, filePath, l string) (*symbols.ParseResult, error) {
	tsLang := lang.Default.TreeSitter(l)
	if tsLang == nil {
		return nil, fmt.Errorf("unsupported language: %s", l)
	}
	return ParseSource(src, filePath, l, tsLang)
}

// ParseSource parses source bytes and extracts symbols, imports, and refs.
func ParseSource(src []byte, filePath, lang string, tsLang *sitter.Language) (*symbols.ParseResult, error) {
	p := sitter.NewParser()
	p.SetLanguage(tsLang)

	tree, err := p.ParseCtx(context.Background(), nil, src)
	if err != nil {
		return nil, fmt.Errorf("parsing: %w", err)
	}
	defer tree.Close()

	extractor := &symbolExtractor{
		src:      src,
		filePath: filePath,
		lang:     lang,
	}

	extractor.walk(tree.RootNode(), "", 0)
	return &symbols.ParseResult{
		Symbols: extractor.symbols,
		Imports: extractor.imports,
		Refs:    extractor.refs,
	}, nil
}

type symbolExtractor struct {
	src      []byte
	filePath string
	lang     string
	symbols  []symbols.Symbol
	imports  []symbols.Import
	refs     []symbols.Ref
}

func (e *symbolExtractor) walk(node *sitter.Node, parent string, depth int) {
	if node == nil {
		return
	}

	// Check for import statements.
	if imp, ok := e.extractImport(node); ok {
		e.imports = append(e.imports, imp)
	}

	// Check for call expressions / references.
	if ref, ok := e.extractRef(node); ok {
		e.refs = append(e.refs, ref)
	}

	// Check for inheritance / protocol conformance / interface implementation.
	// These are emitted as refs with Kind=RefKindImplements so `cymbal impls`
	// and impact-style queries can find them without polluting `trace`.
	for _, ref := range e.extractImplements(node) {
		e.refs = append(e.refs, ref)
	}

	sym, isSymbol := e.nodeToSymbol(node, parent, depth)
	if isSymbol {
		e.symbols = append(e.symbols, sym)
	}

	nextParent := parent
	if isSymbol {
		nextParent = sym.Name
	}

	childCount := int(node.ChildCount())
	for i := range childCount {
		child := node.Child(i)
		nextDepth := depth
		if isSymbol {
			nextDepth = depth + 1
		}
		e.walk(child, nextParent, nextDepth)
	}
}

// extractImport checks if the node is an import statement and returns the raw path.
func (e *symbolExtractor) extractImport(node *sitter.Node) (symbols.Import, bool) {
	nodeType := node.Type()

	switch e.lang {
	case "go":
		return e.extractImportGo(nodeType, node)
	case "python":
		return e.extractImportPython(nodeType, node)
	case "javascript", "typescript":
		return e.extractImportJS(nodeType, node)
	case "rust":
		return e.extractImportRust(nodeType, node)
	case "apex", "java", "scala":
		return e.extractImportJVM(nodeType, node)
	case "kotlin":
		return e.extractImportKotlin(nodeType, node)
	case "ruby":
		return e.extractImportRuby(nodeType, node)
	case "c", "cpp":
		return e.extractImportC(nodeType, node)
	case "elixir":
		return e.extractImportElixir(nodeType, node)
	case "protobuf":
		return e.extractImportProtobuf(nodeType, node)
	case "dart":
		return e.extractImportDart(nodeType, node)
	case "swift":
		return e.extractImportSwift(nodeType, node)
	case "csharp":
		return e.extractImportCSharp(nodeType, node)
	case "php":
		return e.extractImportPHP(nodeType, node)
	case "lua":
		return e.extractImportLua(nodeType, node)
	case "bash":
		return e.extractImportBash(nodeType, node)
	}
	return symbols.Import{}, false
}

func (e *symbolExtractor) extractImportGo(nodeType string, node *sitter.Node) (symbols.Import, bool) {
	if nodeType == "import_spec" {
		pathNode := node.ChildByFieldName("path")
		if pathNode != nil {
			raw := strings.Trim(pathNode.Content(e.src), "\"")
			return symbols.Import{RawPath: raw, Language: e.lang}, true
		}
	}
	return symbols.Import{}, false
}

func (e *symbolExtractor) extractImportPython(nodeType string, node *sitter.Node) (symbols.Import, bool) {
	if nodeType == "import_statement" || nodeType == "import_from_statement" {
		return symbols.Import{RawPath: node.Content(e.src), Language: e.lang}, true
	}
	return symbols.Import{}, false
}

func (e *symbolExtractor) extractImportJS(nodeType string, node *sitter.Node) (symbols.Import, bool) {
	if nodeType == "import_statement" {
		sourceNode := node.ChildByFieldName("source")
		if sourceNode != nil {
			raw := strings.Trim(sourceNode.Content(e.src), "\"'`")
			return symbols.Import{RawPath: raw, Language: e.lang}, true
		}
	}
	return symbols.Import{}, false
}

func (e *symbolExtractor) extractImportRust(nodeType string, node *sitter.Node) (symbols.Import, bool) {
	if nodeType == "use_declaration" {
		return symbols.Import{RawPath: node.Content(e.src), Language: e.lang}, true
	}
	return symbols.Import{}, false
}

func (e *symbolExtractor) extractImportJVM(nodeType string, node *sitter.Node) (symbols.Import, bool) {
	if nodeType == "import_declaration" {
		return symbols.Import{RawPath: node.Content(e.src), Language: e.lang}, true
	}
	return symbols.Import{}, false
}

func (e *symbolExtractor) extractImportRuby(nodeType string, node *sitter.Node) (symbols.Import, bool) {
	if nodeType == "call" {
		funcNode := node.ChildByFieldName("method")
		if funcNode != nil {
			name := funcNode.Content(e.src)
			if name == "require" || name == "require_relative" {
				argsNode := node.ChildByFieldName("arguments")
				if argsNode != nil {
					raw := strings.Trim(argsNode.Content(e.src), "()'\"")
					return symbols.Import{RawPath: raw, Language: e.lang}, true
				}
			}
		}
	}
	return symbols.Import{}, false
}

func (e *symbolExtractor) extractImportC(nodeType string, node *sitter.Node) (symbols.Import, bool) {
	if nodeType == "preproc_include" {
		pathNode := node.ChildByFieldName("path")
		if pathNode != nil {
			raw := strings.Trim(pathNode.Content(e.src), "<>\"")
			return symbols.Import{RawPath: raw, Language: e.lang}, true
		}
	}
	return symbols.Import{}, false
}

func (e *symbolExtractor) extractImportKotlin(nodeType string, node *sitter.Node) (symbols.Import, bool) {
	if nodeType == "import_header" {
		return symbols.Import{RawPath: node.Content(e.src), Language: e.lang}, true
	}
	return symbols.Import{}, false
}

func (e *symbolExtractor) extractImportElixir(nodeType string, node *sitter.Node) (symbols.Import, bool) {
	if nodeType == "call" {
		first := node.Child(0)
		if first != nil && first.Type() == "identifier" {
			name := first.Content(e.src)
			if name == "alias" || name == "import" || name == "use" || name == "require" {
				arg := node.Child(1)
				if arg != nil {
					return symbols.Import{RawPath: arg.Content(e.src), Language: e.lang}, true
				}
			}
		}
	}
	return symbols.Import{}, false
}

func (e *symbolExtractor) extractImportProtobuf(nodeType string, node *sitter.Node) (symbols.Import, bool) {
	if nodeType == "import" {
		for i := range int(node.ChildCount()) {
			child := node.Child(i)
			if child.Type() == "string" {
				raw := strings.Trim(child.Content(e.src), "\"")
				return symbols.Import{RawPath: raw, Language: e.lang}, true
			}
		}
	}
	return symbols.Import{}, false
}

// extractRef checks if the node is a call expression and returns the callee name.
func (e *symbolExtractor) extractRef(node *sitter.Node) (symbols.Ref, bool) {
	nodeType := node.Type()

	switch e.lang {
	case "go":
		if ref, ok := e.extractRefCallExpr(nodeType, node); ok {
			return ref, true
		}
		return e.extractRefGoCompositeLiteral(nodeType, node)
	case "javascript", "typescript":
		if ref, ok := e.extractRefCallExpr(nodeType, node); ok {
			return ref, true
		}
		return e.extractRefNewExpr(nodeType, node)
	case "rust", "c", "cpp":
		return e.extractRefCallExpr(nodeType, node)
	case "python":
		return e.extractRefPythonCall(nodeType, node)
	case "apex", "java", "scala":
		return e.extractRefJVM(nodeType, node)
	case "kotlin":
		return e.extractRefKotlin(nodeType, node)
	case "ruby":
		return e.extractRefRuby(nodeType, node)
	case "elixir":
		return e.extractRefElixir(nodeType, node)
	case "dart":
		return e.extractRefDart(nodeType, node)
	case "swift":
		return e.extractRefSwift(nodeType, node)
	case "csharp":
		return e.extractRefCSharp(nodeType, node)
	case "php":
		return e.extractRefPHP(nodeType, node)
	case "lua":
		return e.extractRefLua(nodeType, node)
	case "bash":
		return e.extractRefBash(nodeType, node)
	}
	return symbols.Ref{}, false
}

func (e *symbolExtractor) extractRefCallExpr(nodeType string, node *sitter.Node) (symbols.Ref, bool) {
	if nodeType != "call_expression" {
		return symbols.Ref{}, false
	}
	funcNode := node.ChildByFieldName("function")
	if funcNode != nil {
		name := extractCallName(funcNode, e.src, e.lang)
		if name != "" {
			return symbols.Ref{Name: name, Line: int(node.StartPoint().Row) + 1, Language: e.lang, Kind: symbols.RefKindCall}, true
		}
	}
	return symbols.Ref{}, false
}

func (e *symbolExtractor) extractRefGoCompositeLiteral(nodeType string, node *sitter.Node) (symbols.Ref, bool) {
	if nodeType != "composite_literal" {
		return symbols.Ref{}, false
	}
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil {
		return symbols.Ref{}, false
	}
	line := int(node.StartPoint().Row) + 1

	switch typeNode.Type() {
	case "type_identifier":
		name := typeNode.Content(e.src)
		if name != "" {
			return symbols.Ref{Name: name, Line: line, Language: e.lang}, true
		}
	case "qualified_type":
		// e.g. pkg.StructName — extract StructName
		nameNode := typeNode.ChildByFieldName("name")
		if nameNode != nil {
			name := nameNode.Content(e.src)
			if name != "" {
				return symbols.Ref{Name: name, Line: line, Language: e.lang}, true
			}
		}
	case "map_type":
		// map[KeyType]ValueType — emit refs for named key and value types
		keyNode := typeNode.ChildByFieldName("key")
		valNode := typeNode.ChildByFieldName("value")
		if keyNode != nil {
			switch keyNode.Type() {
			case "type_identifier":
				e.refs = append(e.refs, symbols.Ref{Name: keyNode.Content(e.src), Line: line, Language: e.lang})
			case "qualified_type":
				if nameNode := keyNode.ChildByFieldName("name"); nameNode != nil {
					e.refs = append(e.refs, symbols.Ref{Name: nameNode.Content(e.src), Line: line, Language: e.lang})
				}
			}
		}
		if valNode != nil {
			switch valNode.Type() {
			case "type_identifier":
				return symbols.Ref{Name: valNode.Content(e.src), Line: line, Language: e.lang}, true
			case "qualified_type":
				if nameNode := valNode.ChildByFieldName("name"); nameNode != nil {
					return symbols.Ref{Name: nameNode.Content(e.src), Line: line, Language: e.lang}, true
				}
			}
		}
	case "slice_type":
		// []TypeName{} — extract TypeName
		elemNode := typeNode.ChildByFieldName("element")
		if elemNode != nil {
			switch elemNode.Type() {
			case "type_identifier":
				return symbols.Ref{Name: elemNode.Content(e.src), Line: line, Language: e.lang}, true
			case "qualified_type":
				if nameNode := elemNode.ChildByFieldName("name"); nameNode != nil {
					return symbols.Ref{Name: nameNode.Content(e.src), Line: line, Language: e.lang}, true
				}
			}
		}
	case "array_type":
		elemNode := typeNode.ChildByFieldName("element")
		if elemNode != nil {
			switch elemNode.Type() {
			case "type_identifier":
				return symbols.Ref{Name: elemNode.Content(e.src), Line: line, Language: e.lang}, true
			case "qualified_type":
				if nameNode := elemNode.ChildByFieldName("name"); nameNode != nil {
					return symbols.Ref{Name: nameNode.Content(e.src), Line: line, Language: e.lang}, true
				}
			}
		}
	}
	return symbols.Ref{}, false
}

func (e *symbolExtractor) extractRefNewExpr(nodeType string, node *sitter.Node) (symbols.Ref, bool) {
	if nodeType != "new_expression" {
		return symbols.Ref{}, false
	}
	ctorNode := node.ChildByFieldName("constructor")
	if ctorNode != nil {
		name := extractCallName(ctorNode, e.src, e.lang)
		if name != "" {
			return symbols.Ref{Name: name, Line: int(node.StartPoint().Row) + 1, Language: e.lang, Kind: symbols.RefKindCall}, true
		}
	}
	return symbols.Ref{}, false
}

func (e *symbolExtractor) extractRefPythonCall(nodeType string, node *sitter.Node) (symbols.Ref, bool) {
	if nodeType != "call" {
		return symbols.Ref{}, false
	}
	funcNode := node.ChildByFieldName("function")
	if funcNode != nil {
		name := extractCallName(funcNode, e.src, e.lang)
		if name != "" {
			return symbols.Ref{Name: name, Line: int(node.StartPoint().Row) + 1, Language: e.lang, Kind: symbols.RefKindCall}, true
		}
	}
	return symbols.Ref{}, false
}

func (e *symbolExtractor) extractRefJVM(nodeType string, node *sitter.Node) (symbols.Ref, bool) {
	if nodeType != "method_invocation" {
		return symbols.Ref{}, false
	}
	nameNode := node.ChildByFieldName("name")
	if nameNode != nil {
		return symbols.Ref{Name: nameNode.Content(e.src), Line: int(node.StartPoint().Row) + 1, Language: e.lang, Kind: symbols.RefKindCall}, true
	}
	return symbols.Ref{}, false
}

func (e *symbolExtractor) extractRefRuby(nodeType string, node *sitter.Node) (symbols.Ref, bool) {
	if nodeType != "call" && nodeType != "method_call" {
		return symbols.Ref{}, false
	}
	nameNode := node.ChildByFieldName("method")
	if nameNode != nil {
		return symbols.Ref{Name: nameNode.Content(e.src), Line: int(node.StartPoint().Row) + 1, Language: e.lang, Kind: symbols.RefKindCall}, true
	}
	return symbols.Ref{}, false
}

func (e *symbolExtractor) extractRefKotlin(nodeType string, node *sitter.Node) (symbols.Ref, bool) {
	if nodeType != "call_expression" {
		return symbols.Ref{}, false
	}
	if node.ChildCount() > 0 {
		callee := node.Child(0)
		name := extractCallName(callee, e.src, e.lang)
		if name != "" {
			return symbols.Ref{Name: name, Line: int(node.StartPoint().Row) + 1, Language: e.lang, Kind: symbols.RefKindCall}, true
		}
	}
	return symbols.Ref{}, false
}

func (e *symbolExtractor) extractRefElixir(nodeType string, node *sitter.Node) (symbols.Ref, bool) {
	if nodeType != "call" {
		return symbols.Ref{}, false
	}
	first := node.Child(0)
	if first == nil {
		return symbols.Ref{}, false
	}
	if first.Type() == "dot" {
		for i := range int(first.ChildCount()) {
			child := first.Child(i)
			if child.Type() == "identifier" {
				return symbols.Ref{Name: child.Content(e.src), Line: int(node.StartPoint().Row) + 1, Language: e.lang, Kind: symbols.RefKindCall}, true
			}
		}
	} else if first.Type() == "identifier" {
		name := first.Content(e.src)
		switch name {
		case "def", "defp", "defmodule", "defmacro", "defmacrop",
			"defstruct", "defprotocol", "defimpl", "defguard",
			"alias", "import", "use", "require":
			return symbols.Ref{}, false
		}
		return symbols.Ref{Name: name, Line: int(node.StartPoint().Row) + 1, Language: e.lang, Kind: symbols.RefKindCall}, true
	}
	return symbols.Ref{}, false
}

// extractCallName gets the final identifier from a call expression function node.
// For "foo.bar.Baz()", returns "Baz". For "Baz()", returns "Baz".
// C++ extras (when lang == "cpp"):
//   - "Calculator::multiply()" -> "multiply"
//   - "ptr->method()" -> "method"
func extractCallName(node *sitter.Node, src []byte, lang string) string {
	content := strings.TrimSpace(node.Content(src))

	if lang == "c" || lang == "cpp" {
		// Normalize chained C/C++ qualifiers to the final callable name.
		// Handles separators like ., ->, and :: in mixed forms.
		for {
			idx, step := -1, 0
			if dot := strings.LastIndex(content, "."); dot > idx {
				idx, step = dot, 1
			}
			if arrow := strings.LastIndex(content, "->"); arrow > idx {
				idx, step = arrow, 2
			}
			if sep := strings.LastIndex(content, "::"); sep > idx {
				idx, step = sep, 2
			}
			if idx < 0 {
				break
			}
			content = content[idx+step:]
		}

		// C++ template calls (e.g., std::max<int>) should resolve to max.
		if lang == "cpp" {
			if lt := strings.Index(content, "<"); lt > 0 && strings.HasSuffix(content, ">") {
				content = content[:lt]
			}
		}
	} else {
		if dot := strings.LastIndex(content, "."); dot >= 0 {
			content = content[dot+1:]
		}
	}

	// Skip if it contains special characters (not a simple identifier).
	if strings.ContainsAny(content, "()[]{}") {
		return ""
	}
	return content
}

func (e *symbolExtractor) nodeToSymbol(node *sitter.Node, parent string, depth int) (symbols.Symbol, bool) {
	nodeType := node.Type()

	kind, nameNode := e.classifyNode(nodeType, node)
	if kind == "" {
		return symbols.Symbol{}, false
	}

	var name string
	if nameNode != nil {
		name = nameNode.Content(e.src)
	}
	// For HCL, the name is synthesized from labels, not a single AST node.
	if e.lang == "hcl" && kind != "" {
		name = e.hclBlockName(node)
	}
	if nameNode == nil && name == "" {
		return symbols.Symbol{}, false
	}
	if name == "" {
		return symbols.Symbol{}, false
	}

	sig := e.extractSignature(node, kind)

	return symbols.Symbol{
		Name:      name,
		Kind:      kind,
		File:      e.filePath,
		StartLine: int(node.StartPoint().Row) + 1,
		EndLine:   int(node.EndPoint().Row) + 1,
		StartCol:  int(node.StartPoint().Column),
		EndCol:    int(node.EndPoint().Column),
		Parent:    parent,
		Depth:     depth,
		Signature: sig,
		Language:  e.lang,
	}, true
}

func (e *symbolExtractor) classifyNode(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch e.lang {
	case "go":
		return e.classifyGo(nodeType, node)
	case "python":
		return e.classifyPython(nodeType, node)
	case "javascript", "typescript":
		return e.classifyJS(nodeType, node)
	case "rust":
		return e.classifyRust(nodeType, node)
	case "apex", "java", "scala":
		return e.classifyJavaLike(nodeType, node)
	case "kotlin":
		return e.classifyKotlin(nodeType, node)
	case "ruby":
		return e.classifyRuby(nodeType, node)
	case "c", "cpp":
		return e.classifyC(nodeType, node)
	case "elixir":
		return e.classifyElixir(nodeType, node)
	case "hcl":
		return e.classifyHCL(nodeType, node)
	case "protobuf":
		return e.classifyProtobuf(nodeType, node)
	case "dart":
		return e.classifyDart(nodeType, node)
	case "swift":
		return e.classifySwift(nodeType, node)
	case "csharp":
		return e.classifyCSharp(nodeType, node)
	case "php":
		return e.classifyPHP(nodeType, node)
	case "lua":
		return e.classifyLua(nodeType, node)
	case "bash":
		return e.classifyBash(nodeType, node)
	default:
		return e.classifyGeneric(nodeType, node)
	}
}

func (e *symbolExtractor) classifyGo(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "function_declaration":
		return "function", node.ChildByFieldName("name")
	case "method_declaration":
		return "method", node.ChildByFieldName("name")
	case "type_declaration":
		for i := range int(node.ChildCount()) {
			child := node.Child(i)
			if child.Type() == "type_spec" {
				nameNode := child.ChildByFieldName("name")
				typeNode := child.ChildByFieldName("type")
				if typeNode != nil {
					switch typeNode.Type() {
					case "struct_type":
						return "struct", nameNode
					case "interface_type":
						return "interface", nameNode
					default:
						return "type", nameNode
					}
				}
				return "type", nameNode
			}
		}
	case "const_declaration", "const_spec":
		if nodeType == "const_spec" {
			return "constant", node.ChildByFieldName("name")
		}
	case "var_declaration", "var_spec":
		if nodeType == "var_spec" {
			return "variable", node.ChildByFieldName("name")
		}
	}
	return "", nil
}

func (e *symbolExtractor) classifyPython(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "function_definition":
		// Skip if parent is decorated_definition — the parent already emits this symbol.
		if node.Parent() != nil && node.Parent().Type() == "decorated_definition" {
			return "", nil
		}
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil {
			name := nameNode.Content(e.src)
			if len(name) > 0 && name[0] == '_' && name != "__init__" {
				return "", nil
			}
		}
		return "function", nameNode
	case "class_definition":
		// Skip if parent is decorated_definition — the parent already emits this symbol.
		if node.Parent() != nil && node.Parent().Type() == "decorated_definition" {
			return "", nil
		}
		return "class", node.ChildByFieldName("name")
	case "decorated_definition":
		for i := range int(node.ChildCount()) {
			child := node.Child(i)
			kind, nameNode := e.classifyPythonInner(child.Type(), child)
			if kind != "" {
				return kind, nameNode
			}
		}
	}
	return "", nil
}

// classifyPythonInner is used by decorated_definition to classify the inner
// function/class without the parent check (which would infinitely skip).
func (e *symbolExtractor) classifyPythonInner(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "function_definition":
		nameNode := node.ChildByFieldName("name")
		if nameNode != nil {
			name := nameNode.Content(e.src)
			if len(name) > 0 && name[0] == '_' && name != "__init__" {
				return "", nil
			}
		}
		return "function", nameNode
	case "class_definition":
		return "class", node.ChildByFieldName("name")
	}
	return "", nil
}

func (e *symbolExtractor) classifyJS(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "function_declaration", "class_declaration", "interface_declaration",
		"type_alias_declaration", "enum_declaration", "lexical_declaration":
		// Skip if parent is export_statement — the parent already emits this symbol.
		if node.Parent() != nil && node.Parent().Type() == "export_statement" {
			return "", nil
		}
		return e.classifyJSInner(nodeType, node)
	case "method_definition":
		return "method", node.ChildByFieldName("name")
	case "export_statement":
		for i := range int(node.ChildCount()) {
			child := node.Child(i)
			kind, nameNode := e.classifyJSInner(child.Type(), child)
			if kind != "" {
				return kind, nameNode
			}
		}
	}
	return "", nil
}

// classifyJSInner classifies JS/TS nodes without the export_statement parent check.
func (e *symbolExtractor) classifyJSInner(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "function_declaration":
		return "function", node.ChildByFieldName("name")
	case "class_declaration":
		return "class", node.ChildByFieldName("name")
	case "interface_declaration":
		return "interface", node.ChildByFieldName("name")
	case "type_alias_declaration":
		return "type", node.ChildByFieldName("name")
	case "enum_declaration":
		return "enum", node.ChildByFieldName("name")
	case "lexical_declaration":
		for i := range int(node.ChildCount()) {
			child := node.Child(i)
			if child.Type() == "variable_declarator" {
				nameNode := child.ChildByFieldName("name")
				valueNode := child.ChildByFieldName("value")
				if valueNode != nil && (valueNode.Type() == "arrow_function" || valueNode.Type() == "function") {
					return "function", nameNode
				}
			}
		}
	}
	return "", nil
}

func (e *symbolExtractor) classifyRust(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "function_item":
		return "function", node.ChildByFieldName("name")
	case "struct_item":
		return "struct", node.ChildByFieldName("name")
	case "enum_item":
		return "enum", node.ChildByFieldName("name")
	case "trait_item":
		return "trait", node.ChildByFieldName("name")
	case "impl_item":
		// `impl Foo<T, U> for ...` — the `type` field points at a
		// generic_type wrapper whose own `type` field holds the bare
		// identifier. Descend so the symbol name is `Foo`, not `Foo<T, U>`,
		// so `impls --of Foo` matches both `struct Foo` and all impl blocks.
		typeNode := node.ChildByFieldName("type")
		if typeNode != nil && typeNode.Type() == "generic_type" {
			if inner := typeNode.ChildByFieldName("type"); inner != nil {
				typeNode = inner
			}
		}
		return "impl", typeNode
	case "type_item":
		return "type", node.ChildByFieldName("name")
	case "const_item":
		return "constant", node.ChildByFieldName("name")
	case "static_item":
		return "variable", node.ChildByFieldName("name")
	case "mod_item":
		return "module", node.ChildByFieldName("name")
	}
	return "", nil
}

func (e *symbolExtractor) classifyJavaLike(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "class_declaration":
		return "class", node.ChildByFieldName("name")
	case "method_declaration":
		return "method", node.ChildByFieldName("name")
	case "interface_declaration":
		return "interface", node.ChildByFieldName("name")
	case "enum_declaration":
		return "enum", node.ChildByFieldName("name")
	case "constructor_declaration":
		return "constructor", node.ChildByFieldName("name")
	case "field_declaration":
		for i := range int(node.ChildCount()) {
			child := node.Child(i)
			if child.Type() == "variable_declarator" {
				return "field", child.ChildByFieldName("name")
			}
		}
	}
	return "", nil
}

// findChildByType returns the first direct child with the given type.
func findChildByType(node *sitter.Node, typeName string) *sitter.Node {
	for i := range int(node.ChildCount()) {
		c := node.Child(i)
		if c.Type() == typeName {
			return c
		}
	}
	return nil
}

// findDescendantByType returns the first descendant (BFS) with the given type.
func findDescendantByType(node *sitter.Node, typeName string) *sitter.Node {
	queue := make([]*sitter.Node, 0, int(node.ChildCount()))
	for i := range int(node.ChildCount()) {
		queue = append(queue, node.Child(i))
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.Type() == typeName {
			return current
		}
		for i := range int(current.ChildCount()) {
			queue = append(queue, current.Child(i))
		}
	}
	return nil
}

// hasChildOfType reports whether node has any direct child with the given type.
func hasChildOfType(node *sitter.Node, typeName string) bool {
	return findChildByType(node, typeName) != nil
}

// kotlinInsideClassBody returns true if node sits inside a class_body /
// enum_class_body (i.e. its declaration is a member of a class/object).
func kotlinInsideClassBody(node *sitter.Node) bool {
	p := node.Parent()
	if p == nil {
		return false
	}
	t := p.Type()
	return t == "class_body" || t == "enum_class_body"
}

func (e *symbolExtractor) classifyKotlin(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "class_declaration":
		// Distinguish class / interface / enum by leading keyword child.
		kind := "class"
		if hasChildOfType(node, "interface") {
			kind = "interface"
		} else if hasChildOfType(node, "enum") {
			kind = "enum"
		}
		return kind, findChildByType(node, "type_identifier")
	case "object_declaration":
		return "object", findChildByType(node, "type_identifier")
	case "companion_object":
		// Named companion (`companion object Foo`) has a type_identifier; emit it.
		// Anonymous `companion object` is skipped — members still belong to the
		// enclosing class via the walker's parent tracking.
		if nameNode := findChildByType(node, "type_identifier"); nameNode != nil {
			return "object", nameNode
		}
		return "", nil
	case "function_declaration":
		kind := "function"
		if kotlinInsideClassBody(node) {
			kind = "method"
		}
		return kind, findChildByType(node, "simple_identifier")
	case "property_declaration":
		varDecl := findChildByType(node, "variable_declaration")
		if varDecl == nil {
			return "", nil
		}
		nameNode := findChildByType(varDecl, "simple_identifier")
		// Determine kind: const val → constant; inside class_body → field; else variable.
		kind := "variable"
		if kotlinInsideClassBody(node) {
			kind = "field"
		}
		// Detect `const` modifier.
		if mods := findChildByType(node, "modifiers"); mods != nil {
			for i := range int(mods.ChildCount()) {
				c := mods.Child(i)
				if c.Type() == "property_modifier" && c.Content(e.src) == "const" {
					kind = "constant"
					break
				}
			}
		}
		return kind, nameNode
	case "type_alias":
		return "type", findChildByType(node, "type_identifier")
	case "enum_entry":
		return "enum_member", findChildByType(node, "simple_identifier")
	}
	return "", nil
}

func (e *symbolExtractor) classifyRuby(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "method":
		return "method", node.ChildByFieldName("name")
	case "singleton_method":
		return "method", node.ChildByFieldName("name")
	case "class":
		return "class", node.ChildByFieldName("name")
	case "module":
		return "module", node.ChildByFieldName("name")
	}
	return "", nil
}

func (e *symbolExtractor) classifyC(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "function_definition":
		decl := node.ChildByFieldName("declarator")
		if decl != nil {
			return "function", decl.ChildByFieldName("declarator")
		}
	case "struct_specifier":
		return "struct", node.ChildByFieldName("name")
	case "enum_specifier":
		return "enum", node.ChildByFieldName("name")
	case "type_definition":
		return "type", node.ChildByFieldName("declarator")
	}
	return "", nil
}

func (e *symbolExtractor) classifyElixir(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	if nodeType != "call" {
		return "", nil
	}
	first := node.Child(0)
	if first == nil || first.Type() != "identifier" {
		return "", nil
	}
	keyword := first.Content(e.src)
	// In Elixir's tree-sitter grammar, arguments are positional children (index 1+),
	// not accessed via ChildByFieldName("arguments").
	arg := node.Child(1) // first argument after the keyword
	switch keyword {
	case "defmodule":
		if arg != nil {
			return "module", arg // alias node e.g. MyApp.Accounts
		}
	case "def":
		if arg != nil {
			if arg.Type() == "call" {
				return "function", arg.Child(0) // function name identifier
			}
			return "function", arg
		}
	case "defp":
		if arg != nil {
			if arg.Type() == "call" {
				return "function", arg.Child(0)
			}
			return "function", arg
		}
	case "defmacro", "defmacrop":
		if arg != nil {
			if arg.Type() == "call" {
				return "macro", arg.Child(0)
			}
			return "macro", arg
		}
	case "defprotocol":
		if arg != nil {
			return "interface", arg
		}
	}
	return "", nil
}

func (e *symbolExtractor) classifyHCL(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	if nodeType != "block" {
		return "", nil
	}
	// HCL blocks: identifier [string_lit...] { body }
	// e.g. resource "aws_instance" "web" { ... }
	blockType := node.Child(0)
	if blockType == nil || blockType.Type() != "identifier" {
		return "", nil
	}
	typeName := blockType.Content(e.src)
	// Check if block has any string labels after the type identifier.
	hasLabels := false
	for i := 1; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "string_lit" {
			hasLabels = true
			break
		} else {
			break
		}
	}
	switch typeName {
	case "resource", "variable", "output", "data", "module", "provider":
		if hasLabels {
			return e.hclKind(typeName), blockType
		}
	case "locals", "terraform":
		return e.hclKind(typeName), blockType
	}
	return "", nil
}

func (e *symbolExtractor) hclKind(typeName string) string {
	switch typeName {
	case "resource":
		return "resource"
	case "module", "terraform", "provider":
		return "module"
	default:
		return "variable"
	}
}

// hclBlockName synthesizes a name from block labels.
// e.g. resource "aws_instance" "web" → "aws_instance.web"
func (e *symbolExtractor) hclBlockName(node *sitter.Node) string {
	var labels []string
	for i := 1; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == "string_lit" {
			for j := range int(child.ChildCount()) {
				gc := child.Child(j)
				if gc.Type() == "template_literal" {
					labels = append(labels, gc.Content(e.src))
				}
			}
		} else {
			break
		}
	}
	if len(labels) == 0 {
		// For locals/terraform blocks with no labels.
		first := node.Child(0)
		if first != nil {
			return first.Content(e.src)
		}
		return ""
	}
	return strings.Join(labels, ".")
}

func (e *symbolExtractor) classifyProtobuf(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "message":
		return "struct", protoNameNode(node, "message_name")
	case "enum":
		return "enum", protoNameNode(node, "enum_name")
	case "service":
		return "interface", protoNameNode(node, "service_name")
	case "rpc":
		return "method", protoNameNode(node, "rpc_name")
	}
	return "", nil
}

func protoNameNode(node *sitter.Node, childType string) *sitter.Node {
	for i := range int(node.ChildCount()) {
		child := node.Child(i)
		if child.Type() == childType {
			// The name node wraps an identifier — return the identifier for clean content.
			if child.ChildCount() > 0 {
				return child.Child(0)
			}
			return child
		}
	}
	return nil
}

// dartInsideClassBody reports whether node sits inside a class_body,
// enum_body, or extension_body — i.e. its declaration is a member of a type.
// Note: the Dart grammar uses class_body for mixin bodies too, so mixin
// members are covered by the class_body check.
func dartInsideClassBody(node *sitter.Node) bool {
	p := node.Parent()
	for p != nil {
		t := p.Type()
		if t == "class_body" || t == "enum_body" || t == "extension_body" {
			return true
		}
		if t == "program" {
			return false
		}
		p = p.Parent()
	}
	return false
}

func (e *symbolExtractor) classifyDart(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "class_definition":
		return "class", node.ChildByFieldName("name")
	case "enum_declaration":
		return "enum", node.ChildByFieldName("name")
	case "mixin_declaration":
		return "mixin", findChildByType(node, "identifier")
	case "extension_declaration":
		return "extension", node.ChildByFieldName("name")
	case "type_alias":
		return "type", findChildByType(node, "type_identifier")
	case "function_signature":
		kind := "function"
		if dartInsideClassBody(node) {
			kind = "method"
		}
		return kind, node.ChildByFieldName("name")
	case "getter_signature":
		return "getter", node.ChildByFieldName("name")
	case "setter_signature":
		return "setter", node.ChildByFieldName("name")
	case "constructor_signature":
		return "constructor", node.ChildByFieldName("name")
	case "factory_constructor_signature":
		// factory Foo.named() — first identifier child is the class name.
		return "constructor", findChildByType(node, "identifier")
	case "constant_constructor_signature":
		return "constructor", findChildByType(node, "identifier")
	}
	return "", nil
}

func (e *symbolExtractor) extractImportDart(nodeType string, node *sitter.Node) (symbols.Import, bool) {
	if nodeType != "import_or_export" {
		return symbols.Import{}, false
	}
	// Dart: import 'package:foo/bar.dart';
	// AST: import_or_export → library_import → import_specification → configurable_uri → uri → string_literal
	// Walk descendants to find the configurable_uri node.
	if uri := findDescendantByType(node, "configurable_uri"); uri != nil {
		raw := strings.Trim(uri.Content(e.src), "'\"")
		return symbols.Import{RawPath: raw, Language: e.lang}, true
	}
	// Fallback: use the full statement text.
	return symbols.Import{RawPath: node.Content(e.src), Language: e.lang}, true
}

func (e *symbolExtractor) extractRefDart(nodeType string, node *sitter.Node) (symbols.Ref, bool) {
	// Dart call expressions are encoded as sibling sequences under a parent
	// (expression_statement, initialized_variable_definition, etc.):
	//
	//   Top-level call  print(x)        → identifier("print"),  selector(argument_part)
	//   Method call     c.area()        → identifier("c"),  selector(.area),  selector(argument_part)
	//   Constructor     Circle(5.0)     → identifier("Circle"), selector(argument_part)
	//
	// We trigger on a selector node that contains an argument_part (the "(…)").
	// Then we look at the preceding sibling to determine the callee name.
	if nodeType != "selector" || !hasChildOfType(node, "argument_part") {
		return symbols.Ref{}, false
	}

	parent := node.Parent()
	if parent == nil {
		return symbols.Ref{}, false
	}

	// Find this node's index among its siblings.
	idx := -1
	for i := range int(parent.ChildCount()) {
		if parent.Child(i) == node {
			idx = i
			break
		}
	}
	if idx < 1 {
		return symbols.Ref{}, false
	}

	prev := parent.Child(idx - 1)

	// Case 1: Previous sibling is a selector with unconditional_assignable_selector
	// → method call like c.area() — the ".area" selector precedes the "()" selector.
	if prev.Type() == "selector" {
		uas := findChildByType(prev, "unconditional_assignable_selector")
		if uas != nil {
			id := findChildByType(uas, "identifier")
			if id != nil {
				return symbols.Ref{
					Name:     id.Content(e.src),
					Line:     int(node.StartPoint().Row) + 1,
					Language: e.lang,
					Kind:     symbols.RefKindCall,
				}, true
			}
		}
		return symbols.Ref{}, false
	}

	// Case 2: Previous sibling is an identifier → top-level / constructor call.
	if prev.Type() == "identifier" {
		name := prev.Content(e.src)
		if name != "" {
			return symbols.Ref{
				Name:     name,
				Line:     int(node.StartPoint().Row) + 1,
				Language: e.lang,
				Kind:     symbols.RefKindCall,
			}, true
		}
	}

	return symbols.Ref{}, false
}

// --- Swift ---

func (e *symbolExtractor) extractImportSwift(nodeType string, node *sitter.Node) (symbols.Import, bool) {
	if nodeType != "import_declaration" {
		return symbols.Import{}, false
	}
	// `import Foundation` → identifier(simple_identifier("Foundation"))
	if id := findChildByType(node, "identifier"); id != nil {
		return symbols.Import{RawPath: strings.TrimSpace(id.Content(e.src)), Language: e.lang}, true
	}
	return symbols.Import{RawPath: strings.TrimSpace(node.Content(e.src)), Language: e.lang}, true
}

// extractRefSwift emits refs for call expressions and named type uses.
// tree-sitter-swift exposes named types as `user_type` in annotations,
// inheritance specifiers, generics, parameter types, and return types —
// trigger once per `user_type` and each nested occurrence is visited
// independently by the walker.
func (e *symbolExtractor) extractRefSwift(nodeType string, node *sitter.Node) (symbols.Ref, bool) {
	line := int(node.StartPoint().Row) + 1
	switch nodeType {
	case "call_expression":
		if node.ChildCount() == 0 {
			return symbols.Ref{}, false
		}
		if name := swiftCalleeName(node.Child(0), e.src); name != "" {
			return symbols.Ref{Name: name, Line: line, Language: e.lang, Kind: symbols.RefKindCall}, true
		}
	case "user_type":
		// Type mentions (annotations, generics, return types) — not calls.
		// These are intentionally Kind=use so `trace` doesn't surface them
		// while `cymbal impls` and type-level queries still can.
		if id := findChildByType(node, "type_identifier"); id != nil {
			return symbols.Ref{Name: id.Content(e.src), Line: line, Language: e.lang, Kind: symbols.RefKindUse}, true
		}
	}
	return symbols.Ref{}, false
}

// swiftCalleeName resolves the callable name from a call_expression's first child.
// Handles bare identifiers (`Foo()`) and navigation expressions (`x.y.z()` → `z`).
func swiftCalleeName(node *sitter.Node, src []byte) string {
	switch node.Type() {
	case "simple_identifier":
		return node.Content(src)
	case "navigation_expression":
		var lastSuffix *sitter.Node
		for i := range int(node.ChildCount()) {
			c := node.Child(i)
			if c.Type() == "navigation_suffix" {
				lastSuffix = c
			}
		}
		if lastSuffix != nil {
			if id := findChildByType(lastSuffix, "simple_identifier"); id != nil {
				return id.Content(src)
			}
		}
	}
	return ""
}

// classifySwift recognizes Swift declarations. tree-sitter-swift collapses
// struct/class/enum/extension into a single `class_declaration` node, so we
// disambiguate by the leading keyword.
func (e *symbolExtractor) classifySwift(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "class_declaration":
		return swiftClassKindAndName(node)
	case "protocol_declaration":
		return "protocol", findChildByType(node, "type_identifier")
	case "function_declaration", "protocol_function_declaration":
		kind := "function"
		if swiftInsideTypeBody(node) {
			kind = "method"
		}
		return kind, findChildByType(node, "simple_identifier")
	case "init_declaration":
		return "constructor", findChildByType(node, "init")
	case "deinit_declaration":
		return "destructor", findChildByType(node, "deinit")
	case "property_declaration":
		var nameNode *sitter.Node
		if pat := findChildByType(node, "pattern"); pat != nil {
			nameNode = findChildByType(pat, "simple_identifier")
		}
		kind := "variable"
		switch {
		case swiftInsideTypeBody(node):
			kind = "field"
		case swiftPropertyIsLet(node):
			kind = "constant"
		}
		return kind, nameNode
	case "enum_entry":
		return "enum_member", findChildByType(node, "simple_identifier")
	case "typealias_declaration":
		return "type", findChildByType(node, "type_identifier")
	}
	return "", nil
}

// swiftClassKindAndName reads the leading keyword of a class_declaration to
// distinguish struct/class/actor/enum/extension. For extensions the name lives
// one level deeper, under `user_type`.
func swiftClassKindAndName(node *sitter.Node) (string, *sitter.Node) {
	var kind string
	for i := range int(node.ChildCount()) {
		c := node.Child(i)
		switch c.Type() {
		case "struct":
			kind = "struct"
		case "class":
			kind = "class"
		case "actor":
			kind = "actor"
		case "enum":
			kind = "enum"
		case "extension":
			kind = "extension"
		}
		if kind != "" {
			break
		}
	}
	if kind == "" {
		return "", nil
	}
	if kind == "extension" {
		if ut := findChildByType(node, "user_type"); ut != nil {
			if id := findChildByType(ut, "type_identifier"); id != nil {
				return kind, id
			}
		}
		return "", nil
	}
	return kind, findChildByType(node, "type_identifier")
}

// swiftInsideTypeBody reports whether a declaration's direct parent is a
// class/struct/enum body or a protocol body (i.e. it's a member, not top-level).
func swiftInsideTypeBody(node *sitter.Node) bool {
	p := node.Parent()
	if p == nil {
		return false
	}
	switch p.Type() {
	case "class_body", "protocol_body", "enum_class_body":
		return true
	}
	return false
}

// swiftPropertyIsLet reports whether a top-level property_declaration uses
// `let` (constant binding) vs `var` (variable binding).
func swiftPropertyIsLet(node *sitter.Node) bool {
	vbp := findChildByType(node, "value_binding_pattern")
	if vbp == nil {
		return false
	}
	return findChildByType(vbp, "let") != nil
}

// swiftSignature slices the parenthesized parameter list plus any return
// clause, stopping at the function body (or EOF for protocol requirements).
func swiftSignature(node *sitter.Node, src []byte) string {
	var openParen, body *sitter.Node
	for i := range int(node.ChildCount()) {
		c := node.Child(i)
		switch c.Type() {
		case "(":
			if openParen == nil {
				openParen = c
			}
		case "function_body":
			body = c
		}
	}
	if openParen == nil {
		return ""
	}
	start := openParen.StartByte()
	end := node.EndByte()
	if body != nil {
		end = body.StartByte()
	}
	if end <= start || int(end) > len(src) {
		return ""
	}
	return strings.TrimSpace(string(src[start:end]))
}

// --- C# ---
//
// Grammar refs (tree-sitter-c-sharp):
//   using_directive            → "using Foo.Bar;" / "using static System.Math;"
//   namespace_declaration      → "namespace X { ... }"
//   class_declaration / struct_declaration / interface_declaration /
//   enum_declaration / record_declaration — all expose a `name` child
//   method_declaration / constructor_declaration / destructor_declaration /
//   property_declaration / field_declaration / delegate_declaration
//   invocation_expression      → function is `function` field
//   object_creation_expression → first identifier/qualified_name after `new`
//   member_access_expression   → x.Y — reference to Y

func (e *symbolExtractor) classifyCSharp(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "namespace_declaration", "file_scoped_namespace_declaration":
		return "namespace", node.ChildByFieldName("name")
	case "class_declaration":
		return "class", node.ChildByFieldName("name")
	case "struct_declaration":
		return "struct", node.ChildByFieldName("name")
	case "interface_declaration":
		return "interface", node.ChildByFieldName("name")
	case "enum_declaration":
		return "enum", node.ChildByFieldName("name")
	case "record_declaration", "record_struct_declaration":
		return "record", node.ChildByFieldName("name")
	case "delegate_declaration":
		return "delegate", node.ChildByFieldName("name")
	case "method_declaration":
		return "method", node.ChildByFieldName("name")
	case "constructor_declaration":
		return "constructor", node.ChildByFieldName("name")
	case "destructor_declaration":
		return "destructor", node.ChildByFieldName("name")
	case "property_declaration", "indexer_declaration":
		return "property", node.ChildByFieldName("name")
	case "field_declaration":
		// field_declaration → variable_declaration → variable_declarator(name)
		if vd := findChildByType(node, "variable_declaration"); vd != nil {
			if decl := findChildByType(vd, "variable_declarator"); decl != nil {
				if name := decl.ChildByFieldName("name"); name != nil {
					return "field", name
				}
			}
		}
	case "enum_member_declaration":
		return "constant", node.ChildByFieldName("name")
	}
	return "", nil
}

func (e *symbolExtractor) extractImportCSharp(nodeType string, node *sitter.Node) (symbols.Import, bool) {
	if nodeType != "using_directive" {
		return symbols.Import{}, false
	}
	// Strip leading `using ` / `using static ` and trailing `;`, normalize whitespace.
	raw := strings.TrimSpace(node.Content(e.src))
	raw = strings.TrimSuffix(raw, ";")
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "using"))
	raw = strings.TrimSpace(strings.TrimPrefix(raw, "static"))
	if raw == "" {
		return symbols.Import{}, false
	}
	return symbols.Import{RawPath: raw, Language: e.lang}, true
}

func (e *symbolExtractor) extractRefCSharp(nodeType string, node *sitter.Node) (symbols.Ref, bool) {
	line := int(node.StartPoint().Row) + 1
	switch nodeType {
	case "invocation_expression":
		fn := node.ChildByFieldName("function")
		if fn == nil && node.ChildCount() > 0 {
			fn = node.Child(0)
		}
		if fn == nil {
			return symbols.Ref{}, false
		}
		name := extractCallName(fn, e.src, e.lang)
		if name == "" {
			return symbols.Ref{}, false
		}
		return symbols.Ref{Name: name, Line: line, Language: e.lang, Kind: symbols.RefKindCall}, true
	case "object_creation_expression":
		// `new Foo(...)` or `new N.Foo(...)` — emit Foo.
		typeNode := node.ChildByFieldName("type")
		if typeNode == nil {
			// Fall back: first identifier or qualified_name after `new`.
			for i := 0; i < int(node.ChildCount()); i++ {
				c := node.Child(i)
				if c.Type() == "identifier" || c.Type() == "qualified_name" || c.Type() == "generic_name" {
					typeNode = c
					break
				}
			}
		}
		if typeNode == nil {
			return symbols.Ref{}, false
		}
		name := extractCallName(typeNode, e.src, e.lang)
		if name == "" {
			return symbols.Ref{}, false
		}
		return symbols.Ref{Name: name, Line: line, Language: e.lang, Kind: symbols.RefKindCall}, true
	}
	return symbols.Ref{}, false
}

// --- PHP ---
//
// Grammar refs (tree-sitter-php):
//   namespace_use_declaration  → "use Foo\\Bar;" / "use Foo\\Bar as Baz;"
//   namespace_definition       → "namespace Foo\\Bar;"
//   class_declaration / interface_declaration / trait_declaration /
//   enum_declaration — `name` field
//   function_definition / method_declaration — `name` field
//   function_call_expression   → function field
//   member_call_expression     → name field is method
//   scoped_call_expression     → name field is method (Foo::bar())
//   object_creation_expression → first type after `new`

func (e *symbolExtractor) classifyPHP(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "namespace_definition":
		return "namespace", node.ChildByFieldName("name")
	case "class_declaration":
		return "class", node.ChildByFieldName("name")
	case "interface_declaration":
		return "interface", node.ChildByFieldName("name")
	case "trait_declaration":
		return "trait", node.ChildByFieldName("name")
	case "enum_declaration":
		return "enum", node.ChildByFieldName("name")
	case "function_definition":
		return "function", node.ChildByFieldName("name")
	case "method_declaration":
		return "method", node.ChildByFieldName("name")
	case "const_element":
		// const_element has `name` field; parent const_declaration wraps it.
		return "constant", node.ChildByFieldName("name")
	case "enum_case":
		return "constant", node.ChildByFieldName("name")
	}
	return "", nil
}

func (e *symbolExtractor) extractImportPHP(nodeType string, node *sitter.Node) (symbols.Import, bool) {
	if nodeType != "namespace_use_declaration" {
		return symbols.Import{}, false
	}
	// Emit one import per `namespace_use_clause` child; fallback to raw text.
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		if c.Type() == "namespace_use_clause" {
			// First qualified_name / namespace_name / name child is the path.
			for j := 0; j < int(c.ChildCount()); j++ {
				cc := c.Child(j)
				switch cc.Type() {
				case "qualified_name", "namespace_name", "name":
					return symbols.Import{RawPath: cc.Content(e.src), Language: e.lang}, true
				}
			}
			return symbols.Import{RawPath: c.Content(e.src), Language: e.lang}, true
		}
	}
	return symbols.Import{}, false
}

func (e *symbolExtractor) extractRefPHP(nodeType string, node *sitter.Node) (symbols.Ref, bool) {
	line := int(node.StartPoint().Row) + 1
	switch nodeType {
	case "function_call_expression":
		fn := node.ChildByFieldName("function")
		if fn == nil {
			return symbols.Ref{}, false
		}
		name := extractCallName(fn, e.src, e.lang)
		if name == "" {
			return symbols.Ref{}, false
		}
		return symbols.Ref{Name: name, Line: line, Language: e.lang, Kind: symbols.RefKindCall}, true
	case "member_call_expression", "scoped_call_expression", "nullsafe_member_call_expression":
		nameNode := node.ChildByFieldName("name")
		if nameNode == nil {
			return symbols.Ref{}, false
		}
		name := strings.TrimSpace(nameNode.Content(e.src))
		if name == "" {
			return symbols.Ref{}, false
		}
		return symbols.Ref{Name: name, Line: line, Language: e.lang, Kind: symbols.RefKindCall}, true
	case "object_creation_expression":
		// Walk children for the first name/qualified_name/named_type.
		for i := 0; i < int(node.ChildCount()); i++ {
			c := node.Child(i)
			switch c.Type() {
			case "name", "qualified_name", "named_type":
				name := extractCallName(c, e.src, e.lang)
				if name == "" {
					continue
				}
				return symbols.Ref{Name: name, Line: line, Language: e.lang, Kind: symbols.RefKindCall}, true
			}
		}
	}
	return symbols.Ref{}, false
}

// --- Lua ---
//
// Grammar refs (tree-sitter-lua, smacker fork):
//   function_statement         → "function Foo() end" / "function M.foo() end" /
//                                "local function helper() end"
//     children: optional `local`, function_start, function_name (or identifier),
//               function_body_paren, parameter_list, function_body, function_end
//   function_name              → M.greet / M:new (table_dot / : separators)
//   function_call              → identifier | dot_index_expression | method_index_expression
//                                followed by function_arguments
//   require("x") / require "x" are function_call forms — handled as imports.

func (e *symbolExtractor) classifyLua(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	if nodeType != "function_statement" {
		return "", nil
	}
	// function_name child for "function X.y()" and "function X()";
	// identifier child for "local function helper()".
	if fn := findChildByType(node, "function_name"); fn != nil {
		// Emit the method name (last identifier in M.greet / M:new).
		kind := "function"
		// Treat `M:method` as method.
		if strings.Contains(fn.Content(e.src), ":") {
			kind = "method"
		}
		if id := lastIdentifier(fn); id != nil {
			return kind, id
		}
		return kind, fn
	}
	if id := findChildByType(node, "identifier"); id != nil {
		return "function", id
	}
	return "", nil
}

// lastIdentifier returns the last direct `identifier` child of n (for
// Lua function_name nodes like M.greet / M:new).
func lastIdentifier(n *sitter.Node) *sitter.Node {
	var last *sitter.Node
	for i := 0; i < int(n.ChildCount()); i++ {
		c := n.Child(i)
		if c.Type() == "identifier" {
			last = c
		}
	}
	return last
}

func (e *symbolExtractor) extractImportLua(nodeType string, node *sitter.Node) (symbols.Import, bool) {
	if nodeType != "function_call" {
		return symbols.Import{}, false
	}
	// First child must be identifier "require".
	if node.ChildCount() == 0 {
		return symbols.Import{}, false
	}
	callee := node.Child(0)
	if callee.Type() != "identifier" || strings.TrimSpace(callee.Content(e.src)) != "require" {
		return symbols.Import{}, false
	}
	// Argument shapes in tree-sitter-lua (smacker fork):
	//   require("x") → function_arguments wrapping string
	//   require "x"  → string_argument (direct wrapper with string_start/_content/_end)
	//   require 'x'  → same as above
	for i := 1; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		switch c.Type() {
		case "function_arguments", "string_argument":
			if s := findDescendantString(c, e.src); s != "" {
				return symbols.Import{RawPath: s, Language: e.lang}, true
			}
		case "string":
			if s := luaStringContent(c, e.src); s != "" {
				return symbols.Import{RawPath: s, Language: e.lang}, true
			}
		}
	}
	return symbols.Import{}, false
}

// findDescendantString walks a subtree looking for the first string node
// (including string_argument / string_content) and returns its unquoted content.
func findDescendantString(n *sitter.Node, src []byte) string {
	if n == nil {
		return ""
	}
	switch n.Type() {
	case "string_content":
		return n.Content(src)
	case "string", "string_argument":
		return luaStringContent(n, src)
	}
	for i := 0; i < int(n.ChildCount()); i++ {
		if s := findDescendantString(n.Child(i), src); s != "" {
			return s
		}
	}
	return ""
}

func luaStringContent(n *sitter.Node, src []byte) string {
	// tree-sitter-lua: string → string_start "string_content" string_end
	if c := findChildByType(n, "string_content"); c != nil {
		return c.Content(src)
	}
	// Fallback: strip surrounding quotes.
	raw := n.Content(src)
	if len(raw) >= 2 && (raw[0] == '"' || raw[0] == '\'') {
		return raw[1 : len(raw)-1]
	}
	return raw
}

func (e *symbolExtractor) extractRefLua(nodeType string, node *sitter.Node) (symbols.Ref, bool) {
	if nodeType != "function_call" {
		return symbols.Ref{}, false
	}
	if node.ChildCount() == 0 {
		return symbols.Ref{}, false
	}
	first := node.Child(0)
	// Don't emit a ref for `require(...)` — it's surfaced as an import.
	if first.Type() == "identifier" && strings.TrimSpace(first.Content(e.src)) == "require" {
		return symbols.Ref{}, false
	}
	// tree-sitter-lua (smacker fork) flattens `util.debug(…)` and `M:new(…)`
	// into a child list:
	//   function_call
	//     identifier            ← receiver (for method-like forms)
	//     '.' | self_call_colon
	//     identifier            ← method/field name
	//     function_call_paren   ← '('
	// Walk children up to the opening paren and pick the last `identifier`,
	// which is the callable name. For simple `foo(…)` this is still just the
	// first identifier.
	var name string
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		if c.Type() == "function_call_paren" || c.Type() == "function_arguments" ||
			c.Type() == "string_argument" || c.Type() == "string" {
			break
		}
		if c.Type() == "identifier" {
			name = strings.TrimSpace(c.Content(e.src))
		}
	}
	if name == "" {
		// Fall back to the composite-callee shape (dot_index_expression etc.).
		name = extractCallName(first, e.src, e.lang)
	}
	if name == "" {
		return symbols.Ref{}, false
	}
	return symbols.Ref{
		Name:     name,
		Line:     int(node.StartPoint().Row) + 1,
		Language: e.lang,
		Kind:     symbols.RefKindCall,
	}, true
}

// --- Bash ---
//
// Grammar refs (tree-sitter-bash):
//   function_definition        → "foo() { ... }" / "function foo { ... }"
//                                `name` field holds a `word` identifier.
//   command                    → command_name + argument words. `source x.sh`
//                                and `. x.sh` are both commands; treat as
//                                imports. Other commands emit a call ref.
//   variable_assignment        → handled generically; we skip it here.
//   declaration_command        → local/readonly/declare — not classified as
//                                a top-level symbol.

func (e *symbolExtractor) classifyBash(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	if nodeType != "function_definition" {
		return "", nil
	}
	// `name` field is a word node with the function identifier.
	if name := node.ChildByFieldName("name"); name != nil {
		return "function", name
	}
	return "", nil
}

func (e *symbolExtractor) extractImportBash(nodeType string, node *sitter.Node) (symbols.Import, bool) {
	if nodeType != "command" {
		return symbols.Import{}, false
	}
	cn := findChildByType(node, "command_name")
	if cn == nil {
		return symbols.Import{}, false
	}
	cmd := strings.TrimSpace(cn.Content(e.src))
	if cmd != "source" && cmd != "." {
		return symbols.Import{}, false
	}
	// First `word` after the command_name is the path being sourced.
	seenCmd := false
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		if !seenCmd {
			if c == cn {
				seenCmd = true
			}
			continue
		}
		if c.Type() == "word" || c.Type() == "string" || c.Type() == "raw_string" {
			raw := strings.TrimSpace(c.Content(e.src))
			// Strip surrounding quotes if present.
			if len(raw) >= 2 {
				if (raw[0] == '"' && raw[len(raw)-1] == '"') ||
					(raw[0] == '\'' && raw[len(raw)-1] == '\'') {
					raw = raw[1 : len(raw)-1]
				}
			}
			if raw == "" {
				continue
			}
			return symbols.Import{RawPath: raw, Language: e.lang}, true
		}
	}
	return symbols.Import{}, false
}

func (e *symbolExtractor) extractRefBash(nodeType string, node *sitter.Node) (symbols.Ref, bool) {
	if nodeType != "command" {
		return symbols.Ref{}, false
	}
	cn := findChildByType(node, "command_name")
	if cn == nil {
		return symbols.Ref{}, false
	}
	name := strings.TrimSpace(cn.Content(e.src))
	if name == "" {
		return symbols.Ref{}, false
	}
	// source/. are imports, not call refs. A minimal ignore list avoids noise
	// from shell builtins that dominate real scripts.
	switch name {
	case "source", ".", "set", "export", "readonly", "local", "declare",
		"unset", "shift", "return", "break", "continue", "exit",
		"if", "elif", "else", "fi", "then", "do", "done", "case", "esac",
		"for", "while", "until", "[", "[[", "true", "false", ":":
		return symbols.Ref{}, false
	}
	return symbols.Ref{
		Name:     name,
		Line:     int(node.StartPoint().Row) + 1,
		Language: e.lang,
		Kind:     symbols.RefKindCall,
	}, true
}

func (e *symbolExtractor) classifyGeneric(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "function_definition", "function_declaration":
		return "function", node.ChildByFieldName("name")
	case "class_definition", "class_declaration":
		return "class", node.ChildByFieldName("name")
	case "method_definition", "method_declaration":
		return "method", node.ChildByFieldName("name")
	}
	return "", nil
}

func (e *symbolExtractor) extractSignature(node *sitter.Node, kind string) string {
	switch kind {
	case "function", "method", "constructor", "destructor", "getter", "setter":
		if e.lang == "swift" {
			return swiftSignature(node, e.src)
		}
		var sig string

		// Parameters: try field name first, then language-specific node types.
		params := node.ChildByFieldName("parameters")
		if params != nil {
			sig = params.Content(e.src)
		} else if fvp := findChildByType(node, "function_value_parameters"); fvp != nil {
			// Kotlin
			sig = fvp.Content(e.src)
		} else if fpl := findChildByType(node, "formal_parameter_list"); fpl != nil {
			// Dart
			sig = fpl.Content(e.src)
		}

		// Return type: append if present. Covers TypeScript, Python, Rust, Go.
		if ret := node.ChildByFieldName("return_type"); ret != nil {
			rt := ret.Content(e.src)
			switch e.lang {
			case "python":
				sig += " -> " + rt
			case "go":
				sig += " " + rt
			default:
				// TypeScript, Rust, etc. — colon or arrow already in the node.
				if len(rt) > 0 && rt[0] != ':' && rt[0] != ' ' {
					sig += ": " + rt
				} else {
					sig += rt
				}
			}
		}
		// TypeScript type_annotation on the node (alternative to return_type field).
		if sig != "" && e.lang == "typescript" || e.lang == "tsx" || e.lang == "javascript" || e.lang == "jsx" {
			if ta := findChildByType(node, "type_annotation"); ta != nil && node.ChildByFieldName("return_type") == nil {
				sig += ta.Content(e.src)
			}
		}
		return sig

	case "struct", "class", "interface", "trait", "object", "enum", "mixin", "extension":
		content := node.Content(e.src)
		for i, ch := range content {
			if ch == '\n' || ch == '{' {
				return content[:i]
			}
		}
		if len(content) > 120 {
			return content[:120]
		}
		return content
	}
	return ""
}

// extractImplements returns zero or more "implements" edges for this node.
//
// This is a cross-language, best-effort extractor for inheritance /
// conformance / interface implementation. Edges are emitted as refs with
// Kind=RefKindImplements and Name=<target type name>. Name-based only; no
// type resolution is performed. External (e.g. framework) target types are
// stored by name.
//
// Languages where the concept doesn't apply return nil.
func (e *symbolExtractor) extractImplements(node *sitter.Node) []symbols.Ref {
	switch e.lang {
	case "swift":
		return e.extractImplementsSwift(node)
	case "go":
		return e.extractImplementsGo(node)
	case "java", "apex":
		return e.extractImplementsJava(node)
	case "csharp":
		return e.extractImplementsCSharp(node)
	case "kotlin":
		return e.extractImplementsKotlin(node)
	case "scala":
		return e.extractImplementsScala(node)
	case "typescript", "javascript":
		return e.extractImplementsTSJS(node)
	case "rust":
		return e.extractImplementsRust(node)
	case "dart":
		return e.extractImplementsDart(node)
	case "python":
		return e.extractImplementsPython(node)
	case "ruby":
		return e.extractImplementsRuby(node)
	case "php":
		return e.extractImplementsPHP(node)
	case "cpp":
		return e.extractImplementsCpp(node)
	}
	return nil
}

// implementsRef builds an implements-kind ref from a type-name node.
func (e *symbolExtractor) implementsRef(nameNode *sitter.Node, line int) (symbols.Ref, bool) {
	if nameNode == nil {
		return symbols.Ref{}, false
	}
	name := typeNameText(nameNode, e.src)
	if name == "" {
		return symbols.Ref{}, false
	}
	return symbols.Ref{
		Name:     name,
		Line:     line,
		Language: e.lang,
		Kind:     symbols.RefKindImplements,
	}, true
}

// typeNameText extracts the simple type name from a tree-sitter node that
// may wrap it in generics, qualifications, or type-specifier containers.
// Returns the final identifier segment (e.g. "Foo" from "pkg.Foo<T>").
func typeNameText(node *sitter.Node, src []byte) string {
	if node == nil {
		return ""
	}
	// Qualified-name-shaped nodes: we want the *final* segment, not the first.
	// Python "attribute" (foo.Bar), TS "nested_identifier" / "nested_type_identifier",
	// Java "scoped_type_identifier", C# "qualified_name", Ruby "scope_resolution",
	// etc. Tree-sitter grammars expose the final segment as a named field when
	// available; fall back to the last identifier-like child.
	switch node.Type() {
	case "attribute", "nested_identifier", "nested_type_identifier",
		"scoped_type_identifier", "qualified_name", "qualified_type",
		"scope_resolution":
		if f := node.ChildByFieldName("attribute"); f != nil {
			if t := typeNameText(f, src); t != "" {
				return t
			}
		}
		if f := node.ChildByFieldName("name"); f != nil {
			if t := typeNameText(f, src); t != "" {
				return t
			}
		}
		// Last identifier-like child.
		for i := int(node.ChildCount()) - 1; i >= 0; i-- {
			c := node.Child(i)
			switch c.Type() {
			case "type_identifier", "identifier", "constant":
				return c.Content(src)
			}
		}
	}
	// Prefer a direct identifier-like child if present.
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		switch c.Type() {
		case "type_identifier", "identifier":
			return c.Content(src)
		}
	}
	// Fall back to textual content, trimming generics / qualifiers.
	text := strings.TrimSpace(node.Content(src))
	if text == "" {
		return ""
	}
	if lt := strings.Index(text, "<"); lt > 0 {
		text = text[:lt]
	}
	if lt := strings.Index(text, "["); lt > 0 {
		text = text[:lt]
	}
	if lt := strings.Index(text, "("); lt > 0 {
		text = text[:lt]
	}
	if dot := strings.LastIndex(text, "."); dot >= 0 {
		text = text[dot+1:]
	}
	if dc := strings.LastIndex(text, "::"); dc >= 0 {
		text = text[dc+2:]
	}
	return strings.TrimSpace(text)
}

// collectImplementsFromClause walks the direct children of a clause node and
// emits one implements ref per child whose type matches any of itemTypes.
func (e *symbolExtractor) collectImplementsFromClause(clause *sitter.Node, line int, itemTypes ...string) []symbols.Ref {
	if clause == nil {
		return nil
	}
	wanted := make(map[string]struct{}, len(itemTypes))
	for _, t := range itemTypes {
		wanted[t] = struct{}{}
	}
	var out []symbols.Ref
	for i := 0; i < int(clause.ChildCount()); i++ {
		c := clause.Child(i)
		if _, ok := wanted[c.Type()]; !ok {
			continue
		}
		if ref, ok := e.implementsRef(c, line); ok {
			out = append(out, ref)
		}
	}
	return out
}

// -----------------------------------------------------------------------------
// Swift
// class / struct / enum / actor / extension / protocol declarations can have an
// inheritance clause with one or more inheritance_specifier children whose
// contained type is a user_type.
// -----------------------------------------------------------------------------
func (e *symbolExtractor) extractImplementsSwift(node *sitter.Node) []symbols.Ref {
	switch node.Type() {
	case "class_declaration", "protocol_declaration":
	default:
		return nil
	}
	line := int(node.StartPoint().Row) + 1

	var out []symbols.Ref
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		switch c.Type() {
		case "inheritance_specifier":
			if ref, ok := e.implementsRef(c, line); ok {
				out = append(out, ref)
			}
		case "type_inheritance_clause", "inheritance_clause":
			for j := 0; j < int(c.ChildCount()); j++ {
				gc := c.Child(j)
				if gc.Type() == "inheritance_specifier" {
					if ref, ok := e.implementsRef(gc, line); ok {
						out = append(out, ref)
					}
				}
			}
		}
	}
	return out
}

// -----------------------------------------------------------------------------
// Go
// interface embedding is the closest explicit "implements" signal Cymbal can
// see without type-checking. type T interface { io.Reader; Foo } → implements
// io.Reader and Foo.
// -----------------------------------------------------------------------------
func (e *symbolExtractor) extractImplementsGo(node *sitter.Node) []symbols.Ref {
	if node.Type() != "type_spec" {
		return nil
	}
	typeNode := node.ChildByFieldName("type")
	if typeNode == nil || typeNode.Type() != "interface_type" {
		return nil
	}

	var out []symbols.Ref
	// Go's tree-sitter grammar wraps each interface element in a type_elem.
	// Embedded types show up as `type_elem → type_identifier | qualified_type`
	// (while method specs show up as `method_elem`). Older grammar versions
	// may expose the identifier directly on interface_type; handle both.
	emit := func(n *sitter.Node) {
		switch n.Type() {
		case "type_identifier":
			if ref, ok := e.implementsRef(n, int(n.StartPoint().Row)+1); ok {
				out = append(out, ref)
			}
		case "qualified_type":
			nameNode := n.ChildByFieldName("name")
			if nameNode == nil {
				nameNode = n
			}
			if ref, ok := e.implementsRef(nameNode, int(n.StartPoint().Row)+1); ok {
				out = append(out, ref)
			}
		}
	}
	for i := 0; i < int(typeNode.ChildCount()); i++ {
		c := typeNode.Child(i)
		switch c.Type() {
		case "type_elem":
			for j := 0; j < int(c.ChildCount()); j++ {
				emit(c.Child(j))
			}
		case "type_identifier", "qualified_type":
			emit(c)
		}
	}
	return out
}

// -----------------------------------------------------------------------------
// Java / Apex
// class_declaration → superclass and super_interfaces clauses.
// interface_declaration → extends_interfaces.
// -----------------------------------------------------------------------------
func (e *symbolExtractor) extractImplementsJava(node *sitter.Node) []symbols.Ref {
	switch node.Type() {
	case "class_declaration", "interface_declaration":
	default:
		return nil
	}
	line := int(node.StartPoint().Row) + 1
	var out []symbols.Ref

	if sc := node.ChildByFieldName("superclass"); sc != nil {
		// superclass is "extends X" — walk for type_identifier children.
		if id := findChildByType(sc, "type_identifier"); id != nil {
			if ref, ok := e.implementsRef(id, line); ok {
				out = append(out, ref)
			}
		}
	}
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		switch c.Type() {
		case "super_interfaces", "extends_interfaces":
			// Contains a type_list of type_identifier / generic_type entries.
			list := findChildByType(c, "type_list")
			if list == nil {
				list = c
			}
			for j := 0; j < int(list.ChildCount()); j++ {
				item := list.Child(j)
				switch item.Type() {
				case "type_identifier", "generic_type", "scoped_type_identifier":
					if ref, ok := e.implementsRef(item, line); ok {
						out = append(out, ref)
					}
				}
			}
		}
	}
	return out
}

// -----------------------------------------------------------------------------
// C#
// class_declaration / struct_declaration / interface_declaration → base_list
// whose entries are identifier / qualified_name / generic_name.
// -----------------------------------------------------------------------------
func (e *symbolExtractor) extractImplementsCSharp(node *sitter.Node) []symbols.Ref {
	switch node.Type() {
	case "class_declaration", "struct_declaration", "interface_declaration", "record_declaration":
	default:
		return nil
	}
	line := int(node.StartPoint().Row) + 1
	base := findChildByType(node, "base_list")
	if base == nil {
		return nil
	}
	return e.collectImplementsFromClause(base, line,
		"identifier", "qualified_name", "generic_name", "predefined_type")
}

// -----------------------------------------------------------------------------
// Kotlin
// class_declaration → delegation_specifier entries; each specifier has a
// user_type (or constructor_invocation → user_type) child.
// -----------------------------------------------------------------------------
func (e *symbolExtractor) extractImplementsKotlin(node *sitter.Node) []symbols.Ref {
	switch node.Type() {
	case "class_declaration", "object_declaration":
	default:
		return nil
	}
	line := int(node.StartPoint().Row) + 1
	var out []symbols.Ref
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		if c.Type() != "delegation_specifier" {
			continue
		}
		// Find the user_type inside (may be nested under constructor_invocation).
		if ut := findDescendantByType(c, "user_type"); ut != nil {
			if ref, ok := e.implementsRef(ut, line); ok {
				out = append(out, ref)
			}
		}
	}
	return out
}

// -----------------------------------------------------------------------------
// Scala
// class / trait / object definitions with extends_clause + with_clauses.
// -----------------------------------------------------------------------------
func (e *symbolExtractor) extractImplementsScala(node *sitter.Node) []symbols.Ref {
	switch node.Type() {
	case "class_definition", "trait_definition", "object_definition":
	default:
		return nil
	}
	line := int(node.StartPoint().Row) + 1
	var out []symbols.Ref
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		switch c.Type() {
		case "extends_clause", "template_body":
			for j := 0; j < int(c.ChildCount()); j++ {
				gc := c.Child(j)
				switch gc.Type() {
				case "type_identifier", "generic_type":
					if ref, ok := e.implementsRef(gc, line); ok {
						out = append(out, ref)
					}
				}
			}
		}
	}
	return out
}

// -----------------------------------------------------------------------------
// TypeScript / JavaScript
// class_declaration → class_heritage → extends_clause + implements_clause.
// interface_declaration → extends_type_clause.
// -----------------------------------------------------------------------------
func (e *symbolExtractor) extractImplementsTSJS(node *sitter.Node) []symbols.Ref {
	switch node.Type() {
	case "class_declaration", "class", "interface_declaration":
	default:
		return nil
	}
	line := int(node.StartPoint().Row) + 1
	var out []symbols.Ref

	// Walk any heritage clause children.
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		switch c.Type() {
		case "class_heritage":
			for j := 0; j < int(c.ChildCount()); j++ {
				out = append(out, e.tsjsHeritageEntry(c.Child(j), line)...)
			}
		case "extends_clause", "implements_clause", "extends_type_clause":
			out = append(out, e.tsjsHeritageEntry(c, line)...)
		}
	}
	return out
}

func (e *symbolExtractor) tsjsHeritageEntry(node *sitter.Node, line int) []symbols.Ref {
	if node == nil {
		return nil
	}
	switch node.Type() {
	case "extends_clause", "implements_clause", "extends_type_clause":
		var out []symbols.Ref
		for i := 0; i < int(node.ChildCount()); i++ {
			c := node.Child(i)
			switch c.Type() {
			case "identifier", "type_identifier", "generic_type",
				"nested_identifier", "nested_type_identifier":
				if ref, ok := e.implementsRef(c, line); ok {
					out = append(out, ref)
				}
			}
		}
		return out
	}
	return nil
}

// -----------------------------------------------------------------------------
// Rust
// impl Trait for Type { ... } → implements edge from Type to Trait.
// -----------------------------------------------------------------------------
func (e *symbolExtractor) extractImplementsRust(node *sitter.Node) []symbols.Ref {
	if node.Type() != "impl_item" {
		return nil
	}
	trait := node.ChildByFieldName("trait")
	if trait == nil {
		return nil
	}
	line := int(node.StartPoint().Row) + 1
	if ref, ok := e.implementsRef(trait, line); ok {
		return []symbols.Ref{ref}
	}
	return nil
}

// -----------------------------------------------------------------------------
// Dart
// class_definition → superclass / interfaces / mixins clauses.
// -----------------------------------------------------------------------------
func (e *symbolExtractor) extractImplementsDart(node *sitter.Node) []symbols.Ref {
	switch node.Type() {
	case "class_definition", "mixin_declaration":
	default:
		return nil
	}
	line := int(node.StartPoint().Row) + 1
	var out []symbols.Ref
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		switch c.Type() {
		case "superclass", "interfaces", "mixins":
			for j := 0; j < int(c.ChildCount()); j++ {
				gc := c.Child(j)
				switch gc.Type() {
				case "type_identifier", "type_name":
					if ref, ok := e.implementsRef(gc, line); ok {
						out = append(out, ref)
					}
				case "type_list":
					for k := 0; k < int(gc.ChildCount()); k++ {
						if ref, ok := e.implementsRef(gc.Child(k), line); ok {
							out = append(out, ref)
						}
					}
				}
			}
		}
	}
	return out
}

// -----------------------------------------------------------------------------
// Python
// class_definition with superclasses → argument_list of identifier/attribute.
// Best-effort; structural protocols (PEP 544) are out of scope.
// -----------------------------------------------------------------------------
func (e *symbolExtractor) extractImplementsPython(node *sitter.Node) []symbols.Ref {
	if node.Type() != "class_definition" {
		return nil
	}
	supers := node.ChildByFieldName("superclasses")
	if supers == nil {
		return nil
	}
	line := int(node.StartPoint().Row) + 1
	var out []symbols.Ref
	for i := 0; i < int(supers.ChildCount()); i++ {
		c := supers.Child(i)
		switch c.Type() {
		case "identifier", "attribute", "subscript":
			if ref, ok := e.implementsRef(c, line); ok {
				out = append(out, ref)
			}
		}
	}
	return out
}

// -----------------------------------------------------------------------------
// Ruby
// class X < Y → implements Y. Module include/extend also emit implements edges.
// -----------------------------------------------------------------------------
func (e *symbolExtractor) extractImplementsRuby(node *sitter.Node) []symbols.Ref {
	line := int(node.StartPoint().Row) + 1
	switch node.Type() {
	case "class":
		if sc := node.ChildByFieldName("superclass"); sc != nil {
			// superclass node wraps the actual name.
			if id := findDescendantByType(sc, "constant"); id != nil {
				if ref, ok := e.implementsRef(id, line); ok {
					return []symbols.Ref{ref}
				}
			}
		}
	case "call":
		// include Foo / extend Foo
		method := node.ChildByFieldName("method")
		if method == nil {
			return nil
		}
		m := method.Content(e.src)
		if m != "include" && m != "extend" && m != "prepend" {
			return nil
		}
		args := node.ChildByFieldName("arguments")
		if args == nil {
			return nil
		}
		var out []symbols.Ref
		for i := 0; i < int(args.ChildCount()); i++ {
			c := args.Child(i)
			if c.Type() == "constant" || c.Type() == "scope_resolution" {
				if ref, ok := e.implementsRef(c, line); ok {
					out = append(out, ref)
				}
			}
		}
		return out
	}
	return nil
}

// -----------------------------------------------------------------------------
// PHP
// class_declaration → base_clause (extends) + class_interface_clause (implements).
// interface_declaration → base_clause for extends.
// -----------------------------------------------------------------------------
func (e *symbolExtractor) extractImplementsPHP(node *sitter.Node) []symbols.Ref {
	switch node.Type() {
	case "class_declaration", "interface_declaration", "trait_declaration":
	default:
		return nil
	}
	line := int(node.StartPoint().Row) + 1
	var out []symbols.Ref
	for i := 0; i < int(node.ChildCount()); i++ {
		c := node.Child(i)
		switch c.Type() {
		case "base_clause", "class_interface_clause":
			for j := 0; j < int(c.ChildCount()); j++ {
				gc := c.Child(j)
				switch gc.Type() {
				case "name", "qualified_name":
					if ref, ok := e.implementsRef(gc, line); ok {
						out = append(out, ref)
					}
				}
			}
		}
	}
	return out
}

// -----------------------------------------------------------------------------
// C++
// class_specifier / struct_specifier → base_class_clause whose entries carry
// identifier / qualified_identifier / template_type names.
// -----------------------------------------------------------------------------
func (e *symbolExtractor) extractImplementsCpp(node *sitter.Node) []symbols.Ref {
	switch node.Type() {
	case "class_specifier", "struct_specifier":
	default:
		return nil
	}
	line := int(node.StartPoint().Row) + 1
	base := findChildByType(node, "base_class_clause")
	if base == nil {
		return nil
	}
	return e.collectImplementsFromClause(base, line,
		"type_identifier", "qualified_identifier", "template_type", "identifier")
}
