package index

import (
	"testing"
	"time"

	"github.com/1broseidon/cymbal/symbols"
)

func TestBuildGraphSymbolModeDirectionsAndStableIDs(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	fid, err := store.UpsertFile("/repo/app/main.go", "app/main.go", "go", "h1", now, 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.InsertSymbols(fid, []symbols.Symbol{
		{Name: "Entry", Kind: "function", File: "/repo/app/main.go", StartLine: 1, EndLine: 20, Language: "go"},
		{Name: "Helper", Kind: "function", File: "/repo/app/main.go", StartLine: 21, EndLine: 30, Language: "go"},
		{Name: "Leaf", Kind: "function", File: "/repo/app/main.go", StartLine: 31, EndLine: 40, Language: "go"},
		{Name: "Caller", Kind: "function", File: "/repo/app/main.go", StartLine: 41, EndLine: 50, Language: "go"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.InsertRefs(fid, []symbols.Ref{
		{Name: "Helper", Line: 2, Language: "go", Kind: symbols.RefKindCall},
		{Name: "Leaf", Line: 22, Language: "go", Kind: symbols.RefKindCall},
		{Name: "Entry", Line: 42, Language: "go", Kind: symbols.RefKindCall},
	}); err != nil {
		t.Fatal(err)
	}

	down, err := store.BuildGraph(GraphQuery{Symbol: "Entry", Direction: GraphDirectionDown, Depth: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(down.Edges) != 2 {
		t.Fatalf("expected 2 downward edges, got %+v", down.Edges)
	}
	if down.Nodes[0].ID != graphNodeID(down.Nodes[0].Symbol) {
		t.Fatalf("expected stable node hash id, got %+v", down.Nodes[0])
	}

	up, err := store.BuildGraph(GraphQuery{Symbol: "Entry", Direction: GraphDirectionUp, Depth: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(up.Edges) != 1 || up.Edges[0].To != graphNodeID("Entry") {
		t.Fatalf("expected upward caller edge into Entry, got %+v", up.Edges)
	}

	both, err := store.BuildGraph(GraphQuery{Symbol: "Entry", Direction: GraphDirectionBoth, Depth: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(both.Edges) != 3 {
		t.Fatalf("expected merged up+down edges, got %+v", both.Edges)
	}
}

func TestBuildGraphSymbolModeScopeExcludeDepthAndUnresolved(t *testing.T) {
	store, _ := newTestStore(t)
	now := time.Now()

	appID, _ := store.UpsertFile("/repo/app/main.go", "app/main.go", "go", "h1", now, 100)
	libID, _ := store.UpsertFile("/repo/lib/lib.go", "lib/lib.go", "go", "h2", now, 100)
	_ = store.InsertSymbols(appID, []symbols.Symbol{{Name: "Entry", Kind: "function", File: "/repo/app/main.go", StartLine: 1, EndLine: 20, Language: "go"}})
	_ = store.InsertSymbols(libID, []symbols.Symbol{{Name: "Helper", Kind: "function", File: "/repo/lib/lib.go", StartLine: 1, EndLine: 20, Language: "go"}})
	_ = store.InsertRefs(appID, []symbols.Ref{{Name: "Helper", Line: 2, Language: "go", Kind: symbols.RefKindCall}, {Name: "fmt.Printf", Line: 3, Language: "go", Kind: symbols.RefKindCall}})

	graph, err := store.BuildGraph(GraphQuery{Symbol: "Entry", Direction: GraphDirectionDown, Depth: 99, Scope: []string{"app/*"}, IncludeUnresolved: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Edges) != 1 {
		t.Fatalf("expected scoped graph to keep only root->external dashed edge, got %+v", graph.Edges)
	}
	if len(graph.Unresolved) != 1 || graph.Unresolved[0].ResolvedAs != "ext:fmt.Printf" {
		t.Fatalf("expected unresolved ext node, got %+v", graph.Unresolved)
	}

	excluded, err := store.BuildGraph(GraphQuery{Symbol: "Entry", Direction: GraphDirectionDown, Depth: 2, Exclude: []string{"app/*"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(excluded.Nodes) != 0 || len(excluded.Edges) != 0 {
		t.Fatalf("expected exclude to hard-cut graph, got %+v", excluded)
	}
}

func TestBuildGraphEmptyGraphIsWellFormed(t *testing.T) {
	store, _ := newTestStore(t)
	graph, err := store.BuildGraph(GraphQuery{Symbol: "Missing", Direction: GraphDirectionDown})
	if err != nil {
		t.Fatal(err)
	}
	if graph.Nodes == nil || graph.Edges == nil || graph.Unresolved == nil {
		t.Fatalf("expected empty slices, got %+v", graph)
	}
	if len(graph.Nodes) != 0 || len(graph.Edges) != 0 || len(graph.Unresolved) != 0 {
		t.Fatalf("expected empty graph, got %+v", graph)
	}
}

func TestBuildGraphLimitAddsSentinelAndTruncatedCount(t *testing.T) {
	graph := &GraphResult{
		Nodes: []GraphNode{
			{ID: graphNodeID("Entry"), Kind: GraphNodeKindSymbol, Label: "Entry", Symbol: "Entry"},
			{ID: graphNodeID("A"), Kind: GraphNodeKindSymbol, Label: "A", Symbol: "A"},
			{ID: graphNodeID("B"), Kind: GraphNodeKindSymbol, Label: "B", Symbol: "B"},
			{ID: graphNodeID("C"), Kind: GraphNodeKindSymbol, Label: "C", Symbol: "C"},
		},
		Edges: []GraphEdge{
			{From: graphNodeID("Entry"), To: graphNodeID("A"), Resolved: true},
			{From: graphNodeID("A"), To: graphNodeID("B"), Resolved: true},
			{From: graphNodeID("A"), To: graphNodeID("C"), Resolved: true},
		},
		Unresolved: []GraphUnresolved{{From: graphNodeID("C"), Key: "fmt.Printf", Reason: GraphUnresolvedExternal}},
	}
	truncated := truncateByDegree(graph, 2, graphNodeID("Entry"))
	if truncated.Truncated != 2 {
		t.Fatalf("expected 2 truncated nodes, got %d", truncated.Truncated)
	}
	if len(truncated.Nodes) != 3 {
		t.Fatalf("expected 2 kept nodes + sentinel, got %+v", truncated.Nodes)
	}
	foundSentinel := false
	foundEntry := false
	for _, n := range truncated.Nodes {
		if n.Kind == GraphNodeKindSentinel {
			foundSentinel = true
		}
		if n.Symbol == "Entry" {
			foundEntry = true
		}
	}
	if !foundSentinel || !foundEntry {
		t.Fatalf("expected sentinel and root preserved, got %+v", truncated.Nodes)
	}
}
