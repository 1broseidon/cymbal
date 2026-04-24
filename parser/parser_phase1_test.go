package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/1broseidon/cymbal/lang"
	"github.com/1broseidon/cymbal/symbols"
)

func requireSymbol(t *testing.T, result *symbols.ParseResult, name, kind string) {
	t.Helper()
	if findSymbolKind(result.Symbols, name, kind) == nil {
		debugParseResult(t, result)
		t.Fatalf("expected %s symbol %q", kind, name)
	}
}

func requireImport(t *testing.T, result *symbols.ParseResult, raw string) {
	t.Helper()
	if findImport(result.Imports, raw) == nil {
		debugParseResult(t, result)
		t.Fatalf("expected import %q", raw)
	}
}

func requireRef(t *testing.T, result *symbols.ParseResult, name, kind string) {
	t.Helper()
	for _, ref := range result.Refs {
		if ref.Name == name && ref.Kind == kind {
			return
		}
	}
	debugParseResult(t, result)
	t.Fatalf("expected %s ref %q", kind, name)
}

func TestPhase1ParserEntryPoints(t *testing.T) {
	if !SupportedLanguage("go") {
		t.Fatal("go should be supported")
	}
	if SupportedLanguage("not-a-language") {
		t.Fatal("unknown languages should not be supported")
	}

	src := []byte("package main\n\nfunc FromBytes() {}\n")
	fromBytes, err := ParseBytes(src, "bytes.go", "go")
	if err != nil {
		t.Fatal(err)
	}
	requireSymbol(t, fromBytes, "FromBytes", "function")

	dir := t.TempDir()
	path := filepath.Join(dir, "file.go")
	if err := os.WriteFile(path, []byte("package main\n\nfunc FromFile() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fromFile, err := ParseFile(path, "go")
	if err != nil {
		t.Fatal(err)
	}
	requireSymbol(t, fromFile, "FromFile", "function")

	if _, err := ParseBytes(src, "bytes.unknown", "not-a-language"); err == nil {
		t.Fatal("expected unsupported language error from ParseBytes")
	}
	if _, err := ParseFile(filepath.Join(dir, "missing.go"), "go"); err == nil {
		t.Fatal("expected missing file error from ParseFile")
	}
}

func TestPhase1HCLSymbols(t *testing.T) {
	src := []byte(`terraform {
  required_version = ">= 1.5"
}

provider "aws" {
  region = var.region
}

resource "aws_instance" "web" {
  ami = data.aws_ami.ubuntu.id
}

module "network" {
  source = "./network"
}

variable "region" {}
output "instance_id" {
  value = aws_instance.web.id
}
locals {
  env = "test"
}
`)
	result := parseOrFail(t, src, "main.tf", "hcl")

	requireSymbol(t, result, "terraform", "module")
	requireSymbol(t, result, "aws", "module")
	requireSymbol(t, result, "aws_instance.web", "resource")
	requireSymbol(t, result, "network", "module")
	requireSymbol(t, result, "region", "variable")
	requireSymbol(t, result, "instance_id", "variable")
	requireSymbol(t, result, "locals", "variable")
}

func TestPhase1ProtobufSymbolsAndImports(t *testing.T) {
	src := []byte(`syntax = "proto3";
package example.v1;

import "google/protobuf/timestamp.proto";

message User {
  string name = 1;
}

enum Status {
  STATUS_UNSPECIFIED = 0;
}

service UserService {
  rpc GetUser (User) returns (User);
}
`)
	result := parseOrFail(t, src, "user.proto", "protobuf")

	requireImport(t, result, "google/protobuf/timestamp.proto")
	requireSymbol(t, result, "User", "struct")
	requireSymbol(t, result, "Status", "enum")
	requireSymbol(t, result, "UserService", "interface")
	requireSymbol(t, result, "GetUser", "method")
}

func TestPhase1RubySymbolsImportsRefsAndImplements(t *testing.T) {
	src := []byte(`require "json"
require_relative "../lib/helper"

module Billing
  class Invoice < ApplicationRecord
    include Payable
    extend Searchable
    prepend Audited

    def total
      Money.new
    end

    def self.build
      new
    end
  end
end
`)
	result := parseOrFail(t, src, "invoice.rb", "ruby")

	requireImport(t, result, "json")
	requireImport(t, result, "../lib/helper")
	requireSymbol(t, result, "Billing", "module")
	requireSymbol(t, result, "Invoice", "class")
	requireSymbol(t, result, "total", "method")
	requireSymbol(t, result, "build", "method")
	requireRef(t, result, "new", symbols.RefKindCall)

	targets := implementsTargets(result.Refs)
	for _, want := range []string{"ApplicationRecord", "Payable", "Searchable", "Audited"} {
		if !hasTarget(targets, want) {
			debugParseResult(t, result)
			t.Fatalf("expected Ruby implements edge to %q; got %v", want, targets)
		}
	}
}

func TestPhase1ElixirSymbolsImportsAndRefs(t *testing.T) {
	src := []byte(`defmodule MyApp.Invoice do
  alias MyApp.Money
  import Enum
  use GenServer
  require Logger

  def total(invoice) do
    Enum.map(invoice.lines, & &1.amount)
    notify(invoice)
  end

  defp notify(_invoice), do: :ok
  defmacro traced(expr), do: expr
  defprotocol Renderable do
  end
end
`)
	result := parseOrFail(t, src, "invoice.ex", "elixir")

	requireImport(t, result, "MyApp.Money")
	requireImport(t, result, "Enum")
	requireImport(t, result, "GenServer")
	requireImport(t, result, "Logger")
	requireSymbol(t, result, "MyApp.Invoice", "module")
	requireSymbol(t, result, "total", "function")
	requireSymbol(t, result, "notify", "function")
	requireSymbol(t, result, "traced", "macro")
	requireSymbol(t, result, "Renderable", "interface")
	requireRef(t, result, "map", symbols.RefKindCall)
	requireRef(t, result, "notify", symbols.RefKindCall)
}

func TestPhase1ScalaSymbolsRefsAndImplements(t *testing.T) {
	src := []byte(`package example

import scala.concurrent.Future

trait Named

class UserRepo extends BaseRepo with Repository with Named {
  def find(id: String): Future[String] = Future.successful(id)
  val timeout = 30
}

object UserRepo {
  def apply(): UserRepo = new UserRepo()
}
`)
	result := parseOrFail(t, src, "UserRepo.scala", "scala")

	requireImport(t, result, "scala.concurrent.Future")
	requireSymbol(t, result, "Named", "interface")
	requireSymbol(t, result, "UserRepo", "class")
	requireSymbol(t, result, "find", "method")
	requireSymbol(t, result, "timeout", "field")
	requireRef(t, result, "successful", symbols.RefKindCall)

	targets := implementsTargets(result.Refs)
	for _, want := range []string{"BaseRepo", "Repository", "Named"} {
		if !hasTarget(targets, want) {
			debugParseResult(t, result)
			t.Fatalf("expected Scala implements edge to %q; got %v", want, targets)
		}
	}
}

func TestPhase1GenericClassifierFallback(t *testing.T) {
	src := []byte(`class Fallback {
  method() {}
}

function helper() {}
`)
	result, err := ParseSource(src, "fallback.custom", "custom", lang.Default.TreeSitter("javascript"))
	if err != nil {
		t.Fatal(err)
	}
	requireSymbol(t, result, "Fallback", "class")
	requireSymbol(t, result, "method", "method")
	requireSymbol(t, result, "helper", "function")
}
