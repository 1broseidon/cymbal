package index

import (
	"bytes"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
	"sync"
)

// TextSearchOptions selects regex matching and repo-relative path constraints.
// Without Regexp, the query remains a literal substring for API compatibility.
type TextSearchOptions struct {
	Regexp bool
	Paths  PathFilter
}

// TextSearch searches indexed file contents for a literal substring.
func TextSearch(dbPath, query, language string, limit int) ([]TextResult, error) {
	return TextSearchWithOptions(dbPath, query, language, limit, TextSearchOptions{})
}

// TextSearchWithOptions searches eligible indexed files, filtering before limiting.
// Results are ordered by repo-relative path and line regardless of worker scheduling.
func TextSearchWithOptions(dbPath, query, language string, limit int, opts TextSearchOptions) ([]TextResult, error) {
	needle := []byte(query)
	match := func(line []byte) bool { return bytes.Contains(line, needle) }
	if opts.Regexp {
		re, err := regexp.Compile(query)
		if err != nil {
			return nil, fmt.Errorf("invalid search pattern: %w", err)
		}
		match = re.Match
	}
	files, err := TextFiles(dbPath, language, opts.Paths)
	if err != nil {
		return nil, err
	}
	return searchTextFiles(files, match, limit)
}

type textFileResult struct {
	matches []TextResult
	err     error
}

// Bound concurrent reads and merge in file order so a limit selects the same
// matches on every run. Each file buffers at most the remaining result budget.
func searchTextFiles(files []FileInfo, match func([]byte) bool, limit int) ([]TextResult, error) {
	if limit <= 0 {
		limit = 50
	}
	workers := max(1, runtime.NumCPU())
	var results []TextResult
	for start := 0; start < len(files); start += workers {
		batch := files[start:min(start+workers, len(files))]
		remaining := limit - len(results)
		scanned := make([]textFileResult, len(batch))
		var wg sync.WaitGroup
		for i, file := range batch {
			wg.Go(func() { scanned[i].matches, scanned[i].err = scanTextFile(file, match, remaining) })
		}
		wg.Wait()
		for _, result := range scanned {
			if result.err != nil {
				return nil, result.err
			}
			results = append(results, result.matches...)
			if len(results) >= limit {
				return results[:limit], nil
			}
		}
	}
	return results, nil
}

func scanTextFile(file FileInfo, match func([]byte) bool, limit int) ([]TextResult, error) {
	f, err := os.Open(file.Path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", file.RelPath, err)
	}
	defer f.Close()
	scanner := newLargeLineScanner(f)
	var results []TextResult
	for line := 1; scanner.Scan(); line++ {
		if !match(scanner.Bytes()) {
			continue
		}
		results = append(results, NewTextResult(file, line, scanner.Text()))
		if len(results) >= limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", file.RelPath, err)
	}
	return results, nil
}

// NewTextResult normalizes snippets identically for the native and ripgrep readers.
func NewTextResult(file FileInfo, line int, text string) TextResult {
	snippet := strings.TrimSpace(text)
	if len(snippet) > 200 {
		snippet = snippet[:200]
	}
	return TextResult{File: file.Path, RelPath: file.RelPath, Line: line, Snippet: snippet}
}
