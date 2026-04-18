package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/1broseidon/cymbal/walker"
	"github.com/spf13/cobra"
)

var showCmd = &cobra.Command{
	Use:   "show <symbol|file[:L1-L2]> [symbol2 ...]",
	Short: "Read source by symbol name or file path",
	Long: `Show source code for a symbol or file.

If the argument contains '/' or ends with a known extension, it's treated as a file path.
Otherwise, it's treated as a symbol name.

Multi-symbol mode: pass more than one symbol, or pipe newline-separated names
via --stdin, and show will render each one under a "═══ <name> ═══" header.
JSON mode returns a map keyed by the requested name so agents can dispatch
multiple reads in a single turn.

Examples:
  cymbal show ParseFile                        # show symbol source
  cymbal show internal/index/store.go          # show full file
  cymbal show internal/index/store.go:80-120   # show lines 80-120
  cymbal show Foo Bar Baz                      # batch: three symbols at once
  cymbal outline big.go -s --names | cymbal show --stdin`,
	Args: cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)
		jsonOut := getJSONFlag(cmd)
		ctx, _ := cmd.Flags().GetInt("context")
		showAll, _ := cmd.Flags().GetBool("all")
		includes, _ := cmd.Flags().GetStringArray("path")
		excludes, _ := cmd.Flags().GetStringArray("exclude")

		targets, err := collectSymbols(cmd, args)
		if err != nil {
			return err
		}

		// JSON multi mode: return a map keyed by the requested target name.
		if jsonOut && len(targets) > 1 {
			return showMultiJSON(dbPath, targets, ctx, showAll, includes, excludes)
		}

		multi := len(targets) > 1
		anyOK := false
		for i, target := range targets {
			if multi {
				multiSymbolBanner(target, i == 0)
				multiSymbolHeader(target)
			}
			var err error
			if isFilePath(target) {
				err = showFile(target, ctx, jsonOut)
			} else {
				err = showSymbol(dbPath, target, ctx, jsonOut, showAll, includes, excludes)
			}
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", target, err)
				continue
			}
			anyOK = true
		}
		if !anyOK && len(targets) > 0 {
			return fmt.Errorf("no requested symbol or file resolved")
		}
		return nil
	},
}

func init() {
	showCmd.Flags().IntP("context", "C", 0, "lines of context around the target")
	showCmd.Flags().Bool("all", false, "show all matching symbol definitions")
	showCmd.Flags().StringArray("path", nil, "include only results whose path matches this glob (repeatable)")
	showCmd.Flags().StringArray("exclude", nil, "exclude results whose path matches this glob (repeatable)")
	addStdinFlag(showCmd)
	rootCmd.AddCommand(showCmd)
}

// showMultiJSON renders multi-symbol JSON output keyed by each requested name.
// Files, symbol hits, and not-founds all live in one object so an agent can
// dispatch in a single turn and handle partial failures cleanly.
func showMultiJSON(dbPath string, targets []string, ctx int, showAll bool, includes, excludes []string) error {
	out := make(map[string]any, len(targets))
	for _, target := range targets {
		if isFilePath(target) {
			payload, err := buildShowFilePayload(target, ctx)
			if err != nil {
				out[target] = map[string]any{"error": err.Error()}
				continue
			}
			out[target] = payload
			continue
		}
		payload, err := buildShowSymbolPayload(dbPath, target, ctx, showAll, includes, excludes)
		if err != nil {
			out[target] = map[string]any{"error": err.Error()}
			continue
		}
		out[target] = payload
	}
	return writeJSON(out)
}

// buildShowFilePayload returns the same data showFile would print, minus the
// side-effectful stdout write. Used by multi-symbol JSON mode.
func buildShowFilePayload(target string, ctx int) (any, error) {
	path, startLine, endLine := parseFileTarget(target)
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("file not found: %s", path)
	}
	defer f.Close()

	if startLine > 0 && ctx > 0 {
		startLine = max(1, startLine-ctx)
		endLine = endLine + ctx
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
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return map[string]any{"file": absPath, "lines": lines}, nil
}

// buildShowSymbolPayload mirrors showSymbol's JSON shape.
func buildShowSymbolPayload(dbPath, name string, ctx int, showAll bool, includes, excludes []string) (any, error) {
	res, err := flexResolve(dbPath, name)
	if err != nil {
		return nil, err
	}
	allResults := filterByPath(res.Results, func(r index.SymbolResult) string { return r.RelPath }, includes, excludes)
	if len(allResults) == 0 {
		return nil, fmt.Errorf("symbol not found: %s", name)
	}
	displayResults := allResults
	if !showAll {
		displayResults = allResults[:1]
	}
	payload := make([]map[string]any, 0, len(displayResults))
	for i, sym := range displayResults {
		lines, _, startLine, endLine, _, truncated, err := readSymbolLines(sym, ctx)
		if err != nil {
			return nil, err
		}
		item := map[string]any{
			"symbol": sym,
			"file":   sym.File,
			"lines":  lines,
		}
		if truncated {
			item["range"] = fmt.Sprintf("%s:%d-%d", sym.File, startLine, endLine)
			item["truncated"] = true
		}
		if i == 0 {
			if len(allResults) > 1 {
				item["match_count"] = len(allResults)
				item["also"] = allResults[1:]
			}
			if res.Fuzzy {
				item["fuzzy"] = true
			}
		}
		payload = append(payload, item)
	}
	if showAll {
		return payload, nil
	}
	return payload[0], nil
}

// isFilePath returns true if the target looks like a file path (not file:Symbol).
func isFilePath(target string) bool {
	if idx := strings.LastIndex(target, ":"); idx > 0 {
		suffix := target[idx+1:]
		if len(suffix) > 0 && suffix[0] != 'L' && (suffix[0] < '0' || suffix[0] > '9') {
			return false
		}
		target = target[:idx]
	}
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

type lineEntry struct {
	Line    int    `json:"line"`
	Content string `json:"content"`
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

	if startLine > 0 && ctx > 0 {
		startLine = max(1, startLine-ctx)
		endLine = endLine + ctx
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
	if err := scanner.Err(); err != nil {
		return err
	}

	if jsonOut {
		return writeJSON(map[string]any{
			"file":  absPath,
			"lines": lines,
		})
	}

	var content strings.Builder
	for _, l := range lines {
		content.WriteString(l.Content)
		content.WriteByte('\n')
	}

	loc := absPath
	if startLine > 0 {
		loc = fmt.Sprintf("%s:%d-%d", absPath, startLine, endLine)
	}
	frontmatter([]kv{{"file", loc}}, content.String())
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

func readSymbolLines(sym index.SymbolResult, ctx int) ([]lineEntry, string, int, int, int, bool, error) {
	startLine := sym.StartLine
	endLine := sym.EndLine
	if ctx > 0 {
		startLine = max(1, startLine-ctx)
		endLine = endLine + ctx
	}
	totalLines := sym.EndLine - sym.StartLine + 1
	truncated := false
	if isTypeKind(sym.Kind) && totalLines > maxTypeShowLines {
		endLine = startLine + maxTypeShowLines - 1
		truncated = true
	}

	f, err := os.Open(sym.File)
	if err != nil {
		return nil, "", 0, 0, 0, false, fmt.Errorf("file not found: %s", sym.File)
	}
	defer f.Close()

	var lines []lineEntry
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
		text := scanner.Text()
		lines = append(lines, lineEntry{Line: lineNum, Content: text})
		content.WriteString(text)
		content.WriteByte('\n')
	}
	if err := scanner.Err(); err != nil {
		return nil, "", 0, 0, 0, false, err
	}
	if truncated {
		fmt.Fprintf(&content, "\n... (%d more lines — use cymbal show %s:%d-%d for full source)\n",
			totalLines-maxTypeShowLines, sym.RelPath, sym.StartLine, sym.EndLine)
	}
	return lines, content.String(), startLine, endLine, totalLines, truncated, nil
}

func renderShowMeta(sym index.SymbolResult, allResults []index.SymbolResult, fuzzy bool, indexInResults int) []kv {
	meta := []kv{
		{"symbol", sym.Name},
		{"kind", sym.Kind},
		{"file", fmt.Sprintf("%s:%d", sym.RelPath, sym.StartLine)},
	}
	if len(allResults) > 1 {
		also := make([]string, 0, max(0, len(allResults)-1))
		for i, r := range allResults {
			if i == indexInResults {
				continue
			}
			also = append(also, fmt.Sprintf("%s:%d", r.RelPath, r.StartLine))
		}
		meta = append(meta, kv{"matches", fmt.Sprintf("%d (also: %s)", len(allResults), strings.Join(also, ", "))})
	}
	if fuzzy {
		meta = append(meta, kv{"fuzzy", "true"})
	}
	return meta
}

func showSymbol(dbPath, name string, ctx int, jsonOut, showAll bool, includes, excludes []string) error {
	res, err := flexResolve(dbPath, name)
	if err != nil {
		return err
	}

	allResults := filterByPath(res.Results, func(r index.SymbolResult) string { return r.RelPath }, includes, excludes)
	if len(allResults) == 0 {
		return fmt.Errorf("symbol not found: %s", name)
	}
	displayResults := allResults
	if !showAll {
		displayResults = allResults[:1]
	}

	if jsonOut {
		payload := make([]map[string]any, 0, len(displayResults))
		for i, sym := range displayResults {
			lines, _, startLine, endLine, _, truncated, err := readSymbolLines(sym, ctx)
			if err != nil {
				return err
			}
			item := map[string]any{
				"symbol": sym,
				"file":   sym.File,
				"lines":  lines,
			}
			if truncated {
				item["range"] = fmt.Sprintf("%s:%d-%d", sym.File, startLine, endLine)
				item["truncated"] = true
			}
			if i == 0 {
				if len(allResults) > 1 {
					item["match_count"] = len(allResults)
					item["also"] = allResults[1:]
				}
				if res.Fuzzy {
					item["fuzzy"] = true
				}
			}
			payload = append(payload, item)
		}
		if showAll {
			return writeJSON(payload)
		}
		return writeJSON(payload[0])
	}

	for i, sym := range displayResults {
		_, content, _, _, _, _, err := readSymbolLines(sym, ctx)
		if err != nil {
			return err
		}
		frontmatter(renderShowMeta(sym, allResults, res.Fuzzy, i), content)
		if showAll && i < len(displayResults)-1 {
			fmt.Println()
		}
	}
	return nil
}
