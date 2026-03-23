package index

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/1broseidon/cymbal/internal/parser"
	"github.com/1broseidon/cymbal/internal/summarize"
	"github.com/1broseidon/cymbal/internal/walker"
)

// Options controls indexing behavior.
type Options struct {
	Workers   int
	Force     bool
	Summarize bool
	Backend   string // agent backend for summaries (empty = auto-detect)
	Model     string // model override for summaries (empty = backend default)
}

// Stats reports indexing results.
type Stats struct {
	FilesIndexed int
	FilesSkipped int
	SymbolsFound int
	Summarized   int
	Errors       int
}

// SearchQuery defines a search request.
type SearchQuery struct {
	Text     string
	Kind     string
	Language string
	Exact    bool
	Limit    int
}

// TextResult holds a text search match.
type TextResult struct {
	File    string `json:"file"`
	RelPath string `json:"rel_path"`
	Line    int    `json:"line"`
	Snippet string `json:"snippet"`
}

// RepoStatsResult holds repo overview data.
type RepoStatsResult struct {
	Path        string         `json:"path"`
	FileCount   int            `json:"file_count"`
	SymbolCount int            `json:"symbol_count"`
	Languages   map[string]int `json:"languages"`
}

// Index indexes all source files under root.
func Index(root, dbPath string, opts Options) (*Stats, error) {
	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}
	defer store.Close()

	// Remove any sub-repos that are inside this root — their files will be
	// re-indexed under this broader repo.
	subRepos, _ := store.SubRepos(root)
	for _, sub := range subRepos {
		fmt.Fprintf(os.Stderr, "Removing sub-repo %s (now covered by %s)\n", sub.Path, root)
		if err := store.RemoveRepo(sub.Path); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove sub-repo %s: %v\n", sub.Path, err)
		}
	}

	// If this path is inside an already-indexed parent repo, warn and
	// re-index from the parent root instead.
	if parent, ok, _ := store.ParentRepo(root); ok {
		fmt.Fprintf(os.Stderr, "Note: %s is inside indexed repo %s — indexing parent instead\n", root, parent.Path)
		root = parent.Path
	}

	repoID, err := store.EnsureRepo(root)
	if err != nil {
		return nil, fmt.Errorf("registering repo: %w", err)
	}

	files, err := walker.Walk(root, workers)
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	var (
		indexed atomic.Int64
		skipped atomic.Int64
		found   atomic.Int64
		errors  atomic.Int64
	)

	ch := make(chan walker.FileEntry, 256)
	var wg sync.WaitGroup

	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range ch {
				if !parser.SupportedLanguage(f.Language) {
					skipped.Add(1)
					continue
				}

				if !opts.Force {
					hash, err := HashFile(f.Path)
					if err == nil {
						existingHash, _ := store.FileHash(repoID, f.Path)
						if existingHash == hash {
							skipped.Add(1)
							continue
						}
					}
				}

				result, err := parser.ParseFile(f.Path, f.Language)
				if err != nil {
					errors.Add(1)
					continue
				}

				hash, _ := HashFile(f.Path)
				fileID, err := store.UpsertFile(repoID, f.Path, f.RelPath, f.Language, hash)
				if err != nil {
					errors.Add(1)
					continue
				}

				if err := store.InsertSymbols(fileID, result.Symbols); err != nil {
					errors.Add(1)
					continue
				}

				if err := store.InsertImports(fileID, result.Imports); err != nil {
					errors.Add(1)
					continue
				}

				if err := store.InsertRefs(fileID, result.Refs); err != nil {
					errors.Add(1)
					continue
				}

				indexed.Add(1)
				found.Add(int64(len(result.Symbols)))
			}
		}()
	}

	for _, f := range files {
		ch <- f
	}
	close(ch)
	wg.Wait()

	stats := &Stats{
		FilesIndexed: int(indexed.Load()),
		FilesSkipped: int(skipped.Load()),
		SymbolsFound: int(found.Load()),
		Errors:       int(errors.Load()),
	}

	// Summarization pass — runs after indexing is complete.
	if opts.Summarize {
		summarized, err := runSummarization(store, opts.Backend, opts.Model)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: summarization error: %v\n", err)
		}
		stats.Summarized = summarized
	}

	return stats, nil
}

// pendingSymbol holds a symbol that needs summarization.
type pendingSymbol struct {
	id        int64
	name      string
	kind      string
	startLine int
	endLine   int
	language  string
	filePath  string
}

// runSummarization generates AI summaries for symbols whose source has changed.
// Uses batched prompts (10 symbols per call) and source hashing for diff tracking.
func runSummarization(store *Store, backend, model string) (int, error) {
	s, err := summarize.New(backend, model)
	if err != nil {
		return 0, err
	}
	fmt.Fprintf(os.Stderr, "Generating summaries via %s ...\n", s.Backend())

	// Find top-level symbols that need summarization:
	// - No summary yet, OR
	// - Source hash changed (code was modified since last summary)
	rows, err := store.db.Query(`
		SELECT sym.id, sym.name, sym.kind, sym.start_line, sym.end_line, sym.language, f.path,
		       COALESCE(sym.source_hash, '')
		FROM symbols sym
		JOIN files f ON sym.file_id = f.id
		WHERE sym.kind IN ('function', 'method', 'struct', 'class', 'interface', 'trait', 'type', 'enum')
		  AND sym.depth = 0
		ORDER BY f.path, sym.start_line
	`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var items []pendingSymbol
	var currentHashes []string
	for rows.Next() {
		var p pendingSymbol
		var storedHash string
		if err := rows.Scan(&p.id, &p.name, &p.kind, &p.startLine, &p.endLine, &p.language, &p.filePath, &storedHash); err != nil {
			continue
		}

		// Read source and compute hash for diff tracking.
		source := readLines(p.filePath, p.startLine, p.endLine)
		if source == "" {
			continue
		}
		currentHash := hashString(source)

		// Skip if source unchanged since last summarization.
		if storedHash == currentHash {
			continue
		}

		items = append(items, p)
		currentHashes = append(currentHashes, currentHash)
	}

	if len(items) == 0 {
		fmt.Fprintf(os.Stderr, "All symbols up to date.\n")
		return 0, nil
	}

	fmt.Fprintf(os.Stderr, "%d symbols to summarize (batches of %d) ...\n", len(items), summarize.BatchSize())

	count := 0
	for i := 0; i < len(items); i += summarize.BatchSize() {
		end := i + summarize.BatchSize()
		if end > len(items) {
			end = len(items)
		}
		batch := items[i:end]
		batchHashes := currentHashes[i:end]

		// Build batch input.
		var inputs []summarize.SymbolInput
		for _, item := range batch {
			source := readLines(item.filePath, item.startLine, item.endLine)
			inputs = append(inputs, summarize.SymbolInput{
				Name:     item.name,
				Kind:     item.kind,
				Language: item.language,
				Source:   source,
			})
		}

		batchNum := (i / summarize.BatchSize()) + 1
		totalBatches := (len(items) + summarize.BatchSize() - 1) / summarize.BatchSize()
		fmt.Fprintf(os.Stderr, "  batch %d/%d (%d symbols) ...\n", batchNum, totalBatches, len(batch))

		results := s.SummarizeBatch(inputs)

		// Write results + update source hashes.
		for j, item := range batch {
			summary, ok := results[j]
			if !ok || summary == "" {
				continue
			}
			_, err := store.db.Exec(
				"UPDATE symbols SET summary = ?, source_hash = ? WHERE id = ?",
				summary, batchHashes[j], item.id,
			)
			if err == nil {
				count++
			}
		}
	}

	// Rebuild FTS index after bulk summary updates.
	if count > 0 {
		store.db.Exec("INSERT INTO symbols_fts(symbols_fts) VALUES('rebuild')")
	}

	fmt.Fprintf(os.Stderr, "  %d symbols summarized.\n", count)
	return count, nil
}

// hashString returns a short hash of a string for diff tracking.
func hashString(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8]) // 16 hex chars — enough for diff detection
}

// readLines reads lines startLine..endLine from a file.
func readLines(path string, startLine, endLine int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var b strings.Builder
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
		b.WriteString(scanner.Text())
		b.WriteByte('\n')
	}
	return b.String()
}

// ListRepos returns all indexed repos.
func ListRepos(dbPath string) ([]Repo, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.ListRepos()
}

// FileOutline returns symbols for a file.
func FileOutline(dbPath, filePath string) ([]SymbolResult, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.FileSymbols(filePath)
}

// SearchSymbols searches across all indexed repos.
func SearchSymbols(dbPath string, q SearchQuery) ([]SymbolResult, error) {
	if q.Limit <= 0 {
		q.Limit = 50
	}
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.SearchSymbols(q.Text, q.Kind, q.Language, q.Exact, q.Limit)
}

// ResolveRepo finds the nearest indexed repo root by walking up from cwd.
func ResolveRepo(dbPath, cwd string) (string, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return "", err
	}
	defer store.Close()
	return store.ResolveRepo(cwd)
}

// RepoStats returns overview statistics for a repo.
func RepoStats(dbPath, repoPath string) (*RepoStatsResult, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.RepoStats(repoPath)
}

// TextSearch greps indexed file contents on disk.
func TextSearch(dbPath, query, lang string, limit int) ([]TextResult, error) {
	if limit <= 0 {
		limit = 50
	}
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	files, err := store.AllFiles(lang)
	if err != nil {
		return nil, err
	}

	queryBytes := []byte(query)
	var mu sync.Mutex
	var results []TextResult
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())

	for _, f := range files {
		if len(results) >= limit {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(path, relPath string) {
			defer wg.Done()
			defer func() { <-sem }()

			file, err := os.Open(path)
			if err != nil {
				return
			}
			defer file.Close()

			scanner := bufio.NewScanner(file)
			lineNum := 0
			for scanner.Scan() {
				lineNum++
				line := scanner.Bytes()
				if bytes.Contains(line, queryBytes) {
					mu.Lock()
					if len(results) < limit {
						snippet := string(line)
						if len(snippet) > 200 {
							snippet = snippet[:200]
						}
						results = append(results, TextResult{
							File:    path,
							RelPath: relPath,
							Line:    lineNum,
							Snippet: snippet,
						})
					}
					mu.Unlock()
				}
			}
		}(f.Path, f.RelPath)
	}

	wg.Wait()
	return results, nil
}

// FindReferences finds files that reference a symbol name.
func FindReferences(dbPath, name string, limit int) ([]RefResult, error) {
	if limit <= 0 {
		limit = 50
	}
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.FindReferences(name, limit)
}

// FindImporters finds files that import the file containing a symbol.
func FindImporters(dbPath, symbolName string, depth, limit int) ([]ImporterResult, error) {
	if limit <= 0 {
		limit = 50
	}
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.FindImporters(symbolName, depth, limit)
}

// FindImportersByPath finds files that import a given file or package path directly.
func FindImportersByPath(dbPath, target string, depth, limit int) ([]ImporterResult, error) {
	if limit <= 0 {
		limit = 50
	}
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.FindImportersByPath(target, depth, limit)
}

// ContextResult bundles all context needed to understand a symbol.
type ContextResult struct {
	Symbol      SymbolResult   `json:"symbol"`
	Source      string         `json:"source"`
	TypeRefs    []SymbolResult `json:"type_refs"`
	Callers     []RefResult    `json:"callers"`
	FileImports []string       `json:"file_imports"`
}

// SymbolContext returns bundled context for a symbol: source, type refs, callers, and file imports.
func SymbolContext(dbPath, symbolName string, callerLimit int) (*ContextResult, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	// Resolve symbol by exact name.
	results, err := store.SearchSymbols(symbolName, "", "", true, 100)
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("symbol not found: %s", symbolName)
	}
	if len(results) > 1 {
		return nil, &AmbiguousError{Name: symbolName, Matches: results}
	}

	sym := results[0]

	// Read source from file.
	source := readLines(sym.File, sym.StartLine, sym.EndLine)

	// Find type-like symbols referenced in the symbol's range.
	typeRefs, err := store.TypeRefsInRange(sym.File, sym.StartLine, sym.EndLine)
	if err != nil {
		return nil, err
	}

	// Find callers.
	callers, err := store.FindReferences(sym.Name, callerLimit)
	if err != nil {
		return nil, err
	}

	// Get file imports.
	imports, err := store.FileImports(sym.File)
	if err != nil {
		return nil, err
	}

	return &ContextResult{
		Symbol:      sym,
		Source:      source,
		TypeRefs:    typeRefs,
		Callers:     callers,
		FileImports: imports,
	}, nil
}

// AmbiguousError is returned when a symbol name matches multiple symbols.
type AmbiguousError struct {
	Name    string
	Matches []SymbolResult
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("multiple matches for '%s'", e.Name)
}

// FindImpact performs transitive caller analysis for a symbol.
func FindImpact(dbPath, symbolName string, depth, limit int) ([]ImpactResult, error) {
	if limit <= 0 {
		limit = 100
	}
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.FindImpact(symbolName, depth, limit)
}

// SymbolsByName finds symbols by exact name (for show command).
func SymbolsByName(dbPath, name string) ([]SymbolResult, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.SearchSymbols(name, "", "", true, 100)
}
