package cmd

import (
	"bufio"
	"os"
	"sort"
	"strings"
)

type sourceLocation struct {
	file string
	line int
}

// sourceSnippet is shared by text and JSON rendering. Its position is internal;
// only the existing context field is exposed in JSON.
type sourceSnippet struct {
	Context   []string `json:"context,omitempty"`
	startLine int
}

func (s sourceSnippet) refLine(relPath string, line int) refLine {
	text := ""
	if offset := line - s.startLine; offset >= 0 && offset < len(s.Context) {
		text = strings.TrimSpace(s.Context[offset])
	}
	return refLine{relPath: relPath, line: line, text: text, contextLines: s.Context, contextStart: s.startLine}
}

type sourceWindow struct{ start, end int }

// readSourceSnippets opens each requested file once, scans only as far as the
// last requested line, and retains only requested ranges. Nothing survives the
// operation, so a later command always sees edits to the source file.
func readSourceSnippets(locations []sourceLocation, ctx int) []sourceSnippet {
	ctx = max(ctx, 0)
	out := make([]sourceSnippet, len(locations))
	byFile := make(map[string][]int)
	for i, loc := range locations {
		out[i] = sourceSnippet{Context: []string{""}, startLine: loc.line}
		byFile[loc.file] = append(byFile[loc.file], i)
	}
	for path, indices := range byFile {
		windows := make([]sourceWindow, 0, len(indices))
		for _, i := range indices {
			line := locations[i].line
			windows = append(windows, sourceWindow{max(line-ctx, 1), line + ctx})
		}
		lines := readSourceWindows(path, mergeSourceWindows(windows))
		for _, i := range indices {
			line := locations[i].line
			start := max(line-ctx, 1)
			var context []string
			for n := start; n <= line+ctx; n++ {
				text, ok := lines[n]
				if !ok {
					break
				}
				context = append(context, text)
			}
			if len(context) > 0 {
				out[i] = sourceSnippet{Context: context, startLine: start}
			}
		}
	}
	return out
}

func mergeSourceWindows(windows []sourceWindow) []sourceWindow {
	sort.Slice(windows, func(i, j int) bool { return windows[i].start < windows[j].start })
	merged := windows[:0]
	for _, window := range windows {
		if len(merged) > 0 && window.start <= merged[len(merged)-1].end+1 {
			last := &merged[len(merged)-1]
			last.end = max(last.end, window.end)
		} else {
			merged = append(merged, window)
		}
	}
	return merged
}

func readSourceWindows(path string, windows []sourceWindow) map[int]string {
	f, err := os.Open(path)
	if err != nil {
		return nil // Source enrichment remains best-effort for missing files.
	}
	defer f.Close()
	lines := make(map[int]string)
	scanner := bufio.NewScanner(f)
	window := 0
	for line := 1; window < len(windows) && scanner.Scan(); line++ {
		if line >= windows[window].start {
			lines[line] = strings.TrimRight(scanner.Text(), " \t")
		}
		if line >= windows[window].end {
			window++
		}
	}
	return lines
}
