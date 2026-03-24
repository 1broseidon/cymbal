package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// writeJSON writes a versioned JSON envelope to stdout.
func writeJSON(data any) error {
	envelope := map[string]any{
		"version": "0.1",
		"results": data,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope)
}

// frontmatter writes YAML-style frontmatter followed by content.
// Keys are printed in the order provided.
func frontmatter(meta []kv, content string) {
	fmt.Println("---")
	for _, m := range meta {
		fmt.Printf("%s: %s\n", m.k, m.v)
	}
	fmt.Println("---")
	if content != "" {
		fmt.Print(content)
		// Ensure trailing newline.
		if !strings.HasSuffix(content, "\n") {
			fmt.Println()
		}
	}
}

// kv is an ordered key-value pair for frontmatter output.
type kv struct {
	k, v string
}

// refLine is a single reference with file, line, and source text.
type refLine struct {
	relPath string
	line    int
	text    string
}

// dedupRefLines groups identical source text per file.
// Returns formatted lines ready to print and the number of unique groups.
func dedupRefLines(refs []refLine) ([]string, int) {
	type key struct{ path, text string }
	type group struct {
		path  string
		text  string
		lines []int
	}

	seen := make(map[key]*group)
	var order []key

	for _, r := range refs {
		k := key{r.relPath, r.text}
		if g, ok := seen[k]; ok {
			g.lines = append(g.lines, r.line)
		} else {
			seen[k] = &group{path: r.relPath, text: r.text, lines: []int{r.line}}
			order = append(order, k)
		}
	}

	var out []string
	for _, k := range order {
		g := seen[k]
		if len(g.lines) == 1 {
			out = append(out, fmt.Sprintf("%s:%d: %s", g.path, g.lines[0], g.text))
		} else {
			out = append(out, fmt.Sprintf("%s (%d sites): %s", g.path, len(g.lines), g.text))
		}
	}
	return out, len(order)
}

// readSourceLine reads a single line from a file on disk.
// Returns the trimmed-right content or "" on error.
func readSourceLine(path string, lineNum int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	cur := 0
	for scanner.Scan() {
		cur++
		if cur == lineNum {
			return scanner.Text()
		}
	}
	return ""
}
