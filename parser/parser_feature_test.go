package parser

import (
	"testing"

	"github.com/1broseidon/cymbal/lang"
	"github.com/1broseidon/cymbal/symbols"
)

// --- Helpers ---

func findSymbol(syms []symbols.Symbol, name string) *symbols.Symbol {
	for i := range syms {
		if syms[i].Name == name {
			return &syms[i]
		}
	}
	return nil
}

func findSymbolKind(syms []symbols.Symbol, name, kind string) *symbols.Symbol {
	for i := range syms {
		if syms[i].Name == name && syms[i].Kind == kind {
			return &syms[i]
		}
	}
	return nil
}

func findImport(imports []symbols.Import, substring string) *symbols.Import {
	for i := range imports {
		if imports[i].RawPath == substring || contains(imports[i].RawPath, substring) {
			return &imports[i]
		}
	}
	return nil
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && len(sub) > 0 && containsStr(s, sub)))
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func findRef(refs []symbols.Ref, name string) *symbols.Ref {
	for i := range refs {
		if refs[i].Name == name {
			return &refs[i]
		}
	}
	return nil
}

func debugParseResult(t *testing.T, result *symbols.ParseResult) {
	t.Helper()
	t.Log("=== All symbols ===")
	for _, s := range result.Symbols {
		t.Logf("  %s (%s) parent=%q lines=%d-%d",
			s.Name, s.Kind, s.Parent, s.StartLine, s.EndLine)
	}
	t.Log("=== All imports ===")
	for _, imp := range result.Imports {
		t.Logf("  %s", imp.RawPath)
	}
	t.Log("=== All refs ===")
	for _, ref := range result.Refs {
		t.Logf("  %s (line %d)", ref.Name, ref.Line)
	}
}

// --- Go Language Feature Tests ---

func TestFeatureGoFunctions(t *testing.T) {
	src := []byte(`package main

func Hello(name string) string {
	return "Hello, " + name
}

func Add(a, b int) int {
	return a + b
}
`)
	result, err := ParseSource(src, "test.go", "go", lang.Default.TreeSitter("go"))
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Symbols) != 2 {
		t.Fatalf("expected 2 symbols, got %d", len(result.Symbols))
	}

	hello := findSymbol(result.Symbols, "Hello")
	if hello == nil {
		t.Fatal("expected to find Hello function")
	}
	if hello.Kind != "function" {
		t.Errorf("expected kind 'function', got %q", hello.Kind)
	}
	if hello.Language != "go" {
		t.Errorf("expected language 'go', got %q", hello.Language)
	}

	add := findSymbol(result.Symbols, "Add")
	if add == nil {
		t.Fatal("expected to find Add function")
	}
	if add.Kind != "function" {
		t.Errorf("expected kind 'function', got %q", add.Kind)
	}
}

func TestFeatureGoMethods(t *testing.T) {
	src := []byte(`package main

type Server struct {
	Port int
}

func (s *Server) Start() error {
	return nil
}

func (s *Server) Stop() {
}
`)
	result, err := ParseSource(src, "test.go", "go", lang.Default.TreeSitter("go"))
	if err != nil {
		t.Fatal(err)
	}

	srv := findSymbolKind(result.Symbols, "Server", "struct")
	if srv == nil {
		t.Fatal("expected to find Server struct")
	}

	start := findSymbolKind(result.Symbols, "Start", "method")
	if start == nil {
		t.Fatal("expected to find Start method")
	}

	stop := findSymbolKind(result.Symbols, "Stop", "method")
	if stop == nil {
		t.Fatal("expected to find Stop method")
	}
}

func TestFeatureGoTypes(t *testing.T) {
	src := []byte(`package main

type Config struct {
	Host string
	Port int
}

type Handler interface {
	ServeHTTP(w Writer, r *Request)
}

type Duration int64
`)
	result, err := ParseSource(src, "test.go", "go", lang.Default.TreeSitter("go"))
	if err != nil {
		t.Fatal(err)
	}

	config := findSymbol(result.Symbols, "Config")
	if config == nil || config.Kind != "struct" {
		t.Fatalf("expected Config struct, got %v", config)
	}

	handler := findSymbol(result.Symbols, "Handler")
	if handler == nil || handler.Kind != "interface" {
		t.Fatalf("expected Handler interface, got %v", handler)
	}

	dur := findSymbol(result.Symbols, "Duration")
	if dur == nil || dur.Kind != "type" {
		t.Fatalf("expected Duration type alias, got %v", dur)
	}
}

func TestFeatureGoConstants(t *testing.T) {
	src := []byte(`package main

const MaxRetries = 3

const (
	StatusOK    = 200
	StatusError = 500
)
`)
	result, err := ParseSource(src, "test.go", "go", lang.Default.TreeSitter("go"))
	if err != nil {
		t.Fatal(err)
	}

	maxRetries := findSymbolKind(result.Symbols, "MaxRetries", "constant")
	if maxRetries == nil {
		t.Fatal("expected to find MaxRetries constant")
	}

	statusOK := findSymbolKind(result.Symbols, "StatusOK", "constant")
	if statusOK == nil {
		t.Fatal("expected to find StatusOK constant")
	}

	statusErr := findSymbolKind(result.Symbols, "StatusError", "constant")
	if statusErr == nil {
		t.Fatal("expected to find StatusError constant")
	}
}

func TestFeatureGoImports(t *testing.T) {
	src := []byte(`package main

import (
	"fmt"
	"net/http"
	"encoding/json"
)

func main() {
	fmt.Println("hello")
}
`)
	result, err := ParseSource(src, "test.go", "go", lang.Default.TreeSitter("go"))
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Imports) != 3 {
		t.Fatalf("expected 3 imports, got %d", len(result.Imports))
	}

	fmtImp := findImport(result.Imports, "fmt")
	if fmtImp == nil {
		t.Fatal("expected to find fmt import")
	}

	httpImp := findImport(result.Imports, "net/http")
	if httpImp == nil {
		t.Fatal("expected to find net/http import")
	}
}

func TestFeatureGoRefs(t *testing.T) {
	src := []byte(`package main

import "fmt"

func greet(name string) {
	fmt.Println(name)
}

func main() {
	greet("world")
}
`)
	result, err := ParseSource(src, "test.go", "go", lang.Default.TreeSitter("go"))
	if err != nil {
		t.Fatal(err)
	}

	printlnRef := findRef(result.Refs, "Println")
	if printlnRef == nil {
		t.Fatal("expected to find Println ref")
	}

	greetRef := findRef(result.Refs, "greet")
	if greetRef == nil {
		t.Fatal("expected to find greet ref")
	}
}

func TestFeatureGoSignature(t *testing.T) {
	src := []byte(`package main

func Calculate(x int, y int) int {
	return x + y
}
`)
	result, err := ParseSource(src, "test.go", "go", lang.Default.TreeSitter("go"))
	if err != nil {
		t.Fatal(err)
	}

	calc := findSymbol(result.Symbols, "Calculate")
	if calc == nil {
		t.Fatal("expected to find Calculate")
	}
	if calc.Signature == "" {
		t.Error("expected non-empty signature for function")
	}
	// Signature should contain parameter info
	if !containsStr(calc.Signature, "x int") {
		t.Errorf("expected signature to contain 'x int', got %q", calc.Signature)
	}
}

// --- Python Language Feature Tests ---

func TestFeaturePythonFunctions(t *testing.T) {
	src := []byte(`def greet(name):
    return f"Hello, {name}"

def calculate(a, b):
    return a + b
`)
	result, err := ParseSource(src, "test.py", "python", lang.Default.TreeSitter("python"))
	if err != nil {
		t.Fatal(err)
	}

	greet := findSymbol(result.Symbols, "greet")
	if greet == nil || greet.Kind != "function" {
		t.Fatalf("expected greet function, got %v", greet)
	}

	calc := findSymbol(result.Symbols, "calculate")
	if calc == nil || calc.Kind != "function" {
		t.Fatalf("expected calculate function, got %v", calc)
	}
}

func TestFeaturePythonClasses(t *testing.T) {
	src := []byte(`class Animal:
    def __init__(self, name):
        self.name = name

    def speak(self):
        pass

class Dog(Animal):
    def speak(self):
        return "Woof!"
`)
	result, err := ParseSource(src, "test.py", "python", lang.Default.TreeSitter("python"))
	if err != nil {
		t.Fatal(err)
	}

	animal := findSymbolKind(result.Symbols, "Animal", "class")
	if animal == nil {
		t.Fatal("expected to find Animal class")
	}

	dog := findSymbolKind(result.Symbols, "Dog", "class")
	if dog == nil {
		t.Fatal("expected to find Dog class")
	}

	// __init__ should be kept
	init := findSymbol(result.Symbols, "__init__")
	if init == nil {
		t.Fatal("expected __init__ to be kept")
	}
}

func TestFeaturePythonPrivateSkipped(t *testing.T) {
	src := []byte(`def public_func():
    pass

def _private_func():
    pass

def __very_private():
    pass
`)
	result, err := ParseSource(src, "test.py", "python", lang.Default.TreeSitter("python"))
	if err != nil {
		t.Fatal(err)
	}

	pub := findSymbol(result.Symbols, "public_func")
	if pub == nil {
		t.Fatal("expected to find public_func")
	}

	priv := findSymbol(result.Symbols, "_private_func")
	if priv != nil {
		t.Error("expected _private_func to be skipped")
	}

	vpriv := findSymbol(result.Symbols, "__very_private")
	if vpriv != nil {
		t.Error("expected __very_private to be skipped")
	}
}

func TestFeaturePythonDecorated(t *testing.T) {
	src := []byte(`import functools

@functools.lru_cache
def cached_func(x):
    return x * 2

@staticmethod
def static_method():
    pass
`)
	result, err := ParseSource(src, "test.py", "python", lang.Default.TreeSitter("python"))
	if err != nil {
		t.Fatal(err)
	}

	cached := findSymbol(result.Symbols, "cached_func")
	if cached == nil {
		t.Fatal("expected to find cached_func (decorated)")
	}
	if cached.Kind != "function" {
		t.Errorf("expected kind 'function', got %q", cached.Kind)
	}
}

func TestFeaturePythonImports(t *testing.T) {
	src := []byte(`import os
from pathlib import Path
from collections import defaultdict
`)
	result, err := ParseSource(src, "test.py", "python", lang.Default.TreeSitter("python"))
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Imports) < 3 {
		t.Fatalf("expected at least 3 imports, got %d", len(result.Imports))
	}

	osImp := findImport(result.Imports, "os")
	if osImp == nil {
		t.Fatal("expected to find os import")
	}
}

// --- JavaScript Language Feature Tests ---

func TestFeatureJSFunctions(t *testing.T) {
	src := []byte(`function greet(name) {
    return "Hello, " + name;
}

class UserService {
    constructor(db) {
        this.db = db;
    }

    getUser(id) {
        return this.db.find(id);
    }
}
`)
	result, err := ParseSource(src, "test.js", "javascript", lang.Default.TreeSitter("javascript"))
	if err != nil {
		t.Fatal(err)
	}

	greet := findSymbolKind(result.Symbols, "greet", "function")
	if greet == nil {
		t.Fatal("expected to find greet function")
	}

	userSvc := findSymbolKind(result.Symbols, "UserService", "class")
	if userSvc == nil {
		t.Fatal("expected to find UserService class")
	}
}

func TestFeatureJSArrowFunctions(t *testing.T) {
	src := []byte(`const add = (a, b) => a + b;

const multiply = (a, b) => {
    return a * b;
};
`)
	result, err := ParseSource(src, "test.js", "javascript", lang.Default.TreeSitter("javascript"))
	if err != nil {
		t.Fatal(err)
	}

	add := findSymbol(result.Symbols, "add")
	if add == nil {
		t.Fatal("expected to find add arrow function")
	}
	if add.Kind != "function" {
		t.Errorf("expected arrow function kind 'function', got %q", add.Kind)
	}

	mult := findSymbol(result.Symbols, "multiply")
	if mult == nil {
		t.Fatal("expected to find multiply arrow function")
	}
}

func TestFeatureJSExports(t *testing.T) {
	src := []byte(`export function fetchData(url) {
    return fetch(url);
}

export class ApiClient {
    request(endpoint) {}
}
`)
	result, err := ParseSource(src, "test.js", "javascript", lang.Default.TreeSitter("javascript"))
	if err != nil {
		t.Fatal(err)
	}

	fetchData := findSymbol(result.Symbols, "fetchData")
	if fetchData == nil {
		t.Fatal("expected to find exported fetchData function")
	}

	apiClient := findSymbol(result.Symbols, "ApiClient")
	if apiClient == nil {
		t.Fatal("expected to find exported ApiClient class")
	}
}

// --- TypeScript Language Feature Tests ---

func TestFeatureTSInterfaces(t *testing.T) {
	src := []byte(`interface User {
    id: number;
    name: string;
    email: string;
}

interface Repository<T> {
    find(id: number): T;
    save(item: T): void;
}
`)
	result, err := ParseSource(src, "test.ts", "typescript", lang.Default.TreeSitter("typescript"))
	if err != nil {
		t.Fatal(err)
	}

	user := findSymbolKind(result.Symbols, "User", "interface")
	if user == nil {
		t.Fatal("expected to find User interface")
	}

	repo := findSymbolKind(result.Symbols, "Repository", "interface")
	if repo == nil {
		t.Fatal("expected to find Repository interface")
	}
}

func TestFeatureTSTypeAliases(t *testing.T) {
	src := []byte(`type ID = string | number;

type Result<T> = {
    data: T;
    error: string | null;
};
`)
	result, err := ParseSource(src, "test.ts", "typescript", lang.Default.TreeSitter("typescript"))
	if err != nil {
		t.Fatal(err)
	}

	id := findSymbolKind(result.Symbols, "ID", "type")
	if id == nil {
		t.Fatal("expected to find ID type alias")
	}

	res := findSymbolKind(result.Symbols, "Result", "type")
	if res == nil {
		t.Fatal("expected to find Result type alias")
	}
}

func TestFeatureTSEnums(t *testing.T) {
	src := []byte(`enum Color {
    Red = "RED",
    Green = "GREEN",
    Blue = "BLUE",
}

enum Direction {
    Up,
    Down,
    Left,
    Right,
}
`)
	result, err := ParseSource(src, "test.ts", "typescript", lang.Default.TreeSitter("typescript"))
	if err != nil {
		t.Fatal(err)
	}

	color := findSymbolKind(result.Symbols, "Color", "enum")
	if color == nil {
		t.Fatal("expected to find Color enum")
	}

	dir := findSymbolKind(result.Symbols, "Direction", "enum")
	if dir == nil {
		t.Fatal("expected to find Direction enum")
	}
}

// --- Rust Language Feature Tests ---

func TestFeatureRustFunctions(t *testing.T) {
	src := []byte(`fn hello(name: &str) -> String {
    format!("Hello, {}", name)
}

pub fn add(a: i32, b: i32) -> i32 {
    a + b
}
`)
	result, err := ParseSource(src, "test.rs", "rust", lang.Default.TreeSitter("rust"))
	if err != nil {
		t.Fatal(err)
	}

	hello := findSymbolKind(result.Symbols, "hello", "function")
	if hello == nil {
		t.Fatal("expected to find hello function")
	}

	add := findSymbolKind(result.Symbols, "add", "function")
	if add == nil {
		t.Fatal("expected to find add function")
	}
}

func TestFeatureRustStructsEnums(t *testing.T) {
	src := []byte(`struct Point {
    x: f64,
    y: f64,
}

enum Shape {
    Circle(f64),
    Rectangle(f64, f64),
    Triangle(f64, f64, f64),
}
`)
	result, err := ParseSource(src, "test.rs", "rust", lang.Default.TreeSitter("rust"))
	if err != nil {
		t.Fatal(err)
	}

	point := findSymbolKind(result.Symbols, "Point", "struct")
	if point == nil {
		t.Fatal("expected to find Point struct")
	}

	shape := findSymbolKind(result.Symbols, "Shape", "enum")
	if shape == nil {
		t.Fatal("expected to find Shape enum")
	}
}

func TestFeatureRustTraits(t *testing.T) {
	src := []byte(`trait Drawable {
    fn draw(&self);
    fn area(&self) -> f64;
}
`)
	result, err := ParseSource(src, "test.rs", "rust", lang.Default.TreeSitter("rust"))
	if err != nil {
		t.Fatal(err)
	}

	drawable := findSymbolKind(result.Symbols, "Drawable", "trait")
	if drawable == nil {
		t.Fatal("expected to find Drawable trait")
	}
}

func TestFeatureRustImpl(t *testing.T) {
	src := []byte(`struct Circle {
    radius: f64,
}

impl Circle {
    fn new(radius: f64) -> Circle {
        Circle { radius }
    }

    fn area(&self) -> f64 {
        std::f64::consts::PI * self.radius * self.radius
    }
}
`)
	result, err := ParseSource(src, "test.rs", "rust", lang.Default.TreeSitter("rust"))
	if err != nil {
		t.Fatal(err)
	}

	circle := findSymbolKind(result.Symbols, "Circle", "struct")
	if circle == nil {
		t.Fatal("expected to find Circle struct")
	}

	impl := findSymbolKind(result.Symbols, "Circle", "impl")
	if impl == nil {
		t.Fatal("expected to find Circle impl block")
	}

	// Methods inside impl should be found
	newFn := findSymbol(result.Symbols, "new")
	if newFn == nil {
		t.Fatal("expected to find new function inside impl")
	}
	if newFn.Parent != "Circle" {
		t.Errorf("expected parent 'Circle', got %q", newFn.Parent)
	}
}

func TestFeatureRustScopedCallRef(t *testing.T) {
	src := []byte(`fn helper() {}

fn main() {
    let v = 1;
    std::mem::drop(v);
    helper();
}
`)
	result, err := ParseSource(src, "test.rs", "rust", lang.Default.TreeSitter("rust"))
	if err != nil {
		t.Fatal(err)
	}

	if findRef(result.Refs, "std::mem::drop") == nil {
		t.Fatal("expected scoped rust ref 'std::mem::drop'")
	}
	if findRef(result.Refs, "helper") == nil {
		t.Fatal("expected helper ref")
	}
}

// --- Kotlin Language Feature Tests ---

func TestFeatureKotlinSymbols(t *testing.T) {
	src := []byte(`package com.example.foo

import com.fasterxml.jackson.annotation.JsonProperty
import kotlinx.coroutines.flow.Flow

@JvmInline
value class ItemId(val value: String)

enum class ItemType { NORMAL, CONTAINER }

data class Item(
  val id: ItemId,
  val type: ItemType,
)

interface GameEngine {
  fun start()
  fun stop()
}

object Singleton {
  const val VERSION = "1.0"
  fun boot() {}
}

typealias UserId = String

class GameSession(val id: String) {
  val createdAt: Long = 0L
  fun tick() {
    println("tick")
    doThing()
  }

  companion object {
    fun create(): GameSession = GameSession("")
  }
}

fun topLevel(a: Int): Int = a + 1

val GLOBAL = 42
`)
	result, err := ParseSource(src, "test.kt", "kotlin", lang.Default.TreeSitter("kotlin"))
	if err != nil {
		t.Fatal(err)
	}

	// Value class / data class / regular class — all kind "class".
	if findSymbolKind(result.Symbols, "ItemId", "class") == nil {
		t.Error("expected ItemId value class")
	}
	if findSymbolKind(result.Symbols, "Item", "class") == nil {
		t.Error("expected Item data class")
	}
	if findSymbolKind(result.Symbols, "GameSession", "class") == nil {
		t.Error("expected GameSession class")
	}

	// Enum class.
	if findSymbolKind(result.Symbols, "ItemType", "enum") == nil {
		t.Error("expected ItemType enum")
	}

	// Interface.
	if findSymbolKind(result.Symbols, "GameEngine", "interface") == nil {
		t.Error("expected GameEngine interface")
	}

	// Object.
	if findSymbolKind(result.Symbols, "Singleton", "object") == nil {
		t.Error("expected Singleton object")
	}

	// typealias.
	if findSymbolKind(result.Symbols, "UserId", "type") == nil {
		t.Error("expected UserId type alias")
	}

	// Top-level function.
	if findSymbolKind(result.Symbols, "topLevel", "function") == nil {
		t.Error("expected topLevel function")
	}

	// Top-level property.
	if findSymbolKind(result.Symbols, "GLOBAL", "variable") == nil {
		t.Error("expected GLOBAL variable")
	}

	// const val inside object → constant.
	if findSymbolKind(result.Symbols, "VERSION", "constant") == nil {
		t.Error("expected VERSION constant")
	}

	// Method inside class.
	tick := findSymbolKind(result.Symbols, "tick", "method")
	if tick == nil {
		t.Fatal("expected tick method")
	}
	if tick.Parent != "GameSession" {
		t.Errorf("expected tick parent GameSession, got %q", tick.Parent)
	}

	// Field inside class.
	if findSymbolKind(result.Symbols, "createdAt", "field") == nil {
		t.Error("expected createdAt field")
	}

	// Enum member.
	if findSymbolKind(result.Symbols, "NORMAL", "enum_member") == nil {
		t.Error("expected NORMAL enum_member")
	}

	// Imports.
	if findImport(result.Imports, "com.fasterxml.jackson.annotation.JsonProperty") == nil {
		t.Error("expected JsonProperty import")
	}
	if findImport(result.Imports, "kotlinx.coroutines.flow.Flow") == nil {
		t.Error("expected Flow import")
	}

	// Refs from call_expression.
	if findRef(result.Refs, "println") == nil {
		t.Error("expected println ref")
	}
	if findRef(result.Refs, "doThing") == nil {
		t.Error("expected doThing ref")
	}

	// Signature should be captured for functions.
	if topLevel := findSymbol(result.Symbols, "topLevel"); topLevel == nil || topLevel.Signature == "" {
		t.Error("expected non-empty signature for topLevel function")
	}
}

// --- Dart Language Feature Tests ---

func TestFeatureDartSymbols(t *testing.T) {
	src := []byte(`import 'dart:core';
import 'package:flutter/material.dart';

typedef StringCallback = void Function(String value);

mixin Printable {
  void printSelf() {
    print(toString());
  }
}

enum Color { red, green, blue }

abstract class Shape with Printable {
  String get name;
  set name(String value);

  double area();

  Shape();
  Shape.origin() : this();
}

class Circle extends Shape {
  final double radius;

  Circle(this.radius);
  factory Circle.unit() => Circle(1.0);

  @override
  String get name => 'circle';

  @override
  set name(String value) {}

  @override
  double area() {
    return 3.14159 * radius * radius;
  }
}

extension ShapeUtils on Shape {
  bool isLargerThan(Shape other) {
    return area() > other.area();
  }
}

void main() {
  final c = Circle(5.0);
  c.area();
  print(c.name);
  doSomething();
}

void doSomething() {}
`)
	result, err := ParseSource(src, "test.dart", "dart", lang.Default.TreeSitter("dart"))
	if err != nil {
		t.Fatal(err)
	}

	// Debug: print all symbols if any assertion fails.
	debug := func() {
		t.Helper()
		t.Log("=== All symbols ===")
		for _, s := range result.Symbols {
			t.Logf("  %s (%s) parent=%q depth=%d lines=%d-%d sig=%q",
				s.Name, s.Kind, s.Parent, s.Depth, s.StartLine, s.EndLine, s.Signature)
		}
		t.Log("=== All imports ===")
		for _, imp := range result.Imports {
			t.Logf("  %s", imp.RawPath)
		}
		t.Log("=== All refs ===")
		for _, ref := range result.Refs {
			t.Logf("  %s (line %d)", ref.Name, ref.Line)
		}
	}

	// --- Imports ---
	if findImport(result.Imports, "dart:core") == nil {
		debug()
		t.Error("expected import 'dart:core'")
	}
	if findImport(result.Imports, "package:flutter/material.dart") == nil {
		debug()
		t.Error("expected import 'package:flutter/material.dart'")
	}

	// --- Type alias ---
	if findSymbolKind(result.Symbols, "StringCallback", "type") == nil {
		debug()
		t.Error("expected StringCallback type alias")
	}

	// --- Mixin ---
	if findSymbolKind(result.Symbols, "Printable", "mixin") == nil {
		debug()
		t.Error("expected Printable mixin")
	}

	// --- Enum ---
	if findSymbolKind(result.Symbols, "Color", "enum") == nil {
		debug()
		t.Error("expected Color enum")
	}

	// --- Abstract class ---
	if findSymbolKind(result.Symbols, "Shape", "class") == nil {
		debug()
		t.Error("expected Shape class")
	}

	// --- Concrete class ---
	if findSymbolKind(result.Symbols, "Circle", "class") == nil {
		debug()
		t.Error("expected Circle class")
	}

	// --- Extension ---
	if findSymbolKind(result.Symbols, "ShapeUtils", "extension") == nil {
		debug()
		t.Error("expected ShapeUtils extension")
	}

	// --- Methods inside class ---
	areaSym := findSymbolKind(result.Symbols, "area", "method")
	if areaSym == nil {
		debug()
		t.Fatal("expected area method")
	}

	// --- Top-level function ---
	if findSymbolKind(result.Symbols, "main", "function") == nil {
		debug()
		t.Error("expected main function")
	}
	if findSymbolKind(result.Symbols, "doSomething", "function") == nil {
		debug()
		t.Error("expected doSomething function")
	}

	// --- Getters ---
	// The Shape class declares `String get name;` — should be a getter.
	nameSym := findSymbolKind(result.Symbols, "name", "getter")
	if nameSym == nil {
		debug()
		t.Error("expected 'name' getter")
	}

	// --- Setters ---
	nameSetSym := findSymbolKind(result.Symbols, "name", "setter")
	if nameSetSym == nil {
		debug()
		t.Error("expected 'name' setter")
	}

	// --- Constructors ---
	// Shape() and Shape.origin() both map to constructor kind with name "Shape".
	if findSymbolKind(result.Symbols, "Shape", "constructor") == nil {
		debug()
		t.Error("expected Shape constructor")
	}
	// Circle(this.radius) and factory Circle.unit() both map to constructor kind.
	if findSymbolKind(result.Symbols, "Circle", "constructor") == nil {
		debug()
		t.Error("expected Circle constructor")
	}

	// --- Mixin members ---
	printSelfSym := findSymbolKind(result.Symbols, "printSelf", "method")
	if printSelfSym == nil {
		debug()
		t.Fatal("expected printSelf method (mixin member)")
	}
	if printSelfSym.Parent != "Printable" {
		t.Errorf("expected printSelf parent 'Printable', got %q", printSelfSym.Parent)
	}

	// --- Extension members ---
	isLargerSym := findSymbolKind(result.Symbols, "isLargerThan", "method")
	if isLargerSym == nil {
		debug()
		t.Fatal("expected isLargerThan method (extension member)")
	}
	if isLargerSym.Parent != "ShapeUtils" {
		t.Errorf("expected isLargerThan parent 'ShapeUtils', got %q", isLargerSym.Parent)
	}

	// --- Refs (function/method calls) ---
	if findRef(result.Refs, "print") == nil {
		debug()
		t.Error("expected print ref")
	}
	if findRef(result.Refs, "area") == nil {
		debug()
		t.Error("expected area ref")
	}

	// --- Signatures ---
	// Functions have a formal_parameter_list signature.
	mainSym := findSymbol(result.Symbols, "main")
	if mainSym == nil || mainSym.Signature == "" {
		debug()
		t.Error("expected non-empty signature for main function")
	}
	// Setters carry their single parameter as a signature.
	if nameSetSym != nil && nameSetSym.Signature == "" {
		t.Error("expected non-empty signature for name setter")
	}
	// Constructor with a parameter list should have a signature.
	circleCtor := findSymbolKind(result.Symbols, "Circle", "constructor")
	if circleCtor == nil || circleCtor.Signature == "" {
		t.Error("expected non-empty signature for Circle constructor")
	}
}

// --- Swift Language Feature Tests ---

func TestFeatureSwiftSymbols(t *testing.T) {
	src := []byte(`import Foundation
import SwiftUI

protocol BabyTrackingService {
    func logFeeding(_ event: FeedingEvent) async throws
}

struct FeedingActivityAttributes {
    var babyName: String
}

enum FeedingKind {
    case bottle
    case breast
}

final class BabyTrackingServiceImpl: BabyTrackingService {
    let attrs: FeedingActivityAttributes
    var kinds: [FeedingKind] = []

    init(attrs: FeedingActivityAttributes) {
        self.attrs = attrs
    }

    func logFeeding(_ event: FeedingEvent) async throws {}
}

extension BabyTrackingServiceImpl {
    func summary() -> String { "ok" }
}

func make() -> BabyTrackingService {
    return BabyTrackingServiceImpl(attrs: FeedingActivityAttributes(babyName: ""))
}

let GLOBAL = 42
var counter = 0
`)
	result, err := ParseSource(src, "test.swift", "swift", lang.Default.TreeSitter("swift"))
	if err != nil {
		t.Fatal(err)
	}

	// Protocol.
	if findSymbolKind(result.Symbols, "BabyTrackingService", "protocol") == nil {
		t.Error("expected BabyTrackingService protocol")
	}

	// Struct / enum / class.
	if findSymbolKind(result.Symbols, "FeedingActivityAttributes", "struct") == nil {
		t.Error("expected FeedingActivityAttributes struct")
	}
	if findSymbolKind(result.Symbols, "FeedingKind", "enum") == nil {
		t.Error("expected FeedingKind enum")
	}
	if findSymbolKind(result.Symbols, "BabyTrackingServiceImpl", "class") == nil {
		t.Error("expected BabyTrackingServiceImpl class")
	}

	// Extension — name is the extended type.
	if findSymbolKind(result.Symbols, "BabyTrackingServiceImpl", "extension") == nil {
		t.Error("expected BabyTrackingServiceImpl extension")
	}

	// Top-level function.
	if findSymbolKind(result.Symbols, "make", "function") == nil {
		t.Error("expected make function")
	}

	// logFeeding appears twice — once as a protocol requirement (parent
	// BabyTrackingService) and once as a class method (parent
	// BabyTrackingServiceImpl). Both should be classified as method.
	var sawProtocol, sawImpl bool
	for _, s := range result.Symbols {
		if s.Name != "logFeeding" || s.Kind != "method" {
			continue
		}
		switch s.Parent {
		case "BabyTrackingService":
			sawProtocol = true
		case "BabyTrackingServiceImpl":
			sawImpl = true
		}
	}
	if !sawProtocol {
		t.Error("expected logFeeding method under BabyTrackingService protocol")
	}
	if !sawImpl {
		t.Error("expected logFeeding method under BabyTrackingServiceImpl class")
	}

	// Constructor.
	if findSymbolKind(result.Symbols, "init", "constructor") == nil {
		t.Error("expected init constructor")
	}

	// Fields (inside class_body).
	if findSymbolKind(result.Symbols, "attrs", "field") == nil {
		t.Error("expected attrs field")
	}
	if findSymbolKind(result.Symbols, "kinds", "field") == nil {
		t.Error("expected kinds field")
	}

	// Top-level bindings: let → constant, var → variable.
	if findSymbolKind(result.Symbols, "GLOBAL", "constant") == nil {
		t.Error("expected GLOBAL constant")
	}
	if findSymbolKind(result.Symbols, "counter", "variable") == nil {
		t.Error("expected counter variable")
	}

	// Enum members.
	if findSymbolKind(result.Symbols, "bottle", "enum_member") == nil {
		t.Error("expected bottle enum_member")
	}

	// Imports.
	if findImport(result.Imports, "Foundation") == nil {
		t.Error("expected Foundation import")
	}
	if findImport(result.Imports, "SwiftUI") == nil {
		t.Error("expected SwiftUI import")
	}

	// Signature captured for functions.
	if m := findSymbol(result.Symbols, "make"); m == nil || m.Signature == "" {
		t.Error("expected non-empty signature for make function")
	}
}

func TestFeatureSwiftActor(t *testing.T) {
	// Swift concurrency `actor` is a `class_declaration` with keyword `actor`
	// in tree-sitter-swift; it must be classified as its own kind rather than
	// dropped.
	src := []byte(`actor DataStore {
    var items: [Int] = []
    func append(_ x: Int) { items.append(x) }
}

distributed actor Worker {
    func tick() {}
}
`)
	result, err := ParseSource(src, "test.swift", "swift", lang.Default.TreeSitter("swift"))
	if err != nil {
		t.Fatal(err)
	}
	if findSymbolKind(result.Symbols, "DataStore", "actor") == nil {
		t.Error("expected DataStore actor")
	}
	if findSymbolKind(result.Symbols, "Worker", "actor") == nil {
		t.Error("expected distributed Worker actor")
	}
	// Members should still be classified with DataStore as parent.
	m := findSymbolKind(result.Symbols, "append", "method")
	if m == nil {
		t.Fatal("expected append method inside actor body")
	}
	if m.Parent != "DataStore" {
		t.Errorf("expected append parent DataStore, got %q", m.Parent)
	}
}

func TestFeatureSwiftRefs(t *testing.T) {
	src := []byte(`protocol BabyTrackingService {}

final class BabyTrackingServiceImpl: BabyTrackingService {
    private let store: FeedingStore
    let attrs: FeedingActivityAttributes
    var kinds: [FeedingKind] = []
    var mapping: [String: BabyTrackingService] = [:]

    init(store: FeedingStore) {
        self.store = store
    }

    func logFeeding(_ event: FeedingEvent) async throws {
        try await store.persist(event)
        let arr: Array<FeedingKind> = []
        _ = arr
    }
}

func make() -> BabyTrackingService {
    return BabyTrackingServiceImpl(store: InMemoryFeedingStore())
}
`)
	result, err := ParseSource(src, "test.swift", "swift", lang.Default.TreeSitter("swift"))
	if err != nil {
		t.Fatal(err)
	}

	// Type annotation refs: `let store: FeedingStore`, `let attrs: FeedingActivityAttributes`.
	if findRef(result.Refs, "FeedingStore") == nil {
		t.Error("expected FeedingStore ref from type annotation")
	}
	if findRef(result.Refs, "FeedingActivityAttributes") == nil {
		t.Error("expected FeedingActivityAttributes ref from type annotation")
	}

	// Generic / array / dictionary element types.
	if findRef(result.Refs, "FeedingKind") == nil {
		t.Error("expected FeedingKind ref from array_type / generic")
	}
	if findRef(result.Refs, "BabyTrackingService") == nil {
		t.Error("expected BabyTrackingService ref (inheritance or dictionary value)")
	}

	// Constructor / function calls.
	if findRef(result.Refs, "BabyTrackingServiceImpl") == nil {
		t.Error("expected BabyTrackingServiceImpl ref from call_expression")
	}
	if findRef(result.Refs, "InMemoryFeedingStore") == nil {
		t.Error("expected InMemoryFeedingStore ref from nested call_expression")
	}

	// Navigation-expression call: `store.persist(event)` → persist.
	if findRef(result.Refs, "persist") == nil {
		t.Error("expected persist ref from store.persist(event)")
	}

	// Parameter type.
	if findRef(result.Refs, "FeedingEvent") == nil {
		t.Error("expected FeedingEvent ref from parameter type")
	}

	// Function return type.
	// (BabyTrackingService is also the protocol's name — confirmed via any occurrence.)
}

// --- C Language Feature Tests ---

func TestFeatureCRefs(t *testing.T) {
	src := []byte(`#include <stdio.h>
#include <stdlib.h>

struct Point { int x; int y; };

enum Color { RED, GREEN, BLUE };

typedef unsigned long ulong;

typedef int (*op_t)(int);

int double_it(int x) {
    return x * 2;
}

struct FnBox {
    op_t cb;
};

int add(int a, int b) {
    return a + b;
}

void helper(int x) {}

int main() {
    int result = add(1, 2);
    helper(result);
    printf("result = %d\n", result);

    struct FnBox box;
    box.cb = double_it;
    box.cb(7);
    struct FnBox *boxPtr = &box;
    boxPtr->cb(8);

    int *p = malloc(sizeof(int));
    free(p);
    return 0;
}
`)
	result, err := ParseSource(src, "test.c", "c", lang.Default.TreeSitter("c"))
	if err != nil {
		t.Fatal(err)
	}

	debug := func() { debugParseResult(t, result) }

	// --- Imports ---
	if findImport(result.Imports, "stdio.h") == nil {
		debug()
		t.Error("expected import 'stdio.h'")
	}
	if findImport(result.Imports, "stdlib.h") == nil {
		debug()
		t.Error("expected import 'stdlib.h'")
	}

	// --- Symbols (existing classifyC coverage) ---
	if findSymbolKind(result.Symbols, "Point", "struct") == nil {
		debug()
		t.Error("expected Point struct")
	}
	if findSymbolKind(result.Symbols, "Color", "enum") == nil {
		debug()
		t.Error("expected Color enum")
	}
	if findSymbolKind(result.Symbols, "ulong", "type") == nil {
		debug()
		t.Error("expected ulong typedef")
	}
	if findSymbolKind(result.Symbols, "add", "function") == nil {
		debug()
		t.Error("expected add function")
	}
	if findSymbolKind(result.Symbols, "helper", "function") == nil {
		debug()
		t.Error("expected helper function")
	}
	if findSymbolKind(result.Symbols, "main", "function") == nil {
		debug()
		t.Error("expected main function")
	}

	// --- Refs (call-site extraction, new feature) ---
	addRef := findRef(result.Refs, "add")
	if addRef == nil {
		debug()
		t.Fatal("expected ref to 'add'")
	}
	if addRef.Line == 0 {
		t.Error("expected non-zero line for add ref")
	}

	helperRef := findRef(result.Refs, "helper")
	if helperRef == nil {
		debug()
		t.Fatal("expected ref to 'helper'")
	}

	if findRef(result.Refs, "printf") == nil {
		debug()
		t.Error("expected ref to 'printf'")
	}
	if findRef(result.Refs, "malloc") == nil {
		debug()
		t.Error("expected ref to 'malloc'")
	}
	if findRef(result.Refs, "free") == nil {
		debug()
		t.Error("expected ref to 'free'")
	}
	if findRef(result.Refs, "cb") == nil {
		debug()
		t.Error("expected ref to 'cb' from function-pointer field calls")
	}
	if findRef(result.Refs, "boxPtr->cb") != nil {
		debug()
		t.Error("expected pointer call to normalize to 'cb', got raw 'boxPtr->cb'")
	}
}

// --- C++ Language Feature Tests ---

func TestFeatureCPPRefs(t *testing.T) {
	src := []byte(`#include <iostream>
#include <algorithm>
#include <vector>

struct Point { int x; int y; };

enum Color { RED, GREEN, BLUE };

typedef unsigned long ulong;

class Calculator {
public:
    int add(int a, int b) { return a + b; }
    int subtract(int a, int b) { return a - b; }
    static int multiply(int a, int b) { return a * b; }
};

namespace utils {
    void helper(int x) {}
}

void standalone() {}

int main() {
    Calculator calc;
    Calculator* ptr = &calc;
    int sum = calc.add(1, 2);
    int diff = ptr->subtract(9, 3);
    int product = Calculator::multiply(3, 4);
    int mx = std::max<int>(1, 2);
    utils::helper(sum);
    standalone();
    printf("done");
    return 0;
}
`)
	result, err := ParseSource(src, "test.cpp", "cpp", lang.Default.TreeSitter("cpp"))
	if err != nil {
		t.Fatal(err)
	}

	debug := func() { debugParseResult(t, result) }

	// --- Imports ---
	if findImport(result.Imports, "iostream") == nil {
		debug()
		t.Error("expected import 'iostream'")
	}
	if findImport(result.Imports, "vector") == nil {
		debug()
		t.Error("expected import 'vector'")
	}

	// --- Symbols (existing classifyC coverage for C++) ---
	if findSymbolKind(result.Symbols, "Point", "struct") == nil {
		debug()
		t.Error("expected Point struct")
	}
	if findSymbolKind(result.Symbols, "Color", "enum") == nil {
		debug()
		t.Error("expected Color enum")
	}
	if findSymbolKind(result.Symbols, "ulong", "type") == nil {
		debug()
		t.Error("expected ulong typedef")
	}
	if findSymbolKind(result.Symbols, "standalone", "function") == nil {
		debug()
		t.Error("expected standalone function")
	}
	if findSymbolKind(result.Symbols, "main", "function") == nil {
		debug()
		t.Error("expected main function")
	}

	// --- Refs (call-site extraction, new feature) ---
	// Simple function call.
	if findRef(result.Refs, "standalone") == nil {
		debug()
		t.Error("expected ref to 'standalone'")
	}

	// Method call via dot: calc.add(1, 2) should extract "add".
	addRef := findRef(result.Refs, "add")
	if addRef == nil {
		debug()
		t.Fatal("expected ref to 'add' (method call via dot)")
	}
	if addRef.Line == 0 {
		t.Error("expected non-zero line for add ref")
	}

	// Method call via pointer: ptr->subtract(9, 3) should extract "subtract".
	if findRef(result.Refs, "subtract") == nil {
		debug()
		t.Error("expected ref to 'subtract' (pointer call via ->)")
	}

	// Static method call via :: scope: Calculator::multiply(3, 4) should extract "multiply".
	if findRef(result.Refs, "multiply") == nil {
		debug()
		t.Error("expected ref to 'multiply' (qualified call via ::)")
	}

	// Template call should normalize to the base callable name.
	if findRef(result.Refs, "max") == nil {
		debug()
		t.Error("expected template call ref to normalize to 'max'")
	}
	if findRef(result.Refs, "max<int>") != nil {
		debug()
		t.Error("expected no raw template ref name 'max<int>'")
	}

	// Namespace-scoped call: utils::helper(sum) should extract "helper".
	if findRef(result.Refs, "helper") == nil {
		debug()
		t.Error("expected ref to 'helper' (namespace-scoped call via ::)")
	}

	// Plain C-style call in C++ context.
	if findRef(result.Refs, "printf") == nil {
		debug()
		t.Error("expected ref to 'printf'")
	}
}

// --- Multi-language table-driven test ---

func TestFeatureParseMultiLanguage(t *testing.T) {
	tests := []struct {
		name     string
		lang     string
		src      string
		file     string
		wantSyms []struct{ name, kind string }
	}{
		{
			name: "Go mixed symbols",
			lang: "go",
			file: "test.go",
			src: `package main

type Config struct { Port int }
type Reader interface { Read() }
func NewConfig() *Config { return nil }
const Version = "1.0"
`,
			wantSyms: []struct{ name, kind string }{
				{"Config", "struct"},
				{"Reader", "interface"},
				{"NewConfig", "function"},
				{"Version", "constant"},
			},
		},
		{
			name: "Python mixed symbols",
			lang: "python",
			file: "test.py",
			src: `class MyClass:
    def __init__(self):
        pass

    def method(self):
        pass

def standalone():
    pass
`,
			wantSyms: []struct{ name, kind string }{
				{"MyClass", "class"},
				{"__init__", "function"},
				{"standalone", "function"},
			},
		},
		{
			name: "TypeScript mixed",
			lang: "typescript",
			file: "test.ts",
			src: `interface Props { name: string; }
type ID = number;
enum Status { Active, Inactive }
function render(props: Props) {}
class Component {}
`,
			wantSyms: []struct{ name, kind string }{
				{"Props", "interface"},
				{"ID", "type"},
				{"Status", "enum"},
				{"render", "function"},
				{"Component", "class"},
			},
		},
		{
			name: "Rust mixed",
			lang: "rust",
			file: "test.rs",
			src: `struct Config { port: u16 }
enum Mode { Debug, Release }
trait Runnable { fn run(&self); }
fn main() {}
`,
			wantSyms: []struct{ name, kind string }{
				{"Config", "struct"},
				{"Mode", "enum"},
				{"Runnable", "trait"},
				{"main", "function"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseSource([]byte(tt.src), tt.file, tt.lang, lang.Default.TreeSitter(tt.lang))
			if err != nil {
				t.Fatal(err)
			}

			for _, want := range tt.wantSyms {
				sym := findSymbolKind(result.Symbols, want.name, want.kind)
				if sym == nil {
					t.Errorf("expected to find %s (%s) but didn't. Found symbols:", want.name, want.kind)
					for _, s := range result.Symbols {
						t.Errorf("  - %s (%s)", s.Name, s.Kind)
					}
				}
			}
		})
	}
}

func TestFeatureUnsupportedLanguage(t *testing.T) {
	_, err := ParseFile("test.xyz", "nonexistent_lang")
	if err == nil {
		t.Error("expected error for unsupported language")
	}
}

func TestFeatureSymbolLineNumbers(t *testing.T) {
	src := []byte(`package main

func First() {}

func Second() {}

func Third() {}
`)
	result, err := ParseSource(src, "test.go", "go", lang.Default.TreeSitter("go"))
	if err != nil {
		t.Fatal(err)
	}

	first := findSymbol(result.Symbols, "First")
	second := findSymbol(result.Symbols, "Second")
	third := findSymbol(result.Symbols, "Third")

	if first == nil || second == nil || third == nil {
		t.Fatal("expected to find all three functions")
	}

	if first.StartLine >= second.StartLine {
		t.Error("First should come before Second")
	}
	if second.StartLine >= third.StartLine {
		t.Error("Second should come before Third")
	}
}

// --- Go Composite Literal Ref Tests ---

func TestFeatureGoCompositeLiteralRef(t *testing.T) {
	src := []byte(`package main

type UsedStruct struct {
	Name string
}

func main() {
	s := UsedStruct{Name: "foo"}
	_ = s
}
`)
	result, err := ParseSource(src, "test.go", "go", lang.Default.TreeSitter("go"))
	if err != nil {
		t.Fatal(err)
	}

	ref := findRef(result.Refs, "UsedStruct")
	if ref == nil {
		debugParseResult(t, result)
		t.Fatal("expected to find UsedStruct ref from composite literal")
	}
	if ref.Line != 8 {
		t.Errorf("expected UsedStruct ref on line 8, got %d", ref.Line)
	}
}

func TestFeatureGoMapLiteralRef(t *testing.T) {
	src := []byte(`package main

type MyKey string
type MyValue int

func main() {
	m := map[MyKey]MyValue{}
	_ = m
}
`)
	result, err := ParseSource(src, "test.go", "go", lang.Default.TreeSitter("go"))
	if err != nil {
		t.Fatal(err)
	}

	keyRef := findRef(result.Refs, "MyKey")
	if keyRef == nil {
		debugParseResult(t, result)
		t.Fatal("expected to find MyKey ref from map composite literal")
	}

	valRef := findRef(result.Refs, "MyValue")
	if valRef == nil {
		debugParseResult(t, result)
		t.Fatal("expected to find MyValue ref from map composite literal")
	}
}

func TestFeatureGoSliceLiteralRef(t *testing.T) {
	src := []byte(`package main

type Item struct{ V int }

func main() {
	items := []Item{{V: 1}, {V: 2}}
	_ = items
}
`)
	result, err := ParseSource(src, "test.go", "go", lang.Default.TreeSitter("go"))
	if err != nil {
		t.Fatal(err)
	}

	ref := findRef(result.Refs, "Item")
	if ref == nil {
		debugParseResult(t, result)
		t.Fatal("expected to find Item ref from slice composite literal")
	}
}

func TestFeatureGoQualifiedCompositeLiteralRef(t *testing.T) {
	src := []byte(`package main

import "net/http"

func main() {
	_ = http.Client{Timeout: 30}
}
`)
	result, err := ParseSource(src, "test.go", "go", lang.Default.TreeSitter("go"))
	if err != nil {
		t.Fatal(err)
	}

	ref := findRef(result.Refs, "Client")
	if ref == nil {
		debugParseResult(t, result)
		t.Fatal("expected to find Client ref from qualified composite literal")
	}
}

func TestFeatureGoQualifiedMapLiteralRef(t *testing.T) {
	src := []byte(`package main

import "pkg"

func main() {
	m := map[pkg.Key]pkg.Value{}
	_ = m
}
`)
	result, err := ParseSource(src, "test.go", "go", lang.Default.TreeSitter("go"))
	if err != nil {
		t.Fatal(err)
	}

	keyRef := findRef(result.Refs, "Key")
	if keyRef == nil {
		debugParseResult(t, result)
		t.Fatal("expected to find Key ref from qualified map key type")
	}

	valRef := findRef(result.Refs, "Value")
	if valRef == nil {
		debugParseResult(t, result)
		t.Fatal("expected to find Value ref from qualified map value type")
	}
}

func TestFeatureGoQualifiedSliceLiteralRef(t *testing.T) {
	src := []byte(`package main

import "pkg"

func main() {
	items := []pkg.Item{}
	_ = items
}
`)
	result, err := ParseSource(src, "test.go", "go", lang.Default.TreeSitter("go"))
	if err != nil {
		t.Fatal(err)
	}

	ref := findRef(result.Refs, "Item")
	if ref == nil {
		debugParseResult(t, result)
		t.Fatal("expected to find Item ref from qualified slice element type")
	}
}

func TestFeatureGoQualifiedArrayLiteralRef(t *testing.T) {
	src := []byte(`package main

import "pkg"

func main() {
	items := [3]pkg.Item{}
	_ = items
}
`)
	result, err := ParseSource(src, "test.go", "go", lang.Default.TreeSitter("go"))
	if err != nil {
		t.Fatal(err)
	}

	ref := findRef(result.Refs, "Item")
	if ref == nil {
		debugParseResult(t, result)
		t.Fatal("expected to find Item ref from qualified array element type")
	}
}

// --- JS/TS New Expression Ref Tests ---

func TestFeatureJSNewExpressionRef(t *testing.T) {
	src := []byte(`class UsedClass {
    constructor(name) {
        this.name = name;
    }
}

const obj = new UsedClass("test");
`)
	result, err := ParseSource(src, "test.js", "javascript", lang.Default.TreeSitter("javascript"))
	if err != nil {
		t.Fatal(err)
	}

	ref := findRef(result.Refs, "UsedClass")
	if ref == nil {
		debugParseResult(t, result)
		t.Fatal("expected to find UsedClass ref from new expression")
	}
	if ref.Line != 7 {
		t.Errorf("expected UsedClass ref on line 7, got %d", ref.Line)
	}
}

func TestFeatureTSNewExpressionRef(t *testing.T) {
	src := []byte(`class Service {
    private name: string;
    constructor(name: string) {
        this.name = name;
    }
}

const svc = new Service("api");
`)
	result, err := ParseSource(src, "test.ts", "typescript", lang.Default.TreeSitter("typescript"))
	if err != nil {
		t.Fatal(err)
	}

	ref := findRef(result.Refs, "Service")
	if ref == nil {
		debugParseResult(t, result)
		t.Fatal("expected to find Service ref from new expression in TypeScript")
	}
	if ref.Line != 8 {
		t.Errorf("expected Service ref on line 8, got %d", ref.Line)
	}
}

func TestFeatureJSNewExpressionMemberRef(t *testing.T) {
	src := []byte(`const ws = new WebSocket.Server({ port: 8080 });
`)
	result, err := ParseSource(src, "test.js", "javascript", lang.Default.TreeSitter("javascript"))
	if err != nil {
		t.Fatal(err)
	}

	ref := findRef(result.Refs, "Server")
	if ref == nil {
		debugParseResult(t, result)
		t.Fatal("expected to find Server ref from new member expression")
	}
}

// --- C# Feature Tests ---

func TestFeatureCSharpSymbols(t *testing.T) {
	src := []byte(`using System;
using System.Collections.Generic;
using static System.Math;

namespace MyApp.Core
{
    public interface IGreeter
    {
        string Greet(string name);
    }

    public class Greeter : IGreeter
    {
        private readonly string _prefix;

        public Greeter(string prefix)
        {
            _prefix = prefix;
        }

        public string Greet(string name)
        {
            return _prefix + ", " + name;
        }
    }

    public struct Point
    {
        public int X { get; }
    }

    public enum Color { Red, Green, Blue }

    public record User(string Name, int Age);
}
`)
	result, err := ParseSource(src, "test.cs", "csharp", lang.Default.TreeSitter("csharp"))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, kind string
	}{
		{"MyApp.Core", "namespace"},
		{"IGreeter", "interface"},
		{"Greeter", "class"},
		{"Greet", "method"},
		{"Point", "struct"},
		{"Color", "enum"},
		{"User", "record"},
	}
	for _, c := range cases {
		if findSymbolKind(result.Symbols, c.name, c.kind) == nil {
			debugParseResult(t, result)
			t.Fatalf("expected %s %q", c.kind, c.name)
		}
	}

	// Constructor
	if findSymbolKind(result.Symbols, "Greeter", "constructor") == nil {
		debugParseResult(t, result)
		t.Fatal("expected constructor Greeter")
	}
	// Property
	if findSymbolKind(result.Symbols, "X", "property") == nil {
		debugParseResult(t, result)
		t.Fatal("expected property X")
	}
}

func TestFeatureCSharpImports(t *testing.T) {
	src := []byte(`using System;
using System.Collections.Generic;
using static System.Math;
using Alias = System.IO.Path;
`)
	result, err := ParseSource(src, "test.cs", "csharp", lang.Default.TreeSitter("csharp"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"System", "System.Collections.Generic", "System.Math"} {
		if findImport(result.Imports, want) == nil {
			debugParseResult(t, result)
			t.Fatalf("expected using %q", want)
		}
	}
}

func TestFeatureCSharpRefs(t *testing.T) {
	src := []byte(`using System;

class Program
{
    static void Main()
    {
        var g = new Greeter("Hi");
        Console.WriteLine(g.Greet("world"));
    }
}
`)
	result, err := ParseSource(src, "test.cs", "csharp", lang.Default.TreeSitter("csharp"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"Greeter", "WriteLine", "Greet"} {
		if findRef(result.Refs, want) == nil {
			debugParseResult(t, result)
			t.Fatalf("expected ref %q", want)
		}
	}
}

// --- PHP Feature Tests ---

func TestFeaturePHPSymbols(t *testing.T) {
	src := []byte(`<?php
namespace App\Service;

interface Greeter {
    public function greet(string $name): string;
}

trait Loggable {
    public function log(string $msg): void {}
}

class DefaultGreeter implements Greeter
{
    use Loggable;

    public function __construct(string $prefix) {}

    public function greet(string $name): string {
        return $prefix . ', ' . $name;
    }
}

enum Color {
    case Red;
    case Green;
}

function make_greeter(string $p): Greeter {
    return new DefaultGreeter($p);
}
`)
	result, err := ParseSource(src, "test.php", "php", lang.Default.TreeSitter("php"))
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, kind string
	}{
		{"Greeter", "interface"},
		{"Loggable", "trait"},
		{"DefaultGreeter", "class"},
		{"Color", "enum"},
		{"make_greeter", "function"},
		{"greet", "method"},
		{"__construct", "method"},
	}
	for _, c := range cases {
		if findSymbolKind(result.Symbols, c.name, c.kind) == nil {
			debugParseResult(t, result)
			t.Fatalf("expected %s %q", c.kind, c.name)
		}
	}
}

func TestFeaturePHPImports(t *testing.T) {
	src := []byte(`<?php
namespace App\Service;

use App\Model\User;
use App\Repo\UserRepo as Repo;
use Psr\Log\LoggerInterface;
`)
	result, err := ParseSource(src, "test.php", "php", lang.Default.TreeSitter("php"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"App\\Model\\User", "App\\Repo\\UserRepo", "Psr\\Log\\LoggerInterface"} {
		if findImport(result.Imports, want) == nil {
			debugParseResult(t, result)
			t.Fatalf("expected use %q", want)
		}
	}
}

func TestFeaturePHPRefs(t *testing.T) {
	src := []byte(`<?php
function run() {
    $g = make_greeter('Hi');
    $g->greet('world');
    Logger::info('ok');
    $x = new DefaultGreeter('p');
}
`)
	result, err := ParseSource(src, "test.php", "php", lang.Default.TreeSitter("php"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"make_greeter", "greet", "info", "DefaultGreeter"} {
		if findRef(result.Refs, want) == nil {
			debugParseResult(t, result)
			t.Fatalf("expected ref %q", want)
		}
	}
}

// --- Lua Feature Tests ---

func TestFeatureLuaSymbols(t *testing.T) {
	src := []byte(`local M = {}

function M.greet(name)
    return "Hello, " .. name
end

local function helper(x)
    return x * 2
end

function M:new(opts)
    return opts
end

return M
`)
	result, err := ParseSource(src, "test.lua", "lua", lang.Default.TreeSitter("lua"))
	if err != nil {
		t.Fatal(err)
	}

	if findSymbolKind(result.Symbols, "greet", "function") == nil {
		debugParseResult(t, result)
		t.Fatal("expected function greet")
	}
	if findSymbolKind(result.Symbols, "helper", "function") == nil {
		debugParseResult(t, result)
		t.Fatal("expected function helper")
	}
	if findSymbolKind(result.Symbols, "new", "method") == nil {
		debugParseResult(t, result)
		t.Fatal("expected method new (M:new)")
	}
}

func TestFeatureLuaImports(t *testing.T) {
	src := []byte(`local util = require("util")
local http = require "http"
local json = require('json')
`)
	result, err := ParseSource(src, "test.lua", "lua", lang.Default.TreeSitter("lua"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"util", "http", "json"} {
		if findImport(result.Imports, want) == nil {
			debugParseResult(t, result)
			t.Fatalf("expected require %q", want)
		}
	}
}

func TestFeatureLuaRefs(t *testing.T) {
	src := []byte(`local util = require("util")

util.debug("ok")
helper(21)
M:new({})
`)
	result, err := ParseSource(src, "test.lua", "lua", lang.Default.TreeSitter("lua"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"debug", "helper", "new"} {
		if findRef(result.Refs, want) == nil {
			debugParseResult(t, result)
			t.Fatalf("expected ref %q", want)
		}
	}
	// require should not emit a call ref (it's an import)
	if findRef(result.Refs, "require") != nil {
		debugParseResult(t, result)
		t.Fatal("require should be an import, not a ref")
	}
}

// --- Bash Feature Tests ---

func TestFeatureBashSymbols(t *testing.T) {
	src := []byte(`#!/usr/bin/env bash

function greet() {
  local name="$1"
  echo "hello, $name"
}

run_task() {
  greet "$1"
}
`)
	result, err := ParseSource(src, "test.sh", "bash", lang.Default.TreeSitter("bash"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"greet", "run_task"} {
		if findSymbolKind(result.Symbols, want, "function") == nil {
			debugParseResult(t, result)
			t.Fatalf("expected function %q", want)
		}
	}
}

func TestFeatureBashImports(t *testing.T) {
	src := []byte(`#!/usr/bin/env bash
source ./lib/common.sh
. ./lib/util.sh
source "./lib/quoted.sh"
`)
	result, err := ParseSource(src, "test.sh", "bash", lang.Default.TreeSitter("bash"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"./lib/common.sh", "./lib/util.sh", "./lib/quoted.sh"} {
		if findImport(result.Imports, want) == nil {
			debugParseResult(t, result)
			t.Fatalf("expected source %q", want)
		}
	}
}

func TestFeatureBashRefs(t *testing.T) {
	src := []byte(`#!/usr/bin/env bash

run_task() {
  greet "$1"
  deploy "$1" || rollback
}

run_task "world"
`)
	result, err := ParseSource(src, "test.sh", "bash", lang.Default.TreeSitter("bash"))
	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"greet", "deploy", "rollback", "run_task"} {
		if findRef(result.Refs, want) == nil {
			debugParseResult(t, result)
			t.Fatalf("expected ref %q", want)
		}
	}
	// shell builtins should not appear as refs
	for _, skip := range []string{"source", ".", "local", "echo"} {
		// echo isn't in our ignore list, but source/./local are — verify those.
		if skip == "echo" {
			continue
		}
		if findRef(result.Refs, skip) != nil {
			debugParseResult(t, result)
			t.Fatalf("shell builtin %q should not appear as a ref", skip)
		}
	}
}
