package parser

import (
	"context"
	"fmt"
	"os"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
	"github.com/1broseidon/cymbal/internal/parser/apex"
	"github.com/smacker/go-tree-sitter/bash"
	"github.com/smacker/go-tree-sitter/c"
	"github.com/smacker/go-tree-sitter/cpp"
	"github.com/smacker/go-tree-sitter/csharp"
	"github.com/smacker/go-tree-sitter/golang"
	"github.com/smacker/go-tree-sitter/java"
	"github.com/smacker/go-tree-sitter/javascript"
	"github.com/smacker/go-tree-sitter/kotlin"
	"github.com/smacker/go-tree-sitter/lua"
	"github.com/smacker/go-tree-sitter/php"
	"github.com/smacker/go-tree-sitter/python"
	"github.com/smacker/go-tree-sitter/ruby"
	"github.com/smacker/go-tree-sitter/rust"
	"github.com/smacker/go-tree-sitter/scala"
	"github.com/smacker/go-tree-sitter/swift"
	"github.com/smacker/go-tree-sitter/typescript/typescript"
	"github.com/smacker/go-tree-sitter/yaml"

	"github.com/1broseidon/cymbal/internal/symbols"
)

var languages = map[string]*sitter.Language{
	"apex":       apex.GetLanguage(),
	"go":         golang.GetLanguage(),
	"python":     python.GetLanguage(),
	"javascript": javascript.GetLanguage(),
	"typescript": typescript.GetLanguage(),
	"rust":       rust.GetLanguage(),
	"ruby":       ruby.GetLanguage(),
	"java":       java.GetLanguage(),
	"c":          c.GetLanguage(),
	"cpp":        cpp.GetLanguage(),
	"csharp":     csharp.GetLanguage(),
	"swift":      swift.GetLanguage(),
	"kotlin":     kotlin.GetLanguage(),
	"lua":        lua.GetLanguage(),
	"php":        php.GetLanguage(),
	"bash":       bash.GetLanguage(),
	"scala":      scala.GetLanguage(),
	"yaml":       yaml.GetLanguage(),
}

// SupportedLanguage returns true if tree-sitter can parse this language.
func SupportedLanguage(lang string) bool {
	_, ok := languages[lang]
	return ok
}

// ParseFile parses a source file and extracts symbols, imports, and refs.
func ParseFile(filePath, lang string) (*symbols.ParseResult, error) {
	tsLang, ok := languages[lang]
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}

	src, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	return ParseSource(src, filePath, lang, tsLang)
}

// ParseBytes parses source bytes (already read) and extracts symbols, imports, and refs.
// Use this when you already have the file contents to avoid a redundant ReadFile.
func ParseBytes(src []byte, filePath, lang string) (*symbols.ParseResult, error) {
	tsLang, ok := languages[lang]
	if !ok {
		return nil, fmt.Errorf("unsupported language: %s", lang)
	}
	return ParseSource(src, filePath, lang, tsLang)
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
		if nodeType == "import_spec" {
			pathNode := node.ChildByFieldName("path")
			if pathNode != nil {
				raw := strings.Trim(pathNode.Content(e.src), "\"")
				return symbols.Import{RawPath: raw, Language: e.lang}, true
			}
		}
	case "python":
		if nodeType == "import_statement" || nodeType == "import_from_statement" {
			content := node.Content(e.src)
			return symbols.Import{RawPath: content, Language: e.lang}, true
		}
	case "javascript", "typescript":
		if nodeType == "import_statement" {
			sourceNode := node.ChildByFieldName("source")
			if sourceNode != nil {
				raw := strings.Trim(sourceNode.Content(e.src), "\"'`")
				return symbols.Import{RawPath: raw, Language: e.lang}, true
			}
		}
	case "rust":
		if nodeType == "use_declaration" {
			content := node.Content(e.src)
			return symbols.Import{RawPath: content, Language: e.lang}, true
		}
	case "kotlin":
		if nodeType == "import_header" {
			content := node.Content(e.src)
			return symbols.Import{RawPath: content, Language: e.lang}, true
		}
	case "apex", "java", "scala":
		// Apex has no import statements — classes are globally available.
		// Included here so the switch case is consistent across all three extraction functions.
		if nodeType == "import_declaration" {
			content := node.Content(e.src)
			return symbols.Import{RawPath: content, Language: e.lang}, true
		}
	case "ruby":
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
	case "c", "cpp":
		if nodeType == "preproc_include" {
			pathNode := node.ChildByFieldName("path")
			if pathNode != nil {
				raw := strings.Trim(pathNode.Content(e.src), "<>\"")
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
		if nodeType == "call_expression" {
			funcNode := node.ChildByFieldName("function")
			if funcNode != nil {
				name := extractCallName(funcNode, e.src)
				if name != "" {
					return symbols.Ref{
						Name:     name,
						Line:     int(node.StartPoint().Row) + 1,
						Language: e.lang,
					}, true
				}
			}
		}
	case "python":
		if nodeType == "call" {
			funcNode := node.ChildByFieldName("function")
			if funcNode != nil {
				name := extractCallName(funcNode, e.src)
				if name != "" {
					return symbols.Ref{
						Name:     name,
						Line:     int(node.StartPoint().Row) + 1,
						Language: e.lang,
					}, true
				}
			}
		}
	case "javascript", "typescript":
		if nodeType == "call_expression" {
			funcNode := node.ChildByFieldName("function")
			if funcNode != nil {
				name := extractCallName(funcNode, e.src)
				if name != "" {
					return symbols.Ref{
						Name:     name,
						Line:     int(node.StartPoint().Row) + 1,
						Language: e.lang,
					}, true
				}
			}
		}
	case "rust":
		if nodeType == "call_expression" {
			funcNode := node.ChildByFieldName("function")
			if funcNode != nil {
				name := extractCallName(funcNode, e.src)
				if name != "" {
					return symbols.Ref{
						Name:     name,
						Line:     int(node.StartPoint().Row) + 1,
						Language: e.lang,
					}, true
				}
			}
		}
	case "kotlin":
		// Kotlin call_expression wraps navigation_expression + call_suffix.
		// Also handle simple function calls via simple_identifier.
		if nodeType == "call_expression" {
			name := extractCallName(node.Child(0), e.src)
			if name != "" {
				return symbols.Ref{
					Name:     name,
					Line:     int(node.StartPoint().Row) + 1,
					Language: e.lang,
				}, true
			}
		}
	case "apex", "java", "scala":
		if nodeType == "method_invocation" {
			nameNode := node.ChildByFieldName("name")
			if nameNode != nil {
				return symbols.Ref{
					Name:     nameNode.Content(e.src),
					Line:     int(node.StartPoint().Row) + 1,
					Language: e.lang,
				}, true
			}
		}
	case "ruby":
		if nodeType == "call" || nodeType == "method_call" {
			nameNode := node.ChildByFieldName("method")
			if nameNode != nil {
				return symbols.Ref{
					Name:     nameNode.Content(e.src),
					Line:     int(node.StartPoint().Row) + 1,
					Language: e.lang,
				}, true
			}
		}
	}
	return symbols.Ref{}, false
}

// extractCallName gets the final identifier from a call expression function node.
// For "foo.bar.Baz()", returns "Baz". For "Baz()", returns "Baz".
func extractCallName(node *sitter.Node, src []byte) string {
	content := node.Content(src)
	if dot := strings.LastIndex(content, "."); dot >= 0 {
		return content[dot+1:]
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
	if kind == "" || nameNode == nil {
		return symbols.Symbol{}, false
	}

	name := nameNode.Content(e.src)
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
	case "kotlin":
		return e.classifyKotlin(nodeType, node)
	case "apex", "java", "scala":
		return e.classifyJavaLike(nodeType, node)
	case "ruby":
		return e.classifyRuby(nodeType, node)
	case "c", "cpp":
		return e.classifyC(nodeType, node)
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

// classifyKotlin handles Kotlin's tree-sitter grammar which differs from Java:
//   - class names are type_identifier children (not "name" field)
//   - function names are simple_identifier children (not "name" field)
//   - uses object_declaration, companion_object, property_declaration
func (e *symbolExtractor) classifyKotlin(nodeType string, node *sitter.Node) (string, *sitter.Node) {
	switch nodeType {
	case "class_declaration":
		return "class", e.findChildByType(node, "type_identifier")
	case "object_declaration":
		return "class", e.findChildByType(node, "type_identifier")
	case "companion_object":
		return "class", e.findChildByType(node, "type_identifier")
	case "interface_declaration":
		return "interface", e.findChildByType(node, "type_identifier")
	case "function_declaration":
		return "function", e.findChildByType(node, "simple_identifier")
	case "property_declaration":
		return "field", e.findChildByType(node, "variable_declaration")
	case "enum_class_body":
		// Skip — enum entries are handled via class_declaration parent
	}
	return "", nil
}

// findChildByType returns the first direct child with the given node type.
func (e *symbolExtractor) findChildByType(node *sitter.Node, childType string) *sitter.Node {
	for i := 0; i < int(node.ChildCount()); i++ {
		child := node.Child(i)
		if child.Type() == childType {
			return child
		}
	}
	return nil
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
	case "function", "method":
		params := node.ChildByFieldName("parameters")
		if params != nil {
			return params.Content(e.src)
		}
	case "struct", "class", "interface", "trait":
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
