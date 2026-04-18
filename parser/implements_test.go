package parser

import (
	"testing"

	"github.com/1broseidon/cymbal/lang"
	"github.com/1broseidon/cymbal/symbols"
)

// implementsTargets returns the Name values of refs whose Kind is "implements".
func implementsTargets(refs []symbols.Ref) []string {
	var out []string
	for _, r := range refs {
		if r.Kind == symbols.RefKindImplements {
			out = append(out, r.Name)
		}
	}
	return out
}

func hasTarget(targets []string, want string) bool {
	for _, t := range targets {
		if t == want {
			return true
		}
	}
	return false
}

func parseOrFail(t *testing.T, src []byte, path, language string) *symbols.ParseResult {
	t.Helper()
	tsLang := lang.Default.TreeSitter(language)
	if tsLang == nil {
		t.Skipf("language %q not compiled in", language)
	}
	result, err := ParseSource(src, path, language, tsLang)
	if err != nil {
		t.Fatalf("parse %s: %v", language, err)
	}
	return result
}

// ---------------------------------------------------------------------------
// Ref kind classification (regression: trace must default to call-only)
// ---------------------------------------------------------------------------

func TestRefKindsTaggedForGoCalls(t *testing.T) {
	src := []byte(`package main

import "fmt"

func Greet(name string) {
	fmt.Println("hello", name)
}
`)
	result := parseOrFail(t, src, "t.go", "go")
	var sawCall bool
	for _, r := range result.Refs {
		if r.Name == "Println" && r.Kind == symbols.RefKindCall {
			sawCall = true
		}
		if r.Kind == "" {
			t.Errorf("ref %q has empty Kind", r.Name)
		}
	}
	if !sawCall {
		t.Errorf("expected Println to be tagged as a call ref; got %+v", result.Refs)
	}
}

// ---------------------------------------------------------------------------
// Swift (the motivating case)
// ---------------------------------------------------------------------------

func TestImplementsSwiftProtocolConformance(t *testing.T) {
	src := []byte(`import Foundation

class TimerActivityIntent: LiveActivityIntent, Sendable {
}

protocol Named: Identifiable {
}
`)
	result := parseOrFail(t, src, "Timer.swift", "swift")
	targets := implementsTargets(result.Refs)

	for _, want := range []string{"LiveActivityIntent", "Sendable", "Identifiable"} {
		if !hasTarget(targets, want) {
			t.Errorf("expected Swift implements edge to %q; got %v", want, targets)
		}
	}
}

// Type mentions (annotations/generics) should NOT be tagged as calls. If they
// were, `trace` would surface them as callees — that's exactly the Swift noise
// the feature is meant to eliminate.
func TestSwiftTypeMentionNotTaggedAsCall(t *testing.T) {
	src := []byte(`import Foundation

func load(id: UUID) -> Date? {
    return nil
}
`)
	result := parseOrFail(t, src, "x.swift", "swift")
	for _, r := range result.Refs {
		if (r.Name == "UUID" || r.Name == "Date") && r.Kind == symbols.RefKindCall {
			t.Errorf("type mention %q should not be Kind=call (got %+v)", r.Name, r)
		}
	}
}

// ---------------------------------------------------------------------------
// Go interface embedding
// ---------------------------------------------------------------------------

func TestImplementsGoInterfaceEmbedding(t *testing.T) {
	src := []byte(`package io

type ReadWriter interface {
	Reader
	Writer
}
`)
	result := parseOrFail(t, src, "io.go", "go")
	targets := implementsTargets(result.Refs)
	for _, want := range []string{"Reader", "Writer"} {
		if !hasTarget(targets, want) {
			t.Errorf("expected Go implements edge to %q; got %v", want, targets)
		}
	}
}

// ---------------------------------------------------------------------------
// Java
// ---------------------------------------------------------------------------

func TestImplementsJava(t *testing.T) {
	src := []byte(`package x;

public class MyRunner extends BaseRunner implements Runnable, AutoCloseable {
}
`)
	result := parseOrFail(t, src, "MyRunner.java", "java")
	targets := implementsTargets(result.Refs)
	for _, want := range []string{"BaseRunner", "Runnable", "AutoCloseable"} {
		if !hasTarget(targets, want) {
			t.Errorf("expected Java implements edge to %q; got %v", want, targets)
		}
	}
}

// ---------------------------------------------------------------------------
// C#
// ---------------------------------------------------------------------------

func TestImplementsCSharp(t *testing.T) {
	src := []byte(`namespace X;

public class UserRepo : BaseRepo, IUserRepository, IDisposable {
}
`)
	result := parseOrFail(t, src, "UserRepo.cs", "csharp")
	targets := implementsTargets(result.Refs)
	for _, want := range []string{"BaseRepo", "IUserRepository", "IDisposable"} {
		if !hasTarget(targets, want) {
			t.Errorf("expected C# implements edge to %q; got %v", want, targets)
		}
	}
}

// ---------------------------------------------------------------------------
// TypeScript
// ---------------------------------------------------------------------------

func TestImplementsTypeScript(t *testing.T) {
	src := []byte(`class UserRepo extends BaseRepo implements IUserRepository, Serializable {
}

interface Named extends Identifiable {
}
`)
	result := parseOrFail(t, src, "repo.ts", "typescript")
	targets := implementsTargets(result.Refs)
	for _, want := range []string{"BaseRepo", "IUserRepository", "Serializable", "Identifiable"} {
		if !hasTarget(targets, want) {
			t.Errorf("expected TS implements edge to %q; got %v", want, targets)
		}
	}
}

// ---------------------------------------------------------------------------
// Rust
// ---------------------------------------------------------------------------

func TestImplementsRust(t *testing.T) {
	src := []byte(`struct Cache;

impl Reader for Cache {
}
`)
	result := parseOrFail(t, src, "c.rs", "rust")
	targets := implementsTargets(result.Refs)
	if !hasTarget(targets, "Reader") {
		t.Errorf("expected Rust implements edge to Reader; got %v", targets)
	}
}

// ---------------------------------------------------------------------------
// Kotlin
// ---------------------------------------------------------------------------

func TestImplementsKotlin(t *testing.T) {
	src := []byte(`class UserRepo : BaseRepo(), IUserRepository, AutoCloseable {
}
`)
	result := parseOrFail(t, src, "repo.kt", "kotlin")
	targets := implementsTargets(result.Refs)
	for _, want := range []string{"BaseRepo", "IUserRepository", "AutoCloseable"} {
		if !hasTarget(targets, want) {
			t.Errorf("expected Kotlin implements edge to %q; got %v", want, targets)
		}
	}
}

// ---------------------------------------------------------------------------
// Python
// ---------------------------------------------------------------------------

func TestImplementsPython(t *testing.T) {
	src := []byte(`class Cache(BaseCache, ContextManager):
    pass
`)
	result := parseOrFail(t, src, "cache.py", "python")
	targets := implementsTargets(result.Refs)
	for _, want := range []string{"BaseCache", "ContextManager"} {
		if !hasTarget(targets, want) {
			t.Errorf("expected Python implements edge to %q; got %v", want, targets)
		}
	}
}

// Regression: a qualified base like `routing.Router` must resolve to the
// final segment "Router", not the module prefix "routing". Same shape covers
// TS nested_identifier, Java scoped_type_identifier, C# qualified_name, etc.
func TestImplementsQualifiedBasePicksFinalSegment(t *testing.T) {
	src := []byte(`import routing

class APIRouter(routing.Router):
    pass
`)
	result := parseOrFail(t, src, "router.py", "python")
	targets := implementsTargets(result.Refs)
	if !hasTarget(targets, "Router") {
		t.Errorf("expected Python implements edge to final segment 'Router'; got %v", targets)
	}
	if hasTarget(targets, "routing") {
		t.Errorf("implements target should not be the module prefix 'routing'; got %v", targets)
	}
}
