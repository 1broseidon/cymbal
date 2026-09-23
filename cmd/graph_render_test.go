package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

func TestBuildImportersGraph(t *testing.T) {
	results := []index.ImporterResult{
		{File: "a.go", RelPath: "a.go", Import: "b", Depth: 1, Parent: ""},
		{File: "b.go", RelPath: "b.go", Import: "c", Depth: 2, Parent: "a.go"},
	}
	graph := buildImportersGraph("b", results)
	if len(graph.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(graph.Nodes))
	}
	if len(graph.Edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(graph.Edges))
	}
	hasAToB := false
	for _, e := range graph.Edges {
		if e.From == index.GraphNodeIDFor("file\x1fa.go") && e.To == index.GraphNodeIDFor("file\x1fb") {
			hasAToB = true
		}
	}
	if !hasAToB {
		t.Fatalf("expected a->b edge, got %+v", graph.Edges)
	}
}

func TestBuildImplsGraph(t *testing.T) {
	results := []index.ImplementorResult{
		{Implementer: "MyType", Target: "MyInterface", File: "a.go", Line: 10, RelPath: "a.go", Resolved: true},
		{Implementer: "OtherType", Target: "ExtInterface", File: "b.go", Line: 20, RelPath: "b.go", Resolved: false},
	}
	graph := buildImplsGraph("MyInterface", false, results, true)
	if len(graph.Nodes) != 4 { // root + 2 impls + 1 external
		t.Fatalf("expected 4 nodes, got %d", len(graph.Nodes))
	}
	if len(graph.Edges) != 2 {
		t.Fatalf("expected 2 edges, got %d", len(graph.Edges))
	}
	if len(graph.Unresolved) != 1 {
		t.Fatalf("expected 1 unresolved, got %d", len(graph.Unresolved))
	}

	inverse := buildImplsGraph("MyType", true, results, true)
	hasMyTypeToMyInterface := false
	for _, e := range inverse.Edges {
		if e.From == index.GraphNodeIDFor("sym-root\x1fMyType") && e.To == index.GraphNodeIDFor("sym-target\x1fMyInterface") {
			hasMyTypeToMyInterface = true
		}
	}
	if !hasMyTypeToMyInterface {
		t.Fatalf("expected MyType->MyInterface in inverse graph, got %+v", inverse.Edges)
	}
}

func TestRenderGraphEmptyFormats(t *testing.T) {
	empty := &index.GraphResult{Nodes: []index.GraphNode{}, Edges: []index.GraphEdge{}, Unresolved: []index.GraphUnresolved{}}
	if got := renderGraphMermaid(empty); got != "flowchart LR\n%% no edges\n" {
		t.Fatalf("unexpected empty mermaid: %q", got)
	}
	if got := renderGraphDOT(empty); got != "digraph cymbal { /* no edges */ }\n" {
		t.Fatalf("unexpected empty dot: %q", got)
	}
}

func TestRenderGraphFormatsUnresolvedAsDashed(t *testing.T) {
	graph := &index.GraphResult{
		Nodes:      []index.GraphNode{{ID: "n1", Label: "Entry"}, {ID: "n2", Label: "ext:fmt.Printf"}},
		Edges:      []index.GraphEdge{{From: "n1", To: "n2", Resolved: false}},
		Unresolved: []index.GraphUnresolved{{From: "n1", To: "n2", Key: "fmt.Printf", Reason: index.GraphUnresolvedExternal}},
	}
	if got := renderGraphMermaid(graph); !strings.Contains(got, "-.->") {
		t.Fatalf("expected dashed mermaid edge, got %q", got)
	}
	if got := renderGraphDOT(graph); !strings.Contains(got, "style=dashed") {
		t.Fatalf("expected dashed dot edge, got %q", got)
	}
}

func TestRenderGraphJSONEnvelope(t *testing.T) {
	graph := &index.GraphResult{Nodes: []index.GraphNode{}, Edges: []index.GraphEdge{}, Unresolved: []index.GraphUnresolved{}}
	stdout := captureStdout(t, func() {
		if err := renderGraph(index.GraphFormatJSON, graph); err != nil {
			t.Fatal(err)
		}
	})
	var payload map[string]any
	if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
		t.Fatalf("expected valid json output: %v", err)
	}
}

func TestSelectGraphFormatFromVerbHonorsGraphFormatAndJSON(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("graph-format", "", "")
	cmd.Flags().Bool("json", false, "")
	if got := selectGraphFormatFromVerb(cmd); got != index.GraphFormatJSON {
		t.Fatalf("expected non-tty default json in tests, got %q", got)
	}
	_ = cmd.Flags().Set("json", "true")
	if got := selectGraphFormatFromVerb(cmd); got != index.GraphFormatJSON {
		t.Fatalf("expected --json to force json, got %q", got)
	}
	_ = cmd.Flags().Set("json", "false")
	_ = cmd.Flags().Set("graph-format", "dot")
	if got := selectGraphFormatFromVerb(cmd); got != index.GraphFormatDot {
		t.Fatalf("expected explicit graph-format override, got %q", got)
	}
}

func TestGraphDepthOrDefaultUsesGraphOverrideOnlyWhenDepthUnset(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Int("depth", 2, "")
	if got := graphDepthOrDefault(cmd, 1); got != 1 {
		t.Fatalf("expected graph-specific default depth, got %d", got)
	}
	_ = cmd.Flags().Set("depth", "4")
	if got := graphDepthOrDefault(cmd, 1); got != 4 {
		t.Fatalf("expected explicit depth to win, got %d", got)
	}
}

func TestTruncateResultByDegreeKeepsRootAndAddsSentinel(t *testing.T) {
	g := &index.GraphResult{
		Nodes: []index.GraphNode{
			{ID: "root", Label: "Root"},
			{ID: "a", Label: "A"},
			{ID: "b", Label: "B"},
			{ID: "c", Label: "C"},
		},
		Edges: []index.GraphEdge{
			{From: "root", To: "a", Resolved: true},
			{From: "a", To: "b", Resolved: true},
			{From: "a", To: "c", Resolved: true},
		},
		Unresolved: []index.GraphUnresolved{{From: "c", Key: "fmt.Printf", Reason: index.GraphUnresolvedExternal}},
	}
	truncated := truncateResultByDegree(g, 2, map[string]bool{"root": true})
	if truncated.Truncated != 2 {
		t.Fatalf("expected 2 truncated nodes, got %d", truncated.Truncated)
	}
	if len(truncated.Nodes) != 3 {
		t.Fatalf("expected 2 kept nodes + sentinel, got %+v", truncated.Nodes)
	}
	if truncated.Nodes[len(truncated.Nodes)-1].Kind != index.GraphNodeKindSentinel {
		t.Fatalf("expected sentinel node, got %+v", truncated.Nodes)
	}
	foundRoot := false
	for _, n := range truncated.Nodes {
		if n.ID == "root" {
			foundRoot = true
		}
	}
	if !foundRoot {
		t.Fatalf("expected root to be preserved, got %+v", truncated.Nodes)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = old }()

	outC := make(chan string, 1)
	go func() {
		var buf bytes.Buffer
		_, _ = io.Copy(&buf, r)
		outC <- buf.String()
	}()

	fn()
	_ = w.Close()
	return <-outC
}

func TestRenderGraphMermaidShowsAmbiguityCue(t *testing.T) {
	g := &index.GraphResult{
		Nodes: []index.GraphNode{
			{ID: "n1", Kind: index.GraphNodeKindSymbol, Label: "Root", Symbol: "Root"},
			{ID: "n2", Kind: index.GraphNodeKindSymbol, Label: "Dup", Symbol: "Dup", DefinitionCount: 2,
				Definitions: []index.GraphDefinition{{Path: "a.go", Language: "go", StartLine: 6}, {Path: "b.go", Language: "go", StartLine: 3}}},
		},
		Edges: []index.GraphEdge{{From: "n1", To: "n2", Kind: index.GraphEdgeKindCall, Resolved: true}},
	}
	out := renderGraphMermaid(g)
	if !strings.Contains(out, `"Dup (2 defs)"`) {
		t.Fatalf("expected ambiguity cue 'Dup (2 defs)' in mermaid, got:\n%s", out)
	}
	if !strings.Contains(out, `"Root"`) {
		t.Fatalf("expected unannotated Root label, got:\n%s", out)
	}
}
