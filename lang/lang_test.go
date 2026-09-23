package lang

import (
	"testing"
)

func TestDefaultRegistryNoPanic(t *testing.T) {
	// Default is built at init time; if we get here it didn't panic.
	if Default == nil {
		t.Fatal("Default registry is nil")
	}
}

func TestForFileExtensions(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		// Existing extensions
		{"main.go", "go"},
		{"app.py", "python"},
		{"index.js", "javascript"},
		{"App.jsx", "javascript"},
		{"index.ts", "typescript"},
		{"App.tsx", "tsx"},
		{"lib.rs", "rust"},
		{"app.rb", "ruby"},
		{"Main.java", "java"},
		{"foo.c", "c"},
		{"foo.h", "c"},
		{"foo.cpp", "cpp"},
		{"foo.cc", "cpp"},
		{"foo.hpp", "cpp"},
		{"Account.cls", "apex"},
		{"MyTrigger.trigger", "apex"},
		{"Program.cs", "csharp"},
		{"main.dart", "dart"},
		{"App.swift", "swift"},
		{"Main.kt", "kotlin"},
		{"script.lua", "lua"},
		{"index.php", "php"},
		{"run.sh", "bash"},
		{"run.bash", "bash"},
		{"run.zsh", "bash"},
		{"Main.scala", "scala"},
		{"config.yaml", "yaml"},
		{"config.yml", "yaml"},
		{"mix.ex", "elixir"},
		{"test.exs", "elixir"},
		{"main.tf", "hcl"},
		{"main.hcl", "hcl"},
		{"schema.proto", "protobuf"},

		// Issue #19 additions
		{"foo.cxx", "cpp"},
		{"foo.hxx", "cpp"},
		{"foo.hh", "cpp"},
		{"module.mjs", "javascript"},
		{"module.cjs", "javascript"},
		{"module.mts", "typescript"},
		{"module.cts", "typescript"},
		{"script.pyw", "python"},
		{"build.kts", "kotlin"},
		{"tasks.rake", "ruby"},
		{"mygem.gemspec", "ruby"},
		{"worksheet.sc", "scala"},
		{"vars.tfvars", "hcl"},

		// Recognition-only (no tree-sitter)
		{"main.zig", "zig"},
		{"config.toml", "toml"},
		{"data.json", "json"},
		{"README.md", "markdown"},
		{"query.sql", "sql"},
		{"module.erl", "erlang"},
		{"Main.hs", "haskell"},
		{"parser.ml", "ocaml"},
		{"parser.mli", "ocaml"},
		{"analysis.r", "r"},
		{"analysis.R", "r"},
		{"script.pl", "perl"},
		{"script.pm", "perl"},
		{"App.vue", "vue"},
		{"App.svelte", "svelte"},

		// Unrecognized
		{"foo.xyz", ""},
		{"foo.txt", ""},
		{"foo", ""},
	}

	for _, tt := range tests {
		got := Default.LangForFile(tt.path)
		if got != tt.want {
			t.Errorf("LangForFile(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestForFileSpecialFilenames(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"Makefile", "make"},
		{"makefile", "make"},
		{"GNUmakefile", "make"},
		{"Dockerfile", "dockerfile"},
		{"Jenkinsfile", "groovy"},
		{"CMakeLists.txt", "cmake"},
		{"src/Makefile", "make"},
		{"docker/Dockerfile", "dockerfile"},
	}

	for _, tt := range tests {
		got := Default.LangForFile(tt.path)
		if got != tt.want {
			t.Errorf("LangForFile(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestSupported(t *testing.T) {
	// Languages with tree-sitter grammars
	for _, name := range []string{"go", "python", "javascript", "typescript", "tsx", "rust", "ruby", "java", "c", "cpp", "csharp", "dart", "swift", "kotlin", "lua", "php", "bash", "scala", "yaml", "elixir", "hcl", "protobuf"} {
		if !Default.Supported(name) {
			t.Errorf("Supported(%q) = false, want true", name)
		}
	}

	// Recognition-only languages should NOT be "supported" (no parser)
	for _, name := range []string{"apex", "zig", "toml", "json", "markdown", "sql", "erlang", "haskell", "ocaml", "r", "perl", "vue", "svelte", "make", "dockerfile", "groovy", "cmake"} {
		if Default.Supported(name) {
			t.Errorf("Supported(%q) = true, want false (no tree-sitter grammar)", name)
		}
	}

	// Unknown language
	if Default.Supported("brainfuck") {
		t.Error("Supported(brainfuck) = true, want false")
	}
}

func TestTreeSitter(t *testing.T) {
	if Default.TreeSitter("go") == nil {
		t.Error("TreeSitter(go) = nil, want non-nil")
	}
	if Default.TreeSitter("make") != nil {
		t.Error("TreeSitter(make) != nil, want nil")
	}
	if Default.TreeSitter("unknown") != nil {
		t.Error("TreeSitter(unknown) != nil, want nil")
	}
}

func TestKnown(t *testing.T) {
	if !Default.Known("go") {
		t.Error("Known(go) = false, want true")
	}
	if !Default.Known("make") {
		t.Error("Known(make) = false, want true")
	}
	if Default.Known("brainfuck") {
		t.Error("Known(brainfuck) = true, want false")
	}
}

func TestAll(t *testing.T) {
	all := Default.All()
	if len(all) == 0 {
		t.Fatal("All() returned empty")
	}

	// Verify it's a copy
	all[0] = nil
	if Default.All()[0] == nil {
		t.Error("All() returned a reference to internal slice, not a copy")
	}
}

func TestConsistency_ParseableLanguagesHaveTreeSitter(t *testing.T) {
	for _, l := range Default.All() {
		if l.Parseable() && l.TreeSitter == nil {
			t.Errorf("language %q: Parseable() is true but TreeSitter is nil", l.Name)
		}
		if !l.Parseable() && l.TreeSitter != nil {
			t.Errorf("language %q: Parseable() is false but TreeSitter is non-nil", l.Name)
		}
	}
}

func TestConsistency_AllExtensionsResolvable(t *testing.T) {
	for _, l := range Default.All() {
		for _, ext := range l.Extensions {
			got := Default.ForFile("test" + ext)
			if got != l {
				t.Errorf("extension %q should resolve to %q, got %v", ext, l.Name, got)
			}
		}
		for _, fn := range l.Filenames {
			got := Default.ForFile(fn)
			if got != l {
				t.Errorf("filename %q should resolve to %q, got %v", fn, l.Name, got)
			}
		}
	}
}

func TestDuplicateNamePanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for duplicate name")
		}
	}()
	NewRegistry(
		Language{Name: "go", Extensions: []string{".go"}},
		Language{Name: "go", Extensions: []string{".go2"}},
	)
}

func TestDuplicateExtensionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for duplicate extension")
		}
	}()
	NewRegistry(
		Language{Name: "lang1", Extensions: []string{".x"}},
		Language{Name: "lang2", Extensions: []string{".x"}},
	)
}

func TestBadExtensionPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic for extension without dot")
		}
	}()
	NewRegistry(
		Language{Name: "bad", Extensions: []string{"nodot"}},
	)
}

func TestFamily(t *testing.T) {
	eq := func(got, want []string) bool {
		if len(got) != len(want) {
			return false
		}
		for i := range got {
			if got[i] != want[i] {
				return false
			}
		}
		return true
	}

	tests := []struct {
		name string
		want []string // sorted
	}{
		// Interop families (members returned sorted, including the queried name).
		{"java", []string{"java", "kotlin", "scala"}},
		{"kotlin", []string{"java", "kotlin", "scala"}},
		{"scala", []string{"java", "kotlin", "scala"}},
		{"javascript", []string{"javascript", "tsx", "typescript"}},
		{"typescript", []string{"javascript", "tsx", "typescript"}},
		{"tsx", []string{"javascript", "tsx", "typescript"}},
		{"c", []string{"c", "cpp"}},
		{"cpp", []string{"c", "cpp"}},
		// No declared family: scopes to itself.
		{"go", []string{"go"}},
		{"python", []string{"python"}},
		{"csharp", []string{"csharp"}},
		// Unknown name: itself; empty: nil.
		{"madeuplang", []string{"madeuplang"}},
		{"", nil},
	}
	for _, tt := range tests {
		got := Default.Family(tt.name)
		if !eq(got, tt.want) {
			t.Errorf("Family(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestForShebang(t *testing.T) {
	tests := []struct {
		head string
		want string // "" = nil
	}{
		{"#!/bin/bash\nset -e\n", "bash"},
		{"#!/bin/sh\n", "bash"},
		{"#! /bin/zsh -f\n", "bash"},
		{"#!/usr/bin/env bash\n", "bash"},
		{"#!/usr/bin/env python3\n", "python"},
		{"#!/usr/bin/python3.12 -u\n", "python"},
		{"#!/usr/bin/env -S python3 -u\n", "python"},
		{"#!/usr/bin/env -Spython3 -u\n", "python"},
		{"#!/usr/bin/env -u HOME -i node\n", "javascript"},
		{"#!/usr/bin/env FOO=1 ruby\n", "ruby"},
		{"#!/usr/bin/env jruby\n", "ruby"},
		{"#!/usr/bin/perl -w\n", "perl"},
		{"#!/bin/bash\r\n", "bash"},    // CRLF checkout
		{"#!/bin/bash", "bash"},        // no trailing newline
		{"#!/usr/bin/env fish\n", ""},  // unknown interpreter
		{"#!/usr/bin/env\n", ""},       // env with no program
		{"#!\n", ""},                   // empty line
		{"echo hi\n#!/bin/bash\n", ""}, // not at the start
		{" #!/bin/bash\n", ""},         // kernel requires column 0
		{"", ""},
		{"#!/bin/python-config\n", ""}, // not a trailing version
		{"#!/usr/bin/perl6\n", ""},     // Raku, not a Perl version
		{"#!/usr/bin/env -C /tmp -P /bin python3\n", "python"},
		{"#!/usr/bin/env -a myname --argv0 x ruby\n", "ruby"},
		{"#!/usr/bin/env -f f --file g node\n", "javascript"},
		{"#!/usr/bin/env --split-string=python3 -u\n", "python"},
		{"#!/usr/bin/env --split-string python3\n", "python"},
		{"#!/usr/bin/pypy3\n", "python"},
		{"#!/usr/bin/env ts-node\n", "typescript"},
		{"#!/usr/bin/env nodejs\n", "javascript"},
		{"#!/usr/bin/env luajit\n", "lua"},
		{"#!/usr/bin/env elixir\n", "elixir"},
		{"#!/usr/bin/php\n", "php"},
	}
	for _, tt := range tests {
		got := ""
		if l := Default.ForShebang([]byte(tt.head)); l != nil {
			got = l.Name
		}
		if got != tt.want {
			t.Errorf("ForShebang(%q) = %q, want %q", tt.head, got, tt.want)
		}
	}
}

func TestNewRegistryPanicsOnDuplicateInterpreter(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate interpreter")
		}
	}()
	NewRegistry(
		Language{Name: "a", Interpreters: []string{"x"}},
		Language{Name: "b", Interpreters: []string{"x"}},
	)
}
