package cmd

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/1broseidon/cymbal/index"
)

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
