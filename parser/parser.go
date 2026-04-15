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
			return symbols.Ref{Name: name, Line: int(node.StartPoint().Row) + 1, Language: e.lang}, true
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
			return symbols.Ref{Name: name, Line: int(node.StartPoint().Row) + 1, Language: e.lang}, true
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
			return symbols.Ref{Name: name, Line: int(node.StartPoint().Row) + 1, Language: e.lang}, true
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
		return symbols.Ref{Name: nameNode.Content(e.src), Line: int(node.StartPoint().Row) + 1, Language: e.lang}, true
	}
	return symbols.Ref{}, false
}

func (e *symbolExtractor) extractRefRuby(nodeType string, node *sitter.Node) (symbols.Ref, bool) {
	if nodeType != "call" && nodeType != "method_call" {
		return symbols.Ref{}, false
	}
	nameNode := node.ChildByFieldName("method")
	if nameNode != nil {
		return symbols.Ref{Name: nameNode.Content(e.src), Line: int(node.StartPoint().Row) + 1, Language: e.lang}, true
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
			return symbols.Ref{Name: name, Line: int(node.StartPoint().Row) + 1, Language: e.lang}, true
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
				return symbols.Ref{Name: child.Content(e.src), Line: int(node.StartPoint().Row) + 1, Language: e.lang}, true
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
		return symbols.Ref{Name: name, Line: int(node.StartPoint().Row) + 1, Language: e.lang}, true
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
		return "impl", node.ChildByFieldName("type")
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
			}, true
		}
	}

	return symbols.Ref{}, false
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
	case "function", "method", "constructor", "getter", "setter":
		params := node.ChildByFieldName("parameters")
		if params != nil {
			return params.Content(e.src)
		}
		// Kotlin grammar has no "parameters" field — look up by type.
		if fvp := findChildByType(node, "function_value_parameters"); fvp != nil {
			return fvp.Content(e.src)
		}
		// Dart grammar uses formal_parameter_list.
		if fpl := findChildByType(node, "formal_parameter_list"); fpl != nil {
			return fpl.Content(e.src)
		}
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
