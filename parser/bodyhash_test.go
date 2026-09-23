package parser

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"
)

func TestBodyHashNormalization(t *testing.T) {
	split := func(src string) [][]byte { return bytes.Split([]byte(src), []byte("\n")) }
	base := bodyHash(split("func A() {\n\treturn\n}\n"), 1, 3)
	if decoded, err := hex.DecodeString(base); err != nil || len(decoded) != bodyHashBytes {
		t.Fatalf("hash %q is not %d hex-encoded bytes", base, bodyHashBytes)
	}
	tests := []struct {
		name       string
		src        string
		start, end int
		same       bool
	}{
		{"CRLF line endings", "func A() {\r\n\treturn\r\n}\r\n", 1, 3, true},
		{"trailing whitespace", "func A() {  \n\treturn\t\n} \n", 1, 3, true},
		{"moved down", "\n\nfunc A() {\n\treturn\n}\n", 3, 5, true},
		{"no final newline", "func A() {\n\treturn\n}", 1, 3, true},
		{"end past last line", "func A() {\n\treturn\n}", 1, 9, true},
		{"body edited", "func A() {\n\treturn 1\n}\n", 1, 3, false},
		{"indentation changed", "func A() {\n    return\n}\n", 1, 3, false},
		{"lines joined", "func A() { return }\n", 1, 1, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := bodyHash(split(tt.src), tt.start, tt.end)
			if (got == base) != tt.same {
				t.Errorf("hash %s, base %s, want same=%v", got, base, tt.same)
			}
		})
	}
}

func TestFeatureBodyHashTracksOnlyTheSymbolsOwnLines(t *testing.T) {
	hashes := func(src string) map[string]string {
		t.Helper()
		out := map[string]string{}
		for _, sym := range parseOrFail(t, []byte(src), "hash.go", "go").Symbols {
			out[sym.Name] = sym.BodyHash
		}
		return out
	}
	orig := "package p\n\nfunc Foo() int {\n\treturn 1\n}\n\nfunc Bar() int {\n\treturn 2\n}\n"
	base := hashes(orig)
	if base["Foo"] == "" || base["Bar"] == "" || base["Foo"] == base["Bar"] {
		t.Fatalf("base hashes %v", base)
	}
	tests := []struct {
		name             string
		src              string
		fooSame, barSame bool
	}{
		{"other function edited", strings.Replace(orig, "return 2", "return 3", 1), true, false},
		{"lines inserted above", strings.Replace(orig, "package p\n", "package p\n\n// Added.\nvar x = 1\n", 1), true, true},
		{"own body edited", strings.Replace(orig, "return 1", "return 10", 1), false, true},
		{"CRLF line endings", strings.ReplaceAll(orig, "\n", "\r\n"), true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hashes(tt.src)
			if (got["Foo"] == base["Foo"]) != tt.fooSame || (got["Bar"] == base["Bar"]) != tt.barSame {
				t.Errorf("Foo %s -> %s (want same=%v), Bar %s -> %s (want same=%v)",
					base["Foo"], got["Foo"], tt.fooSame, base["Bar"], got["Bar"], tt.barSame)
			}
		})
	}
}

func TestFeatureBodyHashIncludesModifiersBeforeTheStartColumn(t *testing.T) {
	// Swift anchors a declaration's start at its keyword, after modifiers on
	// the same line. The hash covers the whole line, so a visibility change
	// still counts as a change.
	hash := func(src string) string {
		t.Helper()
		sym := findSymbol(parseOrFail(t, []byte(src), "greet.swift", "swift").Symbols, "greet")
		if sym == nil {
			t.Fatalf("greet not found in %q", src)
		}
		return sym.BodyHash
	}
	if hash("public func greet() {}\n") == hash("private func greet() {}\n") {
		t.Error("changing public to private left the hash unchanged")
	}
}
