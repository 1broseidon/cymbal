package symbols

// Symbol represents a parsed code symbol.
type Symbol struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	StartCol  int    `json:"start_col,omitempty"`
	EndCol    int    `json:"end_col,omitempty"`
	Parent    string `json:"parent,omitempty"`
	Depth     int    `json:"depth"`
	Signature string `json:"signature,omitempty"`
	Language  string `json:"language"`
}

// Import represents an import/use statement found in source.
type Import struct {
	RawPath  string `json:"raw_path"`
	Language string `json:"language"`
}

// Ref kind constants. These classify the edge from the enclosing symbol
// to the referenced name. Kinds are coarse on purpose so every language can
// emit them in a best-effort way without type resolution.
const (
	// RefKindCall is a function/method invocation or constructor call.
	RefKindCall = "call"
	// RefKindImplements is an inheritance/conformance edge from a declaring
	// type to a supertype, interface, protocol, trait, or embedded interface.
	RefKindImplements = "implements"
	// RefKindUse is the catch-all for identifier usages that aren't calls
	// or implements edges (type mentions, composite literal types, etc.).
	RefKindUse = "use"
)

// Ref represents a reference to an identifier (call expression, usage).
type Ref struct {
	Name     string `json:"name"`
	Line     int    `json:"line"`
	Language string `json:"language"`
	// Kind classifies the edge. One of RefKindCall, RefKindImplements, RefKindUse.
	// Empty is treated as RefKindUse by the store.
	Kind string `json:"kind,omitempty"`
}

// ParseResult holds all extracted data from a file.
type ParseResult struct {
	Symbols []Symbol
	Imports []Import
	Refs    []Ref
}
