package index

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/1broseidon/cymbal/internal/symbols"
)

const schema = `
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS files (
	id       INTEGER PRIMARY KEY AUTOINCREMENT,
	path     TEXT UNIQUE NOT NULL,
	rel_path TEXT NOT NULL,
	language TEXT NOT NULL,
	hash     TEXT NOT NULL,
	indexed_at DATETIME NOT NULL,
	mtime    DATETIME
);

CREATE TABLE IF NOT EXISTS symbols (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	file_id     INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
	name        TEXT NOT NULL,
	kind        TEXT NOT NULL,
	start_line  INTEGER NOT NULL,
	end_line    INTEGER NOT NULL,
	start_col   INTEGER,
	end_col     INTEGER,
	parent      TEXT,
	depth       INTEGER DEFAULT 0,
	signature   TEXT,
	summary     TEXT,
	source_hash TEXT,
	language    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS imports (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	file_id   INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
	raw_path  TEXT NOT NULL,
	language  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS refs (
	id        INTEGER PRIMARY KEY AUTOINCREMENT,
	file_id   INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
	line      INTEGER NOT NULL,
	name      TEXT NOT NULL,
	language  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_symbols_name ON symbols(name);
CREATE INDEX IF NOT EXISTS idx_symbols_kind ON symbols(kind);
CREATE INDEX IF NOT EXISTS idx_symbols_file ON symbols(file_id);
CREATE INDEX IF NOT EXISTS idx_files_path ON files(path);
CREATE INDEX IF NOT EXISTS idx_imports_raw ON imports(raw_path);
CREATE INDEX IF NOT EXISTS idx_imports_file ON imports(file_id);
CREATE INDEX IF NOT EXISTS idx_refs_name ON refs(name);
CREATE INDEX IF NOT EXISTS idx_refs_file ON refs(file_id);

CREATE VIRTUAL TABLE IF NOT EXISTS symbols_fts USING fts5(
	name,
	kind,
	summary,
	content=symbols,
	content_rowid=id
);

CREATE TRIGGER IF NOT EXISTS symbols_ai AFTER INSERT ON symbols BEGIN
	INSERT INTO symbols_fts(rowid, name, kind, summary) VALUES (new.id, new.name, new.kind, COALESCE(new.summary, ''));
END;

CREATE TRIGGER IF NOT EXISTS symbols_ad AFTER DELETE ON symbols BEGIN
	INSERT INTO symbols_fts(symbols_fts, rowid, name, kind, summary) VALUES('delete', old.id, old.name, old.kind, COALESCE(old.summary, ''));
END;
`

// Store manages the SQLite database.
type Store struct {
	db *sql.DB
}

// OpenStore opens or creates the database.
func OpenStore(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000&_foreign_keys=ON")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initializing schema: %w", err)
	}

	// Migration: add mtime column for existing databases.
	db.Exec("ALTER TABLE files ADD COLUMN mtime DATETIME")

	db.Exec("PRAGMA cache_size = -64000")
	db.Exec("PRAGMA mmap_size = 268435456")
	db.Exec("PRAGMA temp_store = MEMORY")

	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// GetMeta returns a metadata value, or empty string if not set.
func (s *Store) GetMeta(key string) (string, error) {
	var value string
	err := s.db.QueryRow("SELECT value FROM meta WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return value, err
}

// SetMeta sets a metadata key/value pair.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO meta (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = ?`,
		key, value, value,
	)
	return err
}

// FileHash returns the stored hash for a file, or empty string if not indexed.
func (s *Store) FileHash(filePath string) (string, error) {
	var hash string
	err := s.db.QueryRow("SELECT hash FROM files WHERE path = ?", filePath).Scan(&hash)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return hash, err
}

// FileMtime returns the stored modification time for a file.
func (s *Store) FileMtime(filePath string) (time.Time, error) {
	var mtime time.Time
	err := s.db.QueryRow("SELECT mtime FROM files WHERE path = ?", filePath).Scan(&mtime)
	if err != nil {
		return time.Time{}, err
	}
	return mtime, nil
}

// AllFileMtimes loads all file paths and their stored mtimes in one query.
func (s *Store) AllFileMtimes() (map[string]time.Time, error) {
	rows, err := s.db.Query("SELECT path, mtime FROM files WHERE mtime IS NOT NULL")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]time.Time)
	for rows.Next() {
		var path string
		var mtime time.Time
		if err := rows.Scan(&path, &mtime); err != nil {
			continue
		}
		m[path] = mtime
	}
	return m, nil
}

// AllStoredPaths returns all file paths currently in the index.
func (s *Store) AllStoredPaths() ([]string, error) {
	rows, err := s.db.Query("SELECT path FROM files")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			continue
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// DeleteStalePaths removes files from the index whose paths are not in the
// provided set of current paths. Returns the number of rows deleted.
func (s *Store) DeleteStalePaths(currentPaths map[string]struct{}) (int, error) {
	stored, err := s.AllStoredPaths()
	if err != nil {
		return 0, err
	}

	var stale []string
	for _, p := range stored {
		if _, ok := currentPaths[p]; !ok {
			stale = append(stale, p)
		}
	}

	if len(stale) == 0 {
		return 0, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	const batchSize = 100
	deleted := 0
	for i := 0; i < len(stale); i += batchSize {
		end := i + batchSize
		if end > len(stale) {
			end = len(stale)
		}
		batch := stale[i:end]
		placeholders := make([]string, len(batch))
		args := make([]any, len(batch))
		for j, p := range batch {
			placeholders[j] = "?"
			args[j] = p
		}
		q := "DELETE FROM files WHERE path IN (" + strings.Join(placeholders, ",") + ")"
		res, err := tx.Exec(q, args...)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		deleted += int(n)
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return deleted, nil
}

// HashFile computes SHA-256 of a file.
func HashFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:]), nil
}

// HashBytes computes SHA-256 hex string from bytes already in memory.
func HashBytes(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

// UpsertFile stores file info and returns the file ID. Clears old data via cascade.
func (s *Store) UpsertFile(filePath, relPath, lang, hash string, mtime time.Time) (int64, error) {
	now := time.Now()
	s.db.Exec("DELETE FROM files WHERE path = ?", filePath)

	res, err := s.db.Exec(
		"INSERT INTO files (path, rel_path, language, hash, indexed_at, mtime) VALUES (?, ?, ?, ?, ?, ?)",
		filePath, relPath, lang, hash, now, mtime,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// InsertSymbols batch-inserts symbols for a file.
func (s *Store) InsertSymbols(fileID int64, syms []symbols.Symbol) error {
	if len(syms) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := insertSymbolsTx(tx, fileID, syms); err != nil {
		return err
	}
	return tx.Commit()
}

func insertSymbolsTx(tx *sql.Tx, fileID int64, syms []symbols.Symbol) error {
	stmt, err := tx.Prepare(`INSERT INTO symbols
		(file_id, name, kind, start_line, end_line, start_col, end_col, parent, depth, signature, summary, language)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, sym := range syms {
		_, err := stmt.Exec(
			fileID, sym.Name, sym.Kind,
			sym.StartLine, sym.EndLine, sym.StartCol, sym.EndCol,
			sym.Parent, sym.Depth, sym.Signature, sym.Summary, sym.Language,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// InsertImports batch-inserts imports for a file.
func (s *Store) InsertImports(fileID int64, imports []symbols.Import) error {
	if len(imports) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := insertImportsTx(tx, fileID, imports); err != nil {
		return err
	}
	return tx.Commit()
}

func insertImportsTx(tx *sql.Tx, fileID int64, imports []symbols.Import) error {
	stmt, err := tx.Prepare("INSERT INTO imports (file_id, raw_path, language) VALUES (?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, imp := range imports {
		if _, err := stmt.Exec(fileID, imp.RawPath, imp.Language); err != nil {
			return err
		}
	}
	return nil
}

// InsertRefs batch-inserts refs for a file.
func (s *Store) InsertRefs(fileID int64, refs []symbols.Ref) error {
	if len(refs) == 0 {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := insertRefsTx(tx, fileID, refs); err != nil {
		return err
	}
	return tx.Commit()
}

func insertRefsTx(tx *sql.Tx, fileID int64, refs []symbols.Ref) error {
	stmt, err := tx.Prepare("INSERT INTO refs (file_id, line, name, language) VALUES (?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, ref := range refs {
		if _, err := stmt.Exec(fileID, ref.Line, ref.Name, ref.Language); err != nil {
			return err
		}
	}
	return nil
}

// InsertFileAll inserts a file and all its data (symbols, imports, refs) in a single
// transaction operation. Designed for use within an external transaction via InsertFileAllTx.
func (s *Store) InsertFileAll(filePath, relPath, lang, hash string, mtime time.Time, syms []symbols.Symbol, imports []symbols.Import, refs []symbols.Ref) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.InsertFileAllTx(tx, filePath, relPath, lang, hash, mtime, syms, imports, refs); err != nil {
		return err
	}
	return tx.Commit()
}

// InsertFileAllTx inserts a file and all its data within an existing transaction.
func (s *Store) InsertFileAllTx(tx *sql.Tx, filePath, relPath, lang, hash string, mtime time.Time, syms []symbols.Symbol, imports []symbols.Import, refs []symbols.Ref) error {
	now := time.Now()
	tx.Exec("DELETE FROM files WHERE path = ?", filePath)

	res, err := tx.Exec(
		"INSERT INTO files (path, rel_path, language, hash, indexed_at, mtime) VALUES (?, ?, ?, ?, ?, ?)",
		filePath, relPath, lang, hash, now, mtime,
	)
	if err != nil {
		return err
	}
	fileID, err := res.LastInsertId()
	if err != nil {
		return err
	}

	if err := insertSymbolsTx(tx, fileID, syms); err != nil {
		return err
	}
	if err := insertImportsTx(tx, fileID, imports); err != nil {
		return err
	}
	if err := insertRefsTx(tx, fileID, refs); err != nil {
		return err
	}
	return nil
}

// BatchStmts holds prepared statements reused across an entire batch transaction.
// This avoids preparing 3–5 statements per file (300+ prepares per 100-file batch).
// Call PrepareBatchStmts once per batch, use InsertFileAllStmts per file, then Close.
type BatchStmts struct {
	delFile   *sql.Stmt
	insFile   *sql.Stmt
	insSymbol *sql.Stmt
	insImport *sql.Stmt
	insRef    *sql.Stmt
}

// PrepareBatchStmts prepares all statements for a batch transaction.
func PrepareBatchStmts(tx *sql.Tx) (*BatchStmts, error) {
	var b BatchStmts
	var err error

	b.delFile, err = tx.Prepare("DELETE FROM files WHERE path = ?")
	if err != nil {
		return nil, err
	}
	b.insFile, err = tx.Prepare(
		"INSERT INTO files (path, rel_path, language, hash, indexed_at, mtime) VALUES (?, ?, ?, ?, ?, ?)")
	if err != nil {
		b.Close()
		return nil, err
	}
	b.insSymbol, err = tx.Prepare(`INSERT INTO symbols
		(file_id, name, kind, start_line, end_line, start_col, end_col, parent, depth, signature, summary, language)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		b.Close()
		return nil, err
	}
	b.insImport, err = tx.Prepare("INSERT INTO imports (file_id, raw_path, language) VALUES (?, ?, ?)")
	if err != nil {
		b.Close()
		return nil, err
	}
	b.insRef, err = tx.Prepare("INSERT INTO refs (file_id, line, name, language) VALUES (?, ?, ?, ?)")
	if err != nil {
		b.Close()
		return nil, err
	}
	return &b, nil
}

// Close closes all prepared statements.
func (b *BatchStmts) Close() {
	if b.delFile != nil {
		b.delFile.Close()
	}
	if b.insFile != nil {
		b.insFile.Close()
	}
	if b.insSymbol != nil {
		b.insSymbol.Close()
	}
	if b.insImport != nil {
		b.insImport.Close()
	}
	if b.insRef != nil {
		b.insRef.Close()
	}
}

// InsertFileAllStmts inserts a file and all its data using pre-prepared statements.
func InsertFileAllStmts(b *BatchStmts, filePath, relPath, lang, hash string, mtime time.Time, syms []symbols.Symbol, imports []symbols.Import, refs []symbols.Ref) error {
	now := time.Now()
	b.delFile.Exec(filePath) //nolint:errcheck

	res, err := b.insFile.Exec(filePath, relPath, lang, hash, now, mtime)
	if err != nil {
		return err
	}
	fileID, err := res.LastInsertId()
	if err != nil {
		return err
	}

	for _, sym := range syms {
		if _, err := b.insSymbol.Exec(
			fileID, sym.Name, sym.Kind,
			sym.StartLine, sym.EndLine, sym.StartCol, sym.EndCol,
			sym.Parent, sym.Depth, sym.Signature, sym.Summary, sym.Language,
		); err != nil {
			return err
		}
	}
	for _, imp := range imports {
		if _, err := b.insImport.Exec(fileID, imp.RawPath, imp.Language); err != nil {
			return err
		}
	}
	for _, ref := range refs {
		if _, err := b.insRef.Exec(fileID, ref.Line, ref.Name, ref.Language); err != nil {
			return err
		}
	}
	return nil
}

// SymbolResult holds a search result.
type SymbolResult struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	File      string `json:"file"`
	RelPath   string `json:"rel_path"`
	StartLine int    `json:"start_line"`
	EndLine   int    `json:"end_line"`
	Parent    string `json:"parent,omitempty"`
	Depth     int    `json:"depth"`
	Signature string `json:"signature,omitempty"`
	Summary   string `json:"summary,omitempty"`
	Language  string `json:"language"`
}

// SymbolID returns a stable identifier for this symbol.
func (r SymbolResult) SymbolID() string {
	return fmt.Sprintf("%s:%s:%s:%s:%d", r.RelPath, r.Language, r.Kind, r.Name, r.StartLine)
}

// SearchSymbols searches using FTS5 with ranking: exact > prefix > fuzzy.
func (s *Store) SearchSymbols(query, kind, lang string, exact bool, limit int) ([]SymbolResult, error) {
	var rows *sql.Rows
	var err error

	if exact {
		q := `SELECT s.name, s.kind, f.path, f.rel_path, s.start_line, s.end_line, s.parent, s.depth, s.signature, COALESCE(s.summary, ''), s.language
			  FROM symbols s JOIN files f ON s.file_id = f.id
			  WHERE s.name = ?`
		args := []any{query}
		if kind != "" {
			q += " AND s.kind = ?"
			args = append(args, kind)
		}
		if lang != "" {
			q += " AND s.language = ?"
			args = append(args, lang)
		}
		q += " ORDER BY s.name LIMIT ?"
		args = append(args, limit)
		rows, err = s.db.Query(q, args...)
	} else {
		ftsQuery := query + "*"
		q := `SELECT s.name, s.kind, f.path, f.rel_path, s.start_line, s.end_line, s.parent, s.depth, s.signature, COALESCE(s.summary, ''), s.language
			  FROM symbols_fts fts
			  JOIN symbols s ON fts.rowid = s.id
			  JOIN files f ON s.file_id = f.id
			  WHERE symbols_fts MATCH ?`
		args := []any{ftsQuery}
		if kind != "" {
			q += " AND s.kind = ?"
			args = append(args, kind)
		}
		if lang != "" {
			q += " AND s.language = ?"
			args = append(args, lang)
		}
		q += ` ORDER BY
			CASE WHEN s.name = ? THEN 0
			     WHEN s.name LIKE ? || '%' THEN 1
			     ELSE 2 END,
			rank
			LIMIT ?`
		args = append(args, query, query, limit)
		rows, err = s.db.Query(q, args...)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SymbolResult
	for rows.Next() {
		var r SymbolResult
		if err := rows.Scan(&r.Name, &r.Kind, &r.File, &r.RelPath, &r.StartLine, &r.EndLine, &r.Parent, &r.Depth, &r.Signature, &r.Summary, &r.Language); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// FileSymbols returns all symbols in a given file.
func (s *Store) FileSymbols(filePath string) ([]SymbolResult, error) {
	rows, err := s.db.Query(`
		SELECT s.name, s.kind, f.path, f.rel_path, s.start_line, s.end_line, s.parent, s.depth, s.signature, COALESCE(s.summary, ''), s.language
		FROM symbols s JOIN files f ON s.file_id = f.id
		WHERE f.path = ?
		ORDER BY s.start_line
	`, filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SymbolResult
	for rows.Next() {
		var r SymbolResult
		if err := rows.Scan(&r.Name, &r.Kind, &r.File, &r.RelPath, &r.StartLine, &r.EndLine, &r.Parent, &r.Depth, &r.Signature, &r.Summary, &r.Language); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// RepoStats returns overview statistics for this database.
func (s *Store) RepoStats() (*RepoStatsResult, error) {
	repoRoot, _ := s.GetMeta("repo_root")
	result := &RepoStatsResult{
		Path:      repoRoot,
		Languages: make(map[string]int),
	}

	s.db.QueryRow("SELECT COUNT(*) FROM files").Scan(&result.FileCount)
	s.db.QueryRow("SELECT COUNT(*) FROM symbols s JOIN files f ON s.file_id = f.id").Scan(&result.SymbolCount)

	rows, err := s.db.Query("SELECT language, COUNT(*) FROM files GROUP BY language ORDER BY COUNT(*) DESC")
	if err != nil {
		return result, nil
	}
	defer rows.Close()
	for rows.Next() {
		var lang string
		var count int
		if err := rows.Scan(&lang, &count); err == nil {
			result.Languages[lang] = count
		}
	}

	return result, nil
}

// FileInfo holds basic file info from the index.
type FileInfo struct {
	Path    string
	RelPath string
}

// AllFiles returns all indexed file paths, optionally filtered by language.
func (s *Store) AllFiles(lang string) ([]FileInfo, error) {
	q := "SELECT path, rel_path FROM files"
	var args []any
	if lang != "" {
		q += " WHERE language = ?"
		args = append(args, lang)
	}
	q += " ORDER BY rel_path"

	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var files []FileInfo
	for rows.Next() {
		var f FileInfo
		if err := rows.Scan(&f.Path, &f.RelPath); err != nil {
			return nil, err
		}
		files = append(files, f)
	}
	return files, rows.Err()
}

// RefResult holds a reference search result.
type RefResult struct {
	File    string `json:"file"`
	RelPath string `json:"rel_path"`
	Line    int    `json:"line"`
	Name    string `json:"name"`
}

// FindReferences finds files that reference a symbol name.
func (s *Store) FindReferences(name string, limit int) ([]RefResult, error) {
	rows, err := s.db.Query(`
		SELECT f.path, f.rel_path, r.line, r.name
		FROM refs r JOIN files f ON r.file_id = f.id
		WHERE r.name = ?
		ORDER BY f.rel_path, r.line
		LIMIT ?
	`, name, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []RefResult
	for rows.Next() {
		var r RefResult
		if err := rows.Scan(&r.File, &r.RelPath, &r.Line, &r.Name); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// ImporterResult holds a file that imports another.
type ImporterResult struct {
	File    string `json:"file"`
	RelPath string `json:"rel_path"`
	Import  string `json:"import"`
	Depth   int    `json:"depth"`
}

// FindImporters finds files that import the file(s) containing a symbol, up to depth hops.
func (s *Store) FindImporters(symbolName string, depth, limit int) ([]ImporterResult, error) {
	if depth <= 0 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}

	// First find which files define this symbol.
	symRows, err := s.db.Query(`
		SELECT DISTINCT f.rel_path
		FROM symbols s JOIN files f ON s.file_id = f.id
		WHERE s.name = ?
	`, symbolName)
	if err != nil {
		return nil, err
	}
	defer symRows.Close()

	var targetPaths []string
	for symRows.Next() {
		var p string
		if err := symRows.Scan(&p); err == nil {
			targetPaths = append(targetPaths, p)
		}
	}

	if len(targetPaths) == 0 {
		return nil, nil
	}

	// BFS through import graph.
	seen := make(map[string]bool)
	var results []ImporterResult
	currentTargets := targetPaths

	for d := 1; d <= depth && len(currentTargets) > 0; d++ {
		var nextTargets []string
		for _, target := range currentTargets {
			// Find files whose imports contain this target path.
			pattern := "%" + strings.TrimSuffix(filepath.Base(target), filepath.Ext(target)) + "%"
			rows, err := s.db.Query(`
				SELECT DISTINCT f.path, f.rel_path, i.raw_path
				FROM imports i JOIN files f ON i.file_id = f.id
				WHERE i.raw_path LIKE ?
				LIMIT ?
			`, pattern, limit)
			if err != nil {
				continue
			}
			for rows.Next() {
				var r ImporterResult
				if err := rows.Scan(&r.File, &r.RelPath, &r.Import); err == nil {
					if !seen[r.RelPath] {
						seen[r.RelPath] = true
						r.Depth = d
						results = append(results, r)
						nextTargets = append(nextTargets, r.RelPath)
					}
				}
			}
			rows.Close()
		}
		currentTargets = nextTargets
	}

	return results, nil
}

// TypeRefsInRange finds type-like symbols referenced within a line range of a file.
func (s *Store) TypeRefsInRange(filePath string, startLine, endLine int) ([]SymbolResult, error) {
	// Find distinct names referenced in the range.
	nameRows, err := s.db.Query(`
		SELECT DISTINCT r.name
		FROM refs r JOIN files f ON r.file_id = f.id
		WHERE f.path = ? AND r.line >= ? AND r.line <= ?
	`, filePath, startLine, endLine)
	if err != nil {
		return nil, err
	}
	defer nameRows.Close()

	var names []string
	for nameRows.Next() {
		var name string
		if err := nameRows.Scan(&name); err != nil {
			return nil, err
		}
		names = append(names, name)
	}
	if err := nameRows.Err(); err != nil {
		return nil, err
	}

	// For each name, look up type-like symbols.
	var results []SymbolResult
	seen := make(map[string]bool)
	for _, name := range names {
		rows, err := s.db.Query(`
			SELECT s.name, s.kind, f.path, f.rel_path, s.start_line, s.end_line, s.parent, s.depth, s.signature, COALESCE(s.summary, ''), s.language
			FROM symbols s JOIN files f ON s.file_id = f.id
			WHERE s.name = ? AND s.kind IN ('struct','interface','class','type','enum','trait')
		`, name)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r SymbolResult
			if err := rows.Scan(&r.Name, &r.Kind, &r.File, &r.RelPath, &r.StartLine, &r.EndLine, &r.Parent, &r.Depth, &r.Signature, &r.Summary, &r.Language); err != nil {
				rows.Close()
				return nil, err
			}
			key := r.SymbolID()
			if !seen[key] {
				seen[key] = true
				results = append(results, r)
			}
		}
		rows.Close()
	}

	return results, nil
}

// FileImports returns the raw import paths for a file.
func (s *Store) FileImports(filePath string) ([]string, error) {
	rows, err := s.db.Query(`
		SELECT i.raw_path
		FROM imports i JOIN files f ON i.file_id = f.id
		WHERE f.path = ?
		ORDER BY i.raw_path
	`, filePath)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var imports []string
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		imports = append(imports, raw)
	}
	return imports, rows.Err()
}

// FindImportersByPath finds files that import a given file or package path directly, up to depth hops.
func (s *Store) FindImportersByPath(target string, depth, limit int) ([]ImporterResult, error) {
	if depth <= 0 {
		depth = 1
	}
	if depth > 3 {
		depth = 3
	}

	// BFS through import graph.
	seen := make(map[string]bool)
	var results []ImporterResult
	currentTargets := []string{target}

	for d := 1; d <= depth && len(currentTargets) > 0; d++ {
		var nextTargets []string
		for _, t := range currentTargets {
			// Match raw_path by suffix (covers package paths like "foo/bar/pkg").
			rawPattern := "%" + t
			// Also try matching against rel_path for when the user provides a file path.
			relPattern := "%" + strings.TrimSuffix(filepath.Base(t), filepath.Ext(t)) + "%"
			rows, err := s.db.Query(`
				SELECT DISTINCT f.path, f.rel_path, i.raw_path
				FROM imports i JOIN files f ON i.file_id = f.id
				WHERE i.raw_path LIKE ? OR i.raw_path LIKE ?
				LIMIT ?
			`, rawPattern, relPattern, limit)
			if err != nil {
				continue
			}
			for rows.Next() {
				var r ImporterResult
				if err := rows.Scan(&r.File, &r.RelPath, &r.Import); err == nil {
					if !seen[r.RelPath] {
						seen[r.RelPath] = true
						r.Depth = d
						results = append(results, r)
						nextTargets = append(nextTargets, r.RelPath)
					}
				}
			}
			rows.Close()
		}
		currentTargets = nextTargets
	}

	return results, nil
}

// ImpactResult holds a transitive caller analysis result.
type ImpactResult struct {
	Symbol  string `json:"symbol"`   // the callee
	Caller  string `json:"caller"`   // the calling function
	File    string `json:"file"`     // abs path of caller's file
	RelPath string `json:"rel_path"` // relative path
	Line    int    `json:"line"`     // line of the call
	Depth   int    `json:"depth"`    // hop distance from original
}

// EnclosingSymbol returns the name of the narrowest symbol that encloses a line in a file.
func (s *Store) EnclosingSymbol(filePath string, line int) (string, error) {
	var name string
	err := s.db.QueryRow(`
		SELECT s.name FROM symbols s
		WHERE s.file_id = (SELECT id FROM files WHERE path = ?)
		  AND s.start_line <= ? AND s.end_line >= ?
		ORDER BY (s.end_line - s.start_line) ASC
		LIMIT 1
	`, filePath, line, line).Scan(&name)
	if err != nil {
		return "", err
	}
	return name, nil
}

// FindImpact performs transitive caller analysis using BFS.
func (s *Store) FindImpact(symbolName string, depth, limit int) ([]ImpactResult, error) {
	if depth <= 0 {
		depth = 2
	}
	if depth > 5 {
		depth = 5
	}

	seen := make(map[string]bool)
	var results []ImpactResult
	currentSymbols := []string{symbolName}

	for d := 1; d <= depth && len(currentSymbols) > 0 && len(results) < limit; d++ {
		var nextSymbols []string
		for _, sym := range currentSymbols {
			rows, err := s.db.Query(`
				SELECT f.path, f.rel_path, r.line, r.name
				FROM refs r JOIN files f ON r.file_id = f.id
				WHERE r.name = ?
			`, sym)
			if err != nil {
				continue
			}
			for rows.Next() {
				var filePath, relPath, refName string
				var line int
				if err := rows.Scan(&filePath, &relPath, &line, &refName); err != nil {
					continue
				}

				caller, err := s.EnclosingSymbol(filePath, line)
				if err != nil || caller == "" || caller == sym {
					continue
				}

				key := caller + "@" + filePath
				if seen[key] {
					continue
				}
				seen[key] = true

				results = append(results, ImpactResult{
					Symbol:  sym,
					Caller:  caller,
					File:    filePath,
					RelPath: relPath,
					Line:    line,
					Depth:   d,
				})
				nextSymbols = append(nextSymbols, caller)

				if len(results) >= limit {
					rows.Close()
					return results, nil
				}
			}
			rows.Close()
		}
		currentSymbols = nextSymbols
	}

	return results, nil
}
