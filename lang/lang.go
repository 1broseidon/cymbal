// Package lang provides a unified language registry for cymbal.
//
// It is the single source of truth for language names, file extensions,
// special filenames, and tree-sitter grammar availability. Both the
// walker (file discovery) and parser (AST extraction) derive their
// behavior from this registry.
//
// The registry intentionally models both:
//   - recognized languages: files cymbal can classify by name/extension
//   - supported languages: files cymbal can parse/index via tree-sitter
//
// Callers that need the parseable subset (for example indexing) should use
// Registry.Supported or Language.Parseable instead of assuming every known
// language can be parsed.
package lang

import (
	"path/filepath"
	"strings"

	sitter "github.com/smacker/go-tree-sitter"
)

// Language defines a supported language and its properties.
type Language struct {
	// Name is the canonical language key (e.g. "go", "cpp").
	Name string

	// Extensions lists file extensions including the dot (e.g. ".go", ".cpp").
	Extensions []string

	// Filenames lists special filenames without extension (e.g. "Makefile").
	Filenames []string

	// TreeSitter is the tree-sitter grammar for this language.
	// Nil means the language is recognized for file classification / CLI
	// heuristics only and is not parseable for symbol indexing.
	TreeSitter *sitter.Language
}

// Parseable returns true if this language has a tree-sitter grammar.
func (l *Language) Parseable() bool {
	return l.TreeSitter != nil
}

// Registry holds all known languages, indexed for fast lookup.
type Registry struct {
	langs  []*Language
	byName map[string]*Language
	byExt  map[string]*Language
	byFile map[string]*Language
}

// NewRegistry builds a registry from the given language definitions.
// It panics on duplicate names, extensions, or filenames.
func NewRegistry(langs ...Language) *Registry {
	r := &Registry{
		byName: make(map[string]*Language, len(langs)),
		byExt:  make(map[string]*Language, len(langs)*3),
		byFile: make(map[string]*Language, 8),
	}
	for i := range langs {
		l := &langs[i]
		if _, dup := r.byName[l.Name]; dup {
			panic("lang: duplicate language name: " + l.Name)
		}
		r.byName[l.Name] = l
		r.langs = append(r.langs, l)

		for _, ext := range l.Extensions {
			if !strings.HasPrefix(ext, ".") {
				panic("lang: extension must start with dot: " + ext)
			}
			if _, dup := r.byExt[ext]; dup {
				panic("lang: duplicate extension: " + ext)
			}
			r.byExt[ext] = l
		}
		for _, fn := range l.Filenames {
			if _, dup := r.byFile[fn]; dup {
				panic("lang: duplicate filename: " + fn)
			}
			r.byFile[fn] = l
		}
	}
	return r
}

// ForFile returns the language for a file path, or nil if unrecognized.
// Extension matches take precedence over special-filename matches.
func (r *Registry) ForFile(path string) *Language {
	ext := filepath.Ext(path)
	if l, ok := r.byExt[ext]; ok {
		return l
	}
	base := filepath.Base(path)
	if l, ok := r.byFile[base]; ok {
		return l
	}
	return nil
}

// LangForFile returns the language name for a file path, or "" if unrecognized.
// This is a convenience wrapper matching the old walker.LangForFile signature.
func (r *Registry) LangForFile(path string) string {
	if l := r.ForFile(path); l != nil {
		return l.Name
	}
	return ""
}

// Supported returns true if the named language has a tree-sitter grammar.
// Indexing and parsing code should use this to select the parseable subset
// of known languages.
func (r *Registry) Supported(name string) bool {
	l, ok := r.byName[name]
	return ok && l.TreeSitter != nil
}

// TreeSitter returns the tree-sitter grammar for the named language, or nil.
func (r *Registry) TreeSitter(name string) *sitter.Language {
	if l, ok := r.byName[name]; ok {
		return l.TreeSitter
	}
	return nil
}

// Known returns true if the language name is in the registry (even without a parser).
func (r *Registry) Known(name string) bool {
	_, ok := r.byName[name]
	return ok
}

// All returns all registered languages.
func (r *Registry) All() []*Language {
	out := make([]*Language, len(r.langs))
	copy(out, r.langs)
	return out
}
