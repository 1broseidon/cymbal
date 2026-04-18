package index

import (
	"testing"
	"time"

	"github.com/1broseidon/cymbal/symbols"
)

// TestFindImplementorsResolvedVsExternal covers the core behavior of the
// implements relationship: a local class conforms to a local protocol
// (Resolved=true) and a local class conforms to an external framework
// protocol not declared anywhere in the index (Resolved=false). Both show
// up under FindImplementors, with correct Implementer resolution via the
// enclosing-symbol line range.
func TestFindImplementorsResolvedVsExternal(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	// File 1: the local protocol declaration.
	fid1, err := store.UpsertFile("/repo/Named.swift", "Named.swift", "swift", "h1", now, 50)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSymbols(fid1, []symbols.Symbol{
		{Name: "Named", Kind: "protocol", File: "/repo/Named.swift", StartLine: 1, EndLine: 3, Language: "swift"},
	}); err != nil {
		t.Fatal(err)
	}

	// File 2: two classes, one conforming to the local "Named" protocol, one
	// to an external "LiveActivityIntent" that is NOT declared in the index.
	fid2, err := store.UpsertFile("/repo/Types.swift", "Types.swift", "swift", "h2", now, 120)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSymbols(fid2, []symbols.Symbol{
		{Name: "TimerIntent", Kind: "class", File: "/repo/Types.swift", StartLine: 1, EndLine: 10, Language: "swift"},
		{Name: "NamedTimer", Kind: "class", File: "/repo/Types.swift", StartLine: 12, EndLine: 20, Language: "swift"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRefs(fid2, []symbols.Ref{
		{Name: "LiveActivityIntent", Line: 1, Language: "swift", Kind: symbols.RefKindImplements},
		{Name: "Named", Line: 12, Language: "swift", Kind: symbols.RefKindImplements},
		// Not an implements edge — must not appear.
		{Name: "LiveActivityIntent", Line: 5, Language: "swift", Kind: symbols.RefKindUse},
	}); err != nil {
		t.Fatal(err)
	}

	// Resolved=false: external protocol.
	ext, err := store.FindImplementors("LiveActivityIntent", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(ext) != 1 {
		t.Fatalf("expected 1 implementor of LiveActivityIntent, got %d (%+v)", len(ext), ext)
	}
	if ext[0].Implementer != "TimerIntent" {
		t.Errorf("expected implementer=TimerIntent, got %q", ext[0].Implementer)
	}
	if ext[0].Resolved {
		t.Errorf("expected Resolved=false for external protocol, got true")
	}

	// Resolved=true: local protocol declared in file 1.
	local, err := store.FindImplementors("Named", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(local) != 1 {
		t.Fatalf("expected 1 implementor of Named, got %d (%+v)", len(local), local)
	}
	if local[0].Implementer != "NamedTimer" {
		t.Errorf("expected implementer=NamedTimer, got %q", local[0].Implementer)
	}
	if !local[0].Resolved {
		t.Errorf("expected Resolved=true for local protocol, got false")
	}
}

// TestFindImplementsInverse verifies the --of direction: given a type, list
// what it implements. Only implements-kind edges inside the type's line range
// should be returned, not call-kind or use-kind refs.
func TestFindImplementsInverse(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/Repo.ts", "Repo.ts", "typescript", "h", now, 80)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "UserRepo", Kind: "class", File: "/repo/Repo.ts", StartLine: 1, EndLine: 10, Language: "typescript"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRefs(fid, []symbols.Ref{
		{Name: "BaseRepo", Line: 1, Language: "typescript", Kind: symbols.RefKindImplements},
		{Name: "IUserRepository", Line: 1, Language: "typescript", Kind: symbols.RefKindImplements},
		{Name: "someCall", Line: 5, Language: "typescript", Kind: symbols.RefKindCall},
	}); err != nil {
		t.Fatal(err)
	}

	edges, err := store.FindImplements("UserRepo", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(edges) != 2 {
		t.Fatalf("expected 2 implements edges, got %d (%+v)", len(edges), edges)
	}
	found := map[string]bool{}
	for _, e := range edges {
		found[e.Target] = true
		if e.Implementer != "UserRepo" {
			t.Errorf("expected Implementer=UserRepo, got %q", e.Implementer)
		}
	}
	if !found["BaseRepo"] || !found["IUserRepository"] {
		t.Errorf("expected BaseRepo + IUserRepository, got %+v", found)
	}
}

// TestFindTraceDefaultFiltersToCallKind is the regression for the Swift noise
// problem: type mentions (Kind=use) must not surface as trace edges by default.
func TestFindTraceDefaultFiltersToCallKind(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/a.swift", "a.swift", "swift", "h", now, 80)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "load", Kind: "function", File: "/repo/a.swift", StartLine: 1, EndLine: 10, Language: "swift"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRefs(fid, []symbols.Ref{
		// real call inside load() — should surface
		{Name: "fetch", Line: 3, Language: "swift", Kind: symbols.RefKindCall},
		// type mentions — MUST NOT surface in default trace
		{Name: "UUID", Line: 2, Language: "swift", Kind: symbols.RefKindUse},
		{Name: "Date", Line: 4, Language: "swift", Kind: symbols.RefKindUse},
		// implements edges at the declaration line — also must not surface
		{Name: "Sendable", Line: 1, Language: "swift", Kind: symbols.RefKindImplements},
	}); err != nil {
		t.Fatal(err)
	}

	// Default: only "call" edges.
	traces, err := store.FindTrace("load", 2, 50)
	if err != nil {
		t.Fatal(err)
	}
	for _, tr := range traces {
		if tr.Callee == "UUID" || tr.Callee == "Date" || tr.Callee == "Sendable" {
			t.Errorf("default trace should not surface non-call edge %q (got %+v)", tr.Callee, tr)
		}
	}
	var sawFetch bool
	for _, tr := range traces {
		if tr.Callee == "fetch" {
			sawFetch = true
		}
	}
	if !sawFetch {
		t.Errorf("expected trace to include 'fetch' call; got %+v", traces)
	}

	// Opt-in: widen to call + use, and the type mentions reappear.
	wide, err := store.FindTrace("load", 2, 50, symbols.RefKindCall, symbols.RefKindUse)
	if err != nil {
		t.Fatal(err)
	}
	var sawUUID bool
	for _, tr := range wide {
		if tr.Callee == "UUID" {
			sawUUID = true
		}
	}
	if !sawUUID {
		t.Errorf("expected widened trace to include UUID; got %+v", wide)
	}
}
