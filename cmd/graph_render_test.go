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

func captureStderr(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stderr: %v", err)
	}
	os.Stderr = w
	defer func() { os.Stderr = old }()

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
