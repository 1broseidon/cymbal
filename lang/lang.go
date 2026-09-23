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
	"bytes"
	"path"
	"path/filepath"
	"sort"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// Language defines a supported language and its properties.
type Language struct {
	// Name is the canonical language key (e.g. "go", "cpp").
	Name string

	// Extensions lists file extensions including the dot (e.g. ".go", ".cpp").
	Extensions []string

	// Filenames lists special filenames without extension (e.g. "Makefile").
	Filenames []string

	// Interpreters lists program names that identify this language in a `#!`
	// line (e.g. "bash", "python"), for extensionless scripts such as
	// commands installed on PATH. A trailing version is ignored on lookup, so
	// "python" also covers "python3" and "python3.12".
	Interpreters []string

	// TreeSitter is the tree-sitter grammar for this language.
	// Nil means the language is recognized for file classification / CLI
	// heuristics only and is not parseable for symbol indexing.
	TreeSitter *sitter.Language

	// Family groups languages that interoperate — calls cross language
	// boundaries within the group (e.g. JVM java/kotlin/scala, JS
	// javascript/typescript/tsx, C c/cpp). Empty means the language has no
	// known interop family and scopes to itself. Used by name-based
	// resolution (trace/impact) to widen "same language" to "same family"
	// without admitting unrelated cross-language name collisions.
	Family string
}

// Parseable returns true if this language has a tree-sitter grammar.
func (l *Language) Parseable() bool {
	return l.TreeSitter != nil
}

// Registry holds all known languages, indexed for fast lookup.
type Registry struct {
	langs    []*Language
	byName   map[string]*Language
	byExt    map[string]*Language
	byFile   map[string]*Language
	byInterp map[string]*Language
	byFamily map[string][]string // family key -> member language names (sorted)
}

// NewRegistry builds a registry from the given language definitions.
// It panics on duplicate names, extensions, or filenames.
func NewRegistry(langs ...Language) *Registry {
	r := &Registry{
		byName:   make(map[string]*Language, len(langs)),
		byExt:    make(map[string]*Language, len(langs)*3),
		byFile:   make(map[string]*Language, 8),
		byInterp: make(map[string]*Language, 16),
		byFamily: make(map[string][]string, 4),
	}
	for i := range langs {
		l := &langs[i]
		if _, dup := r.byName[l.Name]; dup {
			panic("lang: duplicate language name: " + l.Name)
		}
		r.byName[l.Name] = l
		r.langs = append(r.langs, l)

		if l.Family != "" {
			r.byFamily[l.Family] = append(r.byFamily[l.Family], l.Name)
		}

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
		for _, in := range l.Interpreters {
			if _, dup := r.byInterp[in]; dup {
				panic("lang: duplicate interpreter: " + in)
			}
			r.byInterp[in] = l
		}
	}
	for fam := range r.byFamily {
		sort.Strings(r.byFamily[fam])
	}
	return r
}

// Family returns the language names that interoperate with name, including
// name itself, sorted. Languages with no declared interop family — and names
// not in the registry — return just [name]. Only an empty name returns nil.
func (r *Registry) Family(name string) []string {
	if name == "" {
		return nil
	}
	l, ok := r.byName[name]
	if !ok {
		return []string{name}
	}
	if l.Family == "" {
		return []string{name}
	}
	members := r.byFamily[l.Family]
	out := make([]string, len(members))
	copy(out, members)
	return out
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

// ShebangMaxBytes is how much of the start of a file ForShebang needs to see.
const ShebangMaxBytes = 256

// ForShebang returns the language named by the `#!` line at the start of head,
// or nil if there is none or its interpreter is unrecognized. It is the
// fallback for files ForFile cannot classify by name: scripts installed as
// commands conventionally have no extension, so the `#!` line is the only
// record of their language.
func (r *Registry) ForShebang(head []byte) *Language {
	name := shebangInterpreter(head)
	if name == "" {
		return nil
	}
	if l, ok := r.byInterp[name]; ok {
		return l
	}
	if base := strings.TrimRight(name, "0123456789."); base != name && !unversioned[name] {
		return r.byInterp[base]
	}
	return nil
}

// unversioned are interpreter names whose trailing digits are not a version of
// the name before them: perl6 is Raku, a different language from Perl.
var unversioned = map[string]bool{"perl6": true}

// envArgFlags are the env(1) options whose value is a separate word, so the
// word after them is not the interpreter.
var envArgFlags = map[string]bool{
	"-u": true, "--unset": true,
	"-C": true, "--chdir": true,
	"-a": true, "--argv0": true,
	"-f": true, "--file": true,
	"-P": true,
}

// shebangInterpreter returns the base name of the program a `#!` line runs,
// looking through `/usr/bin/env [options] [NAME=value...] prog`.
func shebangInterpreter(head []byte) string {
	if !bytes.HasPrefix(head, []byte("#!")) {
		return ""
	}
	line := head[2:]
	if i := bytes.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	fields := strings.Fields(string(line))
	if len(fields) == 0 {
		return ""
	}
	prog := path.Base(fields[0])
	if prog != "env" {
		return prog
	}
	for i := 1; i < len(fields); i++ {
		f := fields[i]
		switch {
		case envArgFlags[f]:
			i++
		case strings.HasPrefix(f, "-S") && len(f) > 2:
			return path.Base(f[2:]) // -Sprog: env splits the rest itself
		case strings.HasPrefix(f, "--split-string="):
			return path.Base(strings.TrimPrefix(f, "--split-string="))
		case strings.HasPrefix(f, "-"), strings.Contains(f, "="):
		default:
			return path.Base(f)
		}
	}
	return ""
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
