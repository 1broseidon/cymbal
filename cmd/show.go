package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/1broseidon/cymbal/internal/walker"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show <symbol|file[:L1-L2]>",
	Short: "Read source by symbol name or file path",
	Long: `Show source code for a symbol or file.

If the argument contains '/' or ends with a known extension, it's treated as a file path.
Otherwise, it's treated as a symbol name.

Examples:
  cymbal show ParseFile              # show symbol source
  cymbal show internal/index/store.go     # show full file
  cymbal show internal/index/store.go:80-120  # show lines 80-120
  cymbal show Foo Bar Baz                # batch: show multiple symbols`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)
		jsonOut := getJSONFlag(cmd)
		ctx, _ := cmd.Flags().GetInt("context")

		for i, target := range args {
			if i > 0 {
				fmt.Println()
			}
			var err error
			if isFilePath(target) {
				err = showFile(target, ctx, jsonOut)
			} else {
				err = showSymbol(dbPath, target, ctx, jsonOut)
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", target, err)
			}
		}
		return nil
	},
}

func init() {
	showCmd.Flags().IntP("context", "C", 0, "lines of context around the target")
	rootCmd.AddCommand(showCmd)
}

// isFilePath returns true if the target looks like a file path (not file:Symbol).
func isFilePath(target string) bool {
	// Check for "file:suffix" pattern.
	if idx := strings.LastIndex(target, ":"); idx > 0 {
		suffix := target[idx+1:]
		// If suffix starts with a letter (not a digit or 'L'), it's file:Symbol — not a file path.
		if len(suffix) > 0 && suffix[0] != 'L' && (suffix[0] < '0' || suffix[0] > '9') {
			return false
		}
		// Strip :L1-L2 or :123-456 suffix before checking extension.
		target = target[:idx]
	}
	// Contains path separator — treat as file.
	if strings.Contains(target, "/") {
		return true
	}
	return walker.LangForFile(target) != ""
}

// parseFileTarget parses "file.go:100-150" into path, start, end.
func parseFileTarget(target string) (string, int, int) {
	idx := strings.LastIndex(target, ":")
	if idx <= 0 {
		return target, 0, 0
	}

	path := target[:idx]
	rangeStr := target[idx+1:]

	parts := strings.SplitN(rangeStr, "-", 2)
	// Strip optional "L" prefix (e.g., "L119-L132" or "L119").
	p0 := strings.TrimPrefix(parts[0], "L")
	start, err := strconv.Atoi(p0)
	if err != nil {
		return target, 0, 0
	}

	end := start
	if len(parts) == 2 {
		p1 := strings.TrimPrefix(parts[1], "L")
		if e, err := strconv.Atoi(p1); err == nil {
			end = e
		}
	}
	return path, start, end
}

func showFile(target string, ctx int, jsonOut bool) error {
	path, startLine, endLine := parseFileTarget(target)

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	f, err := os.Open(absPath)
	if err != nil {
		return fmt.Errorf("file not found: %s", path)
	}
	defer f.Close()

	// Apply context.
	if startLine > 0 && ctx > 0 {
		startLine = max(1, startLine-ctx)
		endLine = endLine + ctx
	}

	type lineEntry struct {
		Line    int    `json:"line"`
		Content string `json:"content"`
	}
	var lines []lineEntry

	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if startLine > 0 && lineNum < startLine {
			continue
		}
		if endLine > 0 && lineNum > endLine {
			break
		}
		lines = append(lines, lineEntry{Line: lineNum, Content: scanner.Text()})
	}

	if jsonOut {
		return writeJSON(map[string]any{
			"file":  absPath,
			"lines": lines,
		})
	}

	// Build content from lines.
	var content strings.Builder
	for _, l := range lines {
		content.WriteString(l.Content)
		content.WriteByte('\n')
	}

	loc := absPath
	if startLine > 0 {
		loc = fmt.Sprintf("%s:%d-%d", absPath, startLine, endLine)
	}
	frontmatter([]kv{
		{"file", loc},
	}, content.String())
	return nil
}

// maxTypeShowLines caps the source shown for class/struct/type/interface
// symbols. Members are listed separately so the full body is redundant.
const maxTypeShowLines = 60

func isTypeKind(kind string) bool {
	switch kind {
	case "class", "struct", "type", "interface", "trait", "enum", "object", "mixin", "extension":
		return true
	}
	return false
}

func showSymbol(dbPath, name string, ctx int, jsonOut bool) error {
	res, err := flexResolve(dbPath, name)
	if err != nil {
		return err
	}

	if len(res.Results) == 0 {
		return fmt.Errorf("symbol not found: %s", name)
	}

	// Auto-resolve: pick the best match (first after ranking).
	sym := res.Results[0]
	startLine := sym.StartLine
	endLine := sym.EndLine
	if ctx > 0 {
		startLine = max(1, startLine-ctx)
		endLine = endLine + ctx
	}

	// Smart truncation: cap large type symbols.
	totalLines := sym.EndLine - sym.StartLine + 1
	truncated := false
	if isTypeKind(sym.Kind) && totalLines > maxTypeShowLines {
		endLine = startLine + maxTypeShowLines - 1
		truncated = true
	}

	if jsonOut {
		target := fmt.Sprintf("%s:%d-%d", sym.File, startLine, endLine)
		return showFile(target, 0, true)
	}

	// Read source and emit frontmatter format.
	f, err := os.Open(sym.File)
	if err != nil {
		return fmt.Errorf("file not found: %s", sym.File)
	}
	defer f.Close()

	var content strings.Builder
	scanner := bufio.NewScanner(f)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		if lineNum < startLine {
			continue
		}
		if lineNum > endLine {
			break
		}
		content.WriteString(scanner.Text())
		content.WriteByte('\n')
	}

	if truncated {
		fmt.Fprintf(&content, "\n... (%d more lines — use cymbal show %s:%d-%d for full source)\n",
			totalLines-maxTypeShowLines, sym.RelPath, sym.StartLine, sym.EndLine)
	}

	meta := []kv{
		{"symbol", sym.Name},
		{"kind", sym.Kind},
		{"file", fmt.Sprintf("%s:%d", sym.RelPath, sym.StartLine)},
	}
	if res.TotalFound > 1 {
		also := make([]string, 0, len(res.Results)-1)
		for _, r := range res.Results[1:] {
			also = append(also, fmt.Sprintf("%s:%d", r.RelPath, r.StartLine))
		}
		meta = append(meta, kv{"matches", fmt.Sprintf("%d (also: %s)", res.TotalFound, strings.Join(also, ", "))})
	}
	if res.Fuzzy {
		meta = append(meta, kv{"fuzzy", "true"})
	}
	frontmatter(meta, content.String())
	return nil
}
