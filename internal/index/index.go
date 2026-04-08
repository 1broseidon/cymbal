package index

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/1broseidon/cymbal/internal/parser"
	"github.com/1broseidon/cymbal/internal/summarize"
	"github.com/1broseidon/cymbal/internal/symbols"
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
	FilesSkipped int // unchanged (mtime match)
	FilesUnsup   int // unsupported language
	ParseErrors  int // parser.ParseFile failures
	WriteErrors  int // DB write failures
	SymbolsFound int
	StaleRemoved int
	Summarized   int
	Errors       int // total errors (ParseErrors + WriteErrors)
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

// cymbalDir returns the base directory for cymbal data.
// Prefers os.UserCacheDir (%LOCALAPPDATA% on Windows, ~/.cache on Linux,
// ~/Library/Caches on macOS), falls back to ~/.cymbal.
func cymbalDir() (string, error) {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "cymbal"), nil
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".cymbal"), nil
	}
	return "", fmt.Errorf("cannot determine home or cache directory")
}

// RepoDBPath computes the per-repo database path: <cymbalDir>/repos/<hash>/index.db
// where hash is the first 16 hex chars of SHA-256 of the absolute repo root path.
func RepoDBPath(repoRoot string) (string, error) {
	base, err := cymbalDir()
	if err != nil {
		return "", err
	}
	h := sha256.Sum256([]byte(repoRoot))
	hash := hex.EncodeToString(h[:8]) // 16 hex chars
	return filepath.Join(base, "repos", hash, "index.db"), nil
}

// FindGitRoot walks up from dir to find the nearest .git directory or
// .git file (worktrees have a .git file containing "gitdir: <path>").
func FindGitRoot(dir string) (string, error) {
	d := dir
	for {
		dotGit := filepath.Join(d, ".git")
		info, err := os.Stat(dotGit)
		if err == nil {
			if info.IsDir() {
				return d, nil
			}
			// Worktree: .git is a file containing "gitdir: <path>".
			if !info.IsDir() && info.Mode().IsRegular() {
				return d, nil
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return "", fmt.Errorf("no git repository found from %s", dir)
}

// parseResult holds the output of a parse worker.
type parseResult struct {
	entry    walker.FileEntry
	hash     string
	result   *symbols.ParseResult
	rowCount int // symbols + imports + refs for batching decisions
}

// flushBatch writes a batch of parse results to the DB in a single transaction.
// Prepares statements once for the entire batch (not per file).
// Uses SAVEPOINTs per file so a single failure doesn't corrupt the batch.
// Stats are published only after successful commit.
func flushBatch(store *Store, batch []parseResult, indexed, found, writeErrs *atomic.Int64) {
	if len(batch) == 0 {
		return
	}
	tx, err := store.db.Begin()
	if err != nil {
		writeErrs.Add(int64(len(batch)))
		return
	}

	stmts, err := PrepareBatchStmts(tx)
	if err != nil {
		tx.Rollback()
		writeErrs.Add(int64(len(batch)))
		return
	}
	defer stmts.Close()

	type fileStats struct{ symbolCount int }
	committed := make([]fileStats, 0, len(batch))

	for i, pr := range batch {
		sp := fmt.Sprintf("sp_%d", i)
		if _, err := tx.Exec("SAVEPOINT " + sp); err != nil {
			writeErrs.Add(1)
			continue
		}

		err := InsertFileAllStmts(stmts, pr.entry.Path, pr.entry.RelPath,
			pr.entry.Language, pr.hash, pr.entry.ModTime, pr.entry.Size,
			pr.result.Symbols, pr.result.Imports, pr.result.Refs)
		if err != nil {
			tx.Exec("ROLLBACK TO " + sp)
			writeErrs.Add(1)
			continue
		}

		if _, err := tx.Exec("RELEASE " + sp); err != nil {
			tx.Exec("ROLLBACK TO " + sp)
			writeErrs.Add(1)
			continue
		}

		committed = append(committed, fileStats{symbolCount: len(pr.result.Symbols)})
	}

	if err := tx.Commit(); err != nil {
		writeErrs.Add(int64(len(committed)))
	} else {
		for _, fs := range committed {
			indexed.Add(1)
			found.Add(int64(fs.symbolCount))
		}
	}
}

// Index indexes all source files under root.
// If dbPath is empty, it is auto-computed from root using RepoDBPath.
func Index(root, dbPath string, opts Options) (*Stats, error) {
	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.NumCPU()
	}

	if dbPath == "" {
		var err error
		dbPath, err = RepoDBPath(root)
		if err != nil {
			return nil, fmt.Errorf("computing db path: %w", err)
		}
	}

	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("opening store: %w", err)
	}
	defer store.Close()

	// Store repo root in metadata.
	if err := store.SetMeta("repo_root", root); err != nil {
		return nil, fmt.Errorf("setting repo metadata: %w", err)
	}

	files, err := walker.Walk(root, workers, parser.SupportedLanguage)
	if err != nil {
		return nil, fmt.Errorf("walking directory: %w", err)
	}

	// Load all stored mtime_ns + size in one query for fast skip checks.
	fileChecks, _ := store.AllFileChecks()
	if fileChecks == nil {
		fileChecks = make(map[string]FileCheck)
	}

	// Phase 0: prune stale files (deleted/renamed since last index).
	currentPaths := make(map[string]struct{}, len(files))
	for _, f := range files {
		currentPaths[f.Path] = struct{}{}
	}
	staleRemoved, _ := store.DeleteStalePaths(currentPaths)

	// (parseResult is defined at package level)

	var (
		indexed   atomic.Int64
		unchanged atomic.Int64
		unsup     atomic.Int64
		parseErrs atomic.Int64
		found     atomic.Int64
		writeErrs atomic.Int64
	)

	// processed tracks total files done (for progress).
	processed := func() int64 {
		return indexed.Load() + unchanged.Load() + unsup.Load() + parseErrs.Load()
	}
	_ = processed // used in progress goroutine

	totalFiles := len(files)

	parseCh := make(chan walker.FileEntry, 256)
	resultCh := make(chan parseResult, 256)

	// Phase 1: parse workers — CPU-bound, fully parallel.
	// Each worker reads the file once, parses from those bytes, and hashes
	// from the same buffer — eliminating duplicate I/O and allocation.
	var parseWg sync.WaitGroup
	for range workers {
		parseWg.Add(1)
		go func() {
			defer parseWg.Done()
			for f := range parseCh {
				if !parser.SupportedLanguage(f.Language) {
					unsup.Add(1)
					continue
				}

				if !opts.Force {
					if fc, ok := fileChecks[f.Path]; ok && fc.MtimeNs == f.ModTime.UnixNano() && fc.Size == f.Size {
						unchanged.Add(1)
						continue
					}
				}

				// Read once — parse and hash from same bytes.
				src, err := os.ReadFile(f.Path)
				if err != nil {
					parseErrs.Add(1)
					continue
				}

				result, err := parser.ParseBytes(src, f.Path, f.Language)
				if err != nil {
					parseErrs.Add(1)
					continue
				}

				// Only compute hash when there's a stored entry to compare against.
				var hash string
				if _, exists := fileChecks[f.Path]; exists {
					hash = HashBytes(src)
				}

				rows := len(result.Symbols) + len(result.Imports) + len(result.Refs)
				resultCh <- parseResult{entry: f, hash: hash, result: result, rowCount: rows}
			}
		}()
	}

	// Close resultCh when all parsers finish.
	go func() {
		parseWg.Wait()
		close(resultCh)
	}()

	// Feed files to parse workers.
	go func() {
		for _, f := range files {
			parseCh <- f
		}
		close(parseCh)
	}()

	progressDone := startProgress(totalFiles, &indexed, &unchanged, &found)

	// Phase 2: serial writer — batched transactions, no lock contention.
	// Flush when file count >= 100 OR total rows (symbols+imports+refs) >= 50k.
	// This prevents pathological batches from symbol-dense repos.
	const (
		maxBatchFiles = 100
		maxBatchRows  = 50_000
	)
	var batch []parseResult
	batchRows := 0

	for pr := range resultCh {
		batch = append(batch, pr)
		batchRows += pr.rowCount
		if len(batch) >= maxBatchFiles || batchRows >= maxBatchRows {
			flushBatch(store, batch, &indexed, &found, &writeErrs)
			batch = batch[:0]
			batchRows = 0
		}
	}
	flushBatch(store, batch, &indexed, &found, &writeErrs)

	close(progressDone)

	pe := int(parseErrs.Load())
	we := int(writeErrs.Load())
	stats := &Stats{
		FilesIndexed: int(indexed.Load()),
		FilesSkipped: int(unchanged.Load()),
		FilesUnsup:   int(unsup.Load()),
		ParseErrors:  pe,
		WriteErrors:  we,
		SymbolsFound: int(found.Load()),
		StaleRemoved: staleRemoved,
		Errors:       pe + we,
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

// Structure returns a structural overview of the codebase.
func Structure(dbPath string, limit int) (*StructureResult, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.Structure(limit)
}

// EnsureFresh performs a silent, incremental reindex before a query.
// It opens the DB, reads the stored repo root, and runs the standard
// incremental index pass (mtime+size check, parse only dirty files,
// prune stale). Returns the number of files refreshed, or 0 if
// everything was already current. Errors are intentionally swallowed —
// a stale read is better than a failed query.
//
// If the DB does not exist yet, EnsureFresh auto-indexes from the
// current working directory's git root (issue #3).
func EnsureFresh(dbPath string) int {
	store, err := OpenStore(dbPath)
	if err != nil {
		return 0
	}

	repoRoot, err := store.GetMeta("repo_root")
	store.Close()

	if err != nil || repoRoot == "" {
		// No repo_root in DB — this is a fresh/empty database.
		// Auto-index from cwd's git root.
		repoRoot = autoDetectRoot()
		if repoRoot == "" {
			return 0
		}
		fmt.Fprintf(os.Stderr, "Building index for %s ...\n", repoRoot)
	}

	stats, err := Index(repoRoot, dbPath, Options{})
	if err != nil {
		return 0
	}
	return stats.FilesIndexed + stats.StaleRemoved
}

// autoDetectRoot resolves the git root from cwd for auto-indexing.
func autoDetectRoot() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	root, err := FindGitRoot(cwd)
	if err != nil {
		return ""
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return ""
	}
	return abs
}

// startProgress launches a goroutine that prints indexing progress to stderr.
// It only activates after 10s to avoid flicker on small repos.
// Close the returned channel to stop it.
func startProgress(total int, indexed, skipped, found *atomic.Int64) chan struct{} {
	done := make(chan struct{})
	go func() {
		select {
		case <-time.After(10 * time.Second):
		case <-done:
			return
		}
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				n := indexed.Load() + skipped.Load()
				pct := float64(n) / float64(total) * 100
				fmt.Fprintf(os.Stderr, "\r  [%d/%d] %.1f%% — %d symbols found",
					n, total, pct, found.Load())
			case <-done:
				fmt.Fprintf(os.Stderr, "\r%80s\r", "")
				return
			}
		}
	}()
	return done
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

// Repo holds info about an indexed repo (used for listing all repos).
type Repo struct {
	Path        string `json:"path"`
	FileCount   int    `json:"file_count"`
	SymbolCount int    `json:"symbol_count"`
	DBPath      string `json:"db_path"`
}

// ListRepos scans ~/.cymbal/repos/*/index.db and returns info for each.
func ListRepos() ([]Repo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	pattern := filepath.Join(home, ".cymbal", "repos", "*", "index.db")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	var repos []Repo
	for _, dbPath := range matches {
		store, err := OpenStore(dbPath)
		if err != nil {
			continue
		}
		stats, err := store.RepoStats()
		store.Close()
		if err != nil || stats.Path == "" {
			continue
		}
		repos = append(repos, Repo{
			Path:        stats.Path,
			FileCount:   stats.FileCount,
			SymbolCount: stats.SymbolCount,
			DBPath:      dbPath,
		})
	}
	return repos, nil
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

// RepoStats returns overview statistics for the repo in the given database.
func RepoStats(dbPath string) (*RepoStatsResult, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.RepoStats()
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

// InvestigateResult is a kind-adaptive response that returns
// the right shape of information based on what the symbol is.
type InvestigateResult struct {
	Symbol  SymbolResult   `json:"symbol"`
	Source  string         `json:"source"`
	Kind    string         `json:"investigate_kind"`  // "function", "type", "module"
	Refs    []RefResult    `json:"refs,omitempty"`    // callers/usages (functions)
	Impact  []ImpactResult `json:"impact,omitempty"`  // transitive callers (functions)
	Members []SymbolResult `json:"members,omitempty"` // methods/fields (types)
	Outline []SymbolResult `json:"outline,omitempty"` // file overview (when symbol is a file-level type)
}

// Investigate returns kind-adaptive context for a symbol.
// The symbol's kind (from the index) drives which data is included:
//   - function/method: source + refs + shallow impact
//   - class/struct/type/interface: source + members + importers-as-refs
//   - ambiguous: returns AmbiguousError with ranked candidates
//
// InvestigateOpts controls symbol resolution for Investigate.
type InvestigateOpts struct {
	FileHint string // filter matches to symbols in this file path (substring match)
}

func Investigate(dbPath, symbolName string, opts ...InvestigateOpts) (*InvestigateResult, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	results, err := store.SearchSymbols(symbolName, "", "", true, 100)
	if err != nil {
		return nil, err
	}

	// Apply file hint filter if provided.
	if len(opts) > 0 && opts[0].FileHint != "" {
		hint := opts[0].FileHint
		var filtered []SymbolResult
		for _, r := range results {
			if strings.HasSuffix(r.RelPath, hint) || strings.Contains(r.RelPath, hint) {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) > 0 {
			results = filtered
		}
	}

	if len(results) == 0 {
		return nil, fmt.Errorf("symbol not found: %s", symbolName)
	}
	if len(results) > 1 {
		return nil, &AmbiguousError{Name: symbolName, Matches: results}
	}

	sym := results[0]
	source := readLines(sym.File, sym.StartLine, sym.EndLine)

	res := &InvestigateResult{
		Symbol: sym,
		Source: source,
	}

	switch sym.Kind {
	case "function", "method":
		res.Kind = "function"
		res.Refs, _ = store.FindReferences(sym.Name, 20)
		res.Impact, _ = store.FindImpact(sym.Name, 2, 20)

	case "class", "struct", "type", "interface", "trait", "enum", "object", "mixin", "extension":
		res.Kind = "type"
		res.Members, _ = store.ChildSymbols(sym.Name, 50)
		// For types, show who references the type name.
		res.Refs, _ = store.FindReferences(sym.Name, 20)

	default:
		// Unknown kind — return source + refs as best effort.
		res.Kind = sym.Kind
		res.Refs, _ = store.FindReferences(sym.Name, 20)
	}

	return res, nil
}

// InvestigateResolved builds an InvestigateResult for a pre-resolved symbol.
// Use when the caller already resolved the symbol (e.g., via flexResolve).
func InvestigateResolved(dbPath string, sym SymbolResult) (*InvestigateResult, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	// Cap source for large type symbols — members are listed separately.
	const maxTypeLines = 60
	srcEnd := sym.EndLine
	truncated := false
	switch sym.Kind {
	case "class", "struct", "type", "interface", "trait", "enum", "object", "mixin", "extension":
		if srcEnd-sym.StartLine+1 > maxTypeLines {
			srcEnd = sym.StartLine + maxTypeLines - 1
			truncated = true
		}
	}

	source := readLines(sym.File, sym.StartLine, srcEnd)
	if truncated {
		source += fmt.Sprintf("\n... (%d more lines — see cymbal show %s:%d-%d)\n",
			sym.EndLine-srcEnd, sym.RelPath, sym.StartLine, sym.EndLine)
	}
	res := &InvestigateResult{
		Symbol: sym,
		Source: source,
	}

	switch sym.Kind {
	case "function", "method":
		res.Kind = "function"
		res.Refs, _ = store.FindReferences(sym.Name, 20)
		res.Impact, _ = store.FindImpact(sym.Name, 2, 20)
	case "class", "struct", "type", "interface", "trait", "enum", "object", "mixin", "extension":
		res.Kind = "type"
		res.Members, _ = store.ChildSymbols(sym.Name, 50)
		res.Refs, _ = store.FindReferences(sym.Name, 20)
	default:
		res.Kind = sym.Kind
		res.Refs, _ = store.FindReferences(sym.Name, 20)
	}

	return res, nil
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

// FindTrace performs downward call graph traversal for a symbol.
func FindTrace(dbPath, symbolName string, depth, limit int) ([]TraceResult, error) {
	if limit <= 0 {
		limit = 50
	}
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	return store.FindTrace(symbolName, depth, limit)
}

// SearchSymbolsFlex performs a flexible search: case-insensitive + prefix match.
// Used as a fallback when exact name match returns no results.
func SearchSymbolsFlex(dbPath, name string, limit int) ([]SymbolResult, error) {
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()

	// Try case-insensitive exact match first.
	results, err := store.SearchSymbolsCI(name, limit)
	if err != nil {
		return nil, err
	}
	if len(results) > 0 {
		return results, nil
	}

	// Fall back to FTS prefix match.
	return store.SearchSymbols(name, "", "", false, limit)
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
