package index

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/1broseidon/cymbal/symbols"
)

const schema = `
CREATE TABLE IF NOT EXISTS meta (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS files (
	id         INTEGER PRIMARY KEY AUTOINCREMENT,
	path       TEXT UNIQUE NOT NULL,
	rel_path   TEXT NOT NULL,
	language   TEXT NOT NULL,
	hash       TEXT NOT NULL,
	indexed_at DATETIME NOT NULL,
	mtime      DATETIME,
	mtime_ns   INTEGER,
	size       INTEGER
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
	language  TEXT NOT NULL,
	kind      TEXT NOT NULL DEFAULT 'use'
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
	content=symbols,
	content_rowid=id
);

CREATE TRIGGER IF NOT EXISTS symbols_ai AFTER INSERT ON symbols BEGIN
	INSERT INTO symbols_fts(rowid, name, kind) VALUES (new.id, new.name, new.kind);
END;

CREATE TRIGGER IF NOT EXISTS symbols_ad AFTER DELETE ON symbols BEGIN
	INSERT INTO symbols_fts(symbols_fts, rowid, name, kind) VALUES('delete', old.id, old.name, old.kind);
END;
`

// Store manages the SQLite database.
type Store struct {
	db *sql.DB
}

// OpenStore opens or creates the database.
func OpenStore(dbPath string) (*Store, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}
	_ = os.Chmod(dir, 0o700)

	db, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL&_synchronous=NORMAL&_busy_timeout=5000&_foreign_keys=ON")
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("initializing schema: %w", err)
	}

	// Migrations: add columns for existing databases (silently ignored if present).
	db.Exec("ALTER TABLE files ADD COLUMN mtime DATETIME")
	db.Exec("ALTER TABLE files ADD COLUMN mtime_ns INTEGER")
	db.Exec("ALTER TABLE files ADD COLUMN size INTEGER")
	db.Exec("ALTER TABLE refs ADD COLUMN kind TEXT NOT NULL DEFAULT 'use'")
	// Create kind index *after* the ALTER so existing databases don't fail the
	// initial schema CREATE INDEX on a column that doesn't exist yet.
	db.Exec("CREATE INDEX IF NOT EXISTS idx_refs_kind ON refs(kind)")

	db.Exec("PRAGMA cache_size = -64000")
	db.Exec("PRAGMA mmap_size = 268435456")
	db.Exec("PRAGMA temp_store = MEMORY")
	tightenStorePermissions(dbPath)

	return &Store{db: db}, nil
}

func tightenStorePermissions(dbPath string) {
	_ = os.Chmod(dbPath, 0o600)
	_ = os.Chmod(dbPath+"-wal", 0o600)
	_ = os.Chmod(dbPath+"-shm", 0o600)
}

// Close checkpoints WAL pages into the main DB file, then closes the database.
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	_, checkpointErr := s.db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	closeErr := s.db.Close()
	if closeErr != nil {
		return closeErr
	}
	return checkpointErr
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

// FileCheck holds stored mtime (nanoseconds) + size for change detection.
type FileCheck struct {
	MtimeNs int64
	Size    int64
}

// AllFileChecks loads all file paths with their stored mtime_ns and size.
func (s *Store) AllFileChecks() (map[string]FileCheck, error) {
	rows, err := s.db.Query("SELECT path, COALESCE(mtime_ns, 0), COALESCE(size, -1) FROM files")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	m := make(map[string]FileCheck)
	for rows.Next() {
		var path string
		var fc FileCheck
		if err := rows.Scan(&path, &fc.MtimeNs, &fc.Size); err != nil {
			continue
		}
		m[path] = fc
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
func (s *Store) UpsertFile(filePath, relPath, lang, hash string, mtime time.Time, size int64) (int64, error) {
	now := time.Now()
	s.db.Exec("DELETE FROM files WHERE path = ?", filePath)

	res, err := s.db.Exec(
		"INSERT INTO files (path, rel_path, language, hash, indexed_at, mtime_ns, size) VALUES (?, ?, ?, ?, ?, ?, ?)",
		filePath, relPath, lang, hash, now, mtime.UnixNano(), size,
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
		(file_id, name, kind, start_line, end_line, start_col, end_col, parent, depth, signature, language)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, sym := range syms {
		_, err := stmt.Exec(
			fileID, sym.Name, sym.Kind,
			sym.StartLine, sym.EndLine, sym.StartCol, sym.EndCol,
			sym.Parent, sym.Depth, sym.Signature, sym.Language,
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
	stmt, err := tx.Prepare("INSERT INTO refs (file_id, line, name, language, kind) VALUES (?, ?, ?, ?, ?)")
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, ref := range refs {
		kind := ref.Kind
		if kind == "" {
			kind = symbols.RefKindUse
		}
		if _, err := stmt.Exec(fileID, ref.Line, ref.Name, ref.Language, kind); err != nil {
			return err
		}
	}
	return nil
}

// InsertFileAll inserts a file and all its data (symbols, imports, refs) in a single
// transaction operation. Designed for use within an external transaction via InsertFileAllTx.
func (s *Store) InsertFileAll(filePath, relPath, lang, hash string, mtime time.Time, size int64, syms []symbols.Symbol, imports []symbols.Import, refs []symbols.Ref) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := s.InsertFileAllTx(tx, filePath, relPath, lang, hash, mtime, size, syms, imports, refs); err != nil {
		return err
	}
	return tx.Commit()
}

// InsertFileAllTx inserts a file and all its data within an existing transaction.
func (s *Store) InsertFileAllTx(tx *sql.Tx, filePath, relPath, lang, hash string, mtime time.Time, size int64, syms []symbols.Symbol, imports []symbols.Import, refs []symbols.Ref) error {
	now := time.Now()
	tx.Exec("DELETE FROM files WHERE path = ?", filePath)

	res, err := tx.Exec(
		"INSERT INTO files (path, rel_path, language, hash, indexed_at, mtime_ns, size) VALUES (?, ?, ?, ?, ?, ?, ?)",
		filePath, relPath, lang, hash, now, mtime.UnixNano(), size,
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
		"INSERT INTO files (path, rel_path, language, hash, indexed_at, mtime_ns, size) VALUES (?, ?, ?, ?, ?, ?, ?)")
	if err != nil {
		b.Close()
		return nil, err
	}
	b.insSymbol, err = tx.Prepare(`INSERT INTO symbols
		(file_id, name, kind, start_line, end_line, start_col, end_col, parent, depth, signature, language)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		b.Close()
		return nil, err
	}
	b.insImport, err = tx.Prepare("INSERT INTO imports (file_id, raw_path, language) VALUES (?, ?, ?)")
	if err != nil {
		b.Close()
		return nil, err
	}
	b.insRef, err = tx.Prepare("INSERT INTO refs (file_id, line, name, language, kind) VALUES (?, ?, ?, ?, ?)")
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
func InsertFileAllStmts(b *BatchStmts, filePath, relPath, lang, hash string, mtime time.Time, size int64, syms []symbols.Symbol, imports []symbols.Import, refs []symbols.Ref) error {
	now := time.Now()
	b.delFile.Exec(filePath) //nolint:errcheck

	res, err := b.insFile.Exec(filePath, relPath, lang, hash, now, mtime.UnixNano(), size)
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
			sym.Parent, sym.Depth, sym.Signature, sym.Language,
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
		kind := ref.Kind
		if kind == "" {
			kind = symbols.RefKindUse
		}
		if _, err := b.insRef.Exec(fileID, ref.Line, ref.Name, ref.Language, kind); err != nil {
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
	Language  string `json:"language"`
}

// SymbolID returns a stable identifier for this symbol.
func (r SymbolResult) SymbolID() string {
	return fmt.Sprintf("%s:%s:%s:%s:%d", r.RelPath, r.Language, r.Kind, r.Name, r.StartLine)
}

// StructureResult holds the output of a structural analysis.
type StructureResult struct {
	RepoRoot       string          `json:"repo_root"`
	Files          int             `json:"files"`
	Symbols        int             `json:"symbols"`
	Languages      map[string]int  `json:"languages"`
	EntryPoints    []SymbolResult  `json:"entry_points"`
	TopByRefs      []RankedSymbol  `json:"top_by_refs"`
	TopByImportFan []RankedFile    `json:"top_by_import_fan"`
	TopPackages    []RankedPackage `json:"top_packages"`
}

// RankedSymbol is a symbol with a count (e.g., ref count).
type RankedSymbol struct {
	SymbolResult
	Count int `json:"count"`
}

// RankedFile is a file path with a count (e.g., import fan-in).
type RankedFile struct {
	RelPath  string `json:"rel_path"`
	Language string `json:"language"`
	Count    int    `json:"count"`
}

// RankedPackage is a directory with symbol count.
type RankedPackage struct {
	Path    string `json:"path"`
	Symbols int    `json:"symbols"`
	Files   int    `json:"files"`
}

// Structure returns a structural overview of the indexed codebase.
func (s *Store) Structure(limit int) (*StructureResult, error) {
	if limit <= 0 {
		limit = 10
	}

	repoRoot, _ := s.GetMeta("repo_root")
	result := &StructureResult{
		RepoRoot:  repoRoot,
		Languages: make(map[string]int),
	}

	s.db.QueryRow("SELECT COUNT(*) FROM files").Scan(&result.Files)
	s.db.QueryRow("SELECT COUNT(*) FROM symbols s JOIN files f ON s.file_id = f.id").Scan(&result.Symbols)

	// Languages
	langRows, err := s.db.Query("SELECT language, COUNT(*) FROM files GROUP BY language ORDER BY COUNT(*) DESC")
	if err == nil {
		defer langRows.Close()
		for langRows.Next() {
			var lang string
			var cnt int
			langRows.Scan(&lang, &cnt)
			result.Languages[lang] = cnt
		}
	}

	// Entry points: main, init, or exported top-level functions at depth 0
	entryRows, err := s.db.Query(`
		SELECT s.name, s.kind, f.path, f.rel_path, s.start_line, s.end_line, s.parent, s.depth, s.signature, s.language
		FROM symbols s JOIN files f ON s.file_id = f.id
		WHERE s.depth = 0 AND s.kind IN ('function', 'method')
		  AND (s.name = 'main' OR s.name = 'init' OR s.name = 'Main' OR s.name = 'Init'
		       OR (s.name GLOB '[A-Z]*' AND s.kind = 'function' AND f.rel_path LIKE '%main%'))
		ORDER BY s.name LIMIT ?
	`, limit)
	if err == nil {
		defer entryRows.Close()
		for entryRows.Next() {
			var sym SymbolResult
			entryRows.Scan(&sym.Name, &sym.Kind, &sym.File, &sym.RelPath, &sym.StartLine, &sym.EndLine,
				&sym.Parent, &sym.Depth, &sym.Signature, &sym.Language)
			result.EntryPoints = append(result.EntryPoints, sym)
		}
	}

	// Top symbols by ref count
	refRows, err := s.db.Query(`
		SELECT r.name, COUNT(*) as cnt,
		       s.kind, f.path, f.rel_path, s.start_line, s.end_line, s.parent, s.depth, s.signature, s.language
		FROM refs r
		JOIN symbols s ON s.name = r.name AND s.depth = 0
		JOIN files f ON s.file_id = f.id
		GROUP BY r.name, f.path
		ORDER BY cnt DESC
		LIMIT ?
	`, limit)
	if err == nil {
		defer refRows.Close()
		for refRows.Next() {
			var rs RankedSymbol
			refRows.Scan(&rs.Name, &rs.Count,
				&rs.Kind, &rs.File, &rs.RelPath, &rs.StartLine, &rs.EndLine,
				&rs.Parent, &rs.Depth, &rs.Signature, &rs.Language)
			result.TopByRefs = append(result.TopByRefs, rs)
		}
	}

	// Top files by import fan-in (how many other files import this file's package/path)
	impRows, err := s.db.Query(`
		SELECT f.rel_path, f.language, COUNT(DISTINCT i.file_id) as cnt
		FROM files f
		JOIN imports i ON i.raw_path LIKE '%' || REPLACE(f.rel_path, '/', '.') || '%'
		   OR i.raw_path LIKE '%' || REPLACE(REPLACE(f.rel_path, '/', '.'), '.go', '') || '%'
		   OR i.raw_path LIKE '%' || REPLACE(REPLACE(f.rel_path, '/', '.'), '.py', '') || '%'
		GROUP BY f.rel_path
		HAVING cnt > 1
		ORDER BY cnt DESC
		LIMIT ?
	`, limit)
	if err == nil {
		defer impRows.Close()
		for impRows.Next() {
			var rf RankedFile
			impRows.Scan(&rf.RelPath, &rf.Language, &rf.Count)
			result.TopByImportFan = append(result.TopByImportFan, rf)
		}
	}

	// Top packages/directories by symbol count
	pkgRows, err := s.db.Query(`
		SELECT
		  CASE WHEN INSTR(f.rel_path, '/') > 0
		       THEN SUBSTR(f.rel_path, 1, INSTR(f.rel_path, '/') - 1)
			     || CASE WHEN INSTR(SUBSTR(f.rel_path, INSTR(f.rel_path, '/') + 1), '/') > 0
			          THEN '/' || SUBSTR(SUBSTR(f.rel_path, INSTR(f.rel_path, '/') + 1), 1, INSTR(SUBSTR(f.rel_path, INSTR(f.rel_path, '/') + 1), '/') - 1)
			          ELSE '/' || SUBSTR(f.rel_path, INSTR(f.rel_path, '/') + 1)
			        END
		       ELSE '.'
		  END as pkg,
		  COUNT(DISTINCT s.id) as sym_count,
		  COUNT(DISTINCT f.id) as file_count
		FROM files f
		JOIN symbols s ON s.file_id = f.id
		GROUP BY pkg
		ORDER BY sym_count DESC
		LIMIT ?
	`, limit)
	if err == nil {
		defer pkgRows.Close()
		for pkgRows.Next() {
			var rp RankedPackage
			pkgRows.Scan(&rp.Path, &rp.Symbols, &rp.Files)
			result.TopPackages = append(result.TopPackages, rp)
		}
	}

	return result, nil
}

// SearchSymbolsCI performs a case-insensitive exact name match.
func (s *Store) SearchSymbolsCI(name string, limit int) ([]SymbolResult, error) {
	// Exact match: fetch all rows so the ranker sees the full candidate set
	// before truncating to the user limit. Definition counts are small even
	// in large repos, so no LIMIT is needed here.
	rows, err := s.db.Query(`
		SELECT s.name, s.kind, f.path, f.rel_path, s.start_line, s.end_line, s.parent, s.depth, s.signature, s.language
		FROM symbols s JOIN files f ON s.file_id = f.id
		WHERE s.name COLLATE NOCASE = ?
		ORDER BY s.name
	`, name)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SymbolResult
	for rows.Next() {
		var r SymbolResult
		if err := rows.Scan(&r.Name, &r.Kind, &r.File, &r.RelPath, &r.StartLine, &r.EndLine, &r.Parent, &r.Depth, &r.Signature, &r.Language); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	RankSymbols(results)
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// SearchSymbols searches using FTS5 with ranking: exact > prefix > fuzzy.
// When ignoreCase is true and exact is true, the name comparison is
// case-insensitive (FTS5 tokenization is already case-insensitive for the
// non-exact path, so ignoreCase is a no-op there).
func (s *Store) SearchSymbols(query, kind, lang string, exact, ignoreCase bool, limit int) ([]SymbolResult, error) {
	var rows *sql.Rows
	var err error

	// Over-fetch so the ranking window covers enough candidates before truncating.
	fetch := rankFetchWindow(limit, exact)

	if exact {
		nameClause := "s.name = ?"
		if ignoreCase {
			nameClause = "s.name = ? COLLATE NOCASE"
		}
		q := `SELECT s.name, s.kind, f.path, f.rel_path, s.start_line, s.end_line, s.parent, s.depth, s.signature, s.language
			  FROM symbols s JOIN files f ON s.file_id = f.id
			  WHERE ` + nameClause
		args := []any{query}
		if kind != "" {
			q += " AND s.kind = ?"
			args = append(args, kind)
		}
		if lang != "" {
			q += " AND s.language = ?"
			args = append(args, lang)
		}
		// fetch==0 means no LIMIT (fetch all rows so ranking sees full set).
		if fetch > 0 {
			q += " ORDER BY s.name LIMIT ?"
			args = append(args, fetch)
		} else {
			q += " ORDER BY s.name"
		}
		rows, err = s.db.Query(q, args...)
	} else {
		ftsQuery := query + "*"
		q := `SELECT s.name, s.kind, f.path, f.rel_path, s.start_line, s.end_line, s.parent, s.depth, s.signature, s.language
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
		args = append(args, query, query, fetch)
		rows, err = s.db.Query(q, args...)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SymbolResult
	for rows.Next() {
		var r SymbolResult
		if err := rows.Scan(&r.Name, &r.Kind, &r.File, &r.RelPath, &r.StartLine, &r.EndLine, &r.Parent, &r.Depth, &r.Signature, &r.Language); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// For exact queries all results share the same name — canonical ranking is
	// safe. For FTS queries the SQL tier order (exact-name > prefix > fuzzy)
	// must be preserved across different symbol names; apply canonical ranking
	// only within each tier so test/playground results don't float above source
	// results at the same tier.
	if exact {
		RankSymbols(results)
	} else {
		rankWithinFTSTiers(results, query)
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// rankWithinFTSTiers preserves SQL tier order (exact-name > prefix > fuzzy)
// while applying canonical path/kind scoring within each tier.
func rankWithinFTSTiers(results []SymbolResult, query string) {
	tier := func(name string) int {
		n := strings.ToLower(name)
		q := strings.ToLower(query)
		switch {
		case n == q:
			return 0
		case strings.HasPrefix(n, q):
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		ti, tj := tier(results[i].Name), tier(results[j].Name)
		if ti != tj {
			return ti < tj
		}
		return SymbolScore(results[i]) > SymbolScore(results[j])
	})
}

// FileSymbols returns all symbols in a given file.
func (s *Store) FileSymbols(filePath string) ([]SymbolResult, error) {
	rows, err := s.db.Query(`
		SELECT s.name, s.kind, f.path, f.rel_path, s.start_line, s.end_line, s.parent, s.depth, s.signature, s.language
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
		if err := rows.Scan(&r.Name, &r.Kind, &r.File, &r.RelPath, &r.StartLine, &r.EndLine, &r.Parent, &r.Depth, &r.Signature, &r.Language); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// ChildSymbols returns symbols whose parent matches the given name (e.g., methods on a type).
// When filePath is non-empty the results are scoped to that file, preventing
// member bleed when different files contain types with the same name.
func (s *Store) ChildSymbols(parentName string, limit int, filePath ...string) ([]SymbolResult, error) {
	var rows *sql.Rows
	var err error
	if len(filePath) > 0 && filePath[0] != "" {
		rows, err = s.db.Query(`
			SELECT s.name, s.kind, f.path, f.rel_path, s.start_line, s.end_line, s.parent, s.depth, s.signature, s.language
			FROM symbols s JOIN files f ON s.file_id = f.id
			WHERE s.parent = ? AND f.path = ?
			ORDER BY s.start_line
			LIMIT ?
		`, parentName, filePath[0], limit)
	} else {
		rows, err = s.db.Query(`
			SELECT s.name, s.kind, f.path, f.rel_path, s.start_line, s.end_line, s.parent, s.depth, s.signature, s.language
			FROM symbols s JOIN files f ON s.file_id = f.id
			WHERE s.parent = ?
			ORDER BY s.start_line
			LIMIT ?
		`, parentName, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []SymbolResult
	for rows.Next() {
		var r SymbolResult
		if err := rows.Scan(&r.Name, &r.Kind, &r.File, &r.RelPath, &r.StartLine, &r.EndLine, &r.Parent, &r.Depth, &r.Signature, &r.Language); err != nil {
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
// By default this surfaces any ref kind (call, use, implements); pass
// explicit kinds to restrict (e.g. "call" to skip type-mentions).
func (s *Store) FindReferences(name string, limit int, kinds ...string) ([]RefResult, error) {
	if len(kinds) == 0 {
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
	kindPlaceholders := strings.Repeat("?,", len(kinds))
	kindPlaceholders = kindPlaceholders[:len(kindPlaceholders)-1]
	args := []interface{}{name}
	for _, k := range kinds {
		args = append(args, k)
	}
	args = append(args, limit)
	rows, err := s.db.Query(`
		SELECT f.path, f.rel_path, r.line, r.name
		FROM refs r JOIN files f ON r.file_id = f.id
		WHERE r.name = ? AND r.kind IN (`+kindPlaceholders+`)
		ORDER BY f.rel_path, r.line
		LIMIT ?
	`, args...)
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

// ImplementorResult holds a type that implements / conforms to / extends a target.
type ImplementorResult struct {
	// Implementer is the local declaring type name (e.g. "TimerActivityIntent").
	// Empty if the declaring symbol could not be resolved from the line range.
	Implementer string `json:"implementer"`
	// Target is the protocol / interface / superclass name as written in source.
	Target string `json:"target"`
	// File + Line locate the inheritance clause in source.
	File     string `json:"file"`
	RelPath  string `json:"rel_path"`
	Line     int    `json:"line"`
	Language string `json:"language"`
	// Resolved is true when Target matches a locally-indexed symbol (i.e. the
	// protocol/interface is declared in this repo, not an external framework).
	Resolved bool `json:"resolved"`
}

// FindImplementors finds types that declare themselves as implementing /
// conforming to / extending the given target name. Name-based, best-effort;
// external (framework) targets are returned with Resolved=false.
func (s *Store) FindImplementors(target string, limit int) ([]ImplementorResult, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT
			f.path,
			f.rel_path,
			r.line,
			r.language,
			COALESCE((
				SELECT s.name FROM symbols s
				WHERE s.file_id = r.file_id
				  AND s.start_line <= r.line
				  AND s.end_line   >= r.line
				  AND s.kind IN ('class','struct','enum','interface','protocol','trait','record','object','mixin','actor','impl')
				ORDER BY (s.end_line - s.start_line) ASC
				LIMIT 1
			), '') AS implementer,
			EXISTS(
				SELECT 1 FROM symbols s2
				WHERE s2.name = r.name
				  AND s2.kind IN ('class','struct','enum','interface','protocol','trait','record','object','mixin','actor')
			) AS resolved
		FROM refs r JOIN files f ON r.file_id = f.id
		WHERE r.kind = ? AND r.name = ?
		ORDER BY f.rel_path, r.line
		LIMIT ?
	`, symbols.RefKindImplements, target, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ImplementorResult
	for rows.Next() {
		r := ImplementorResult{Target: target}
		var resolvedInt int
		if err := rows.Scan(&r.File, &r.RelPath, &r.Line, &r.Language, &r.Implementer, &resolvedInt); err != nil {
			return nil, err
		}
		r.Resolved = resolvedInt != 0
		results = append(results, r)
	}
	return results, rows.Err()
}

// FindImplements returns the inheritance / conformance edges declared by a
// specific type (the inverse of FindImplementors). It resolves the type's
// declaration line range via the symbols table, then returns implements-kind
// refs inside that range — but only when the queried type is the *smallest*
// enclosing type-like symbol at the ref's line. That constraint prevents
// over-reporting when a big outer class contains nested types whose
// conformances would otherwise be attributed to the outer (e.g. Swift
// `class Session { struct RequestConvertible: URLRequestConvertible {} }` —
// `--of Session` must not return URLRequestConvertible).
func (s *Store) FindImplements(typeName string, limit int) ([]ImplementorResult, error) {
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`
		SELECT
			f.path, f.rel_path, r.line, r.language, s.name, r.name,
			EXISTS(
				SELECT 1 FROM symbols s2
				WHERE s2.name = r.name
				  AND s2.kind IN ('class','struct','enum','interface','protocol','trait','record','object','mixin','actor')
			) AS resolved
		FROM symbols s
		JOIN refs r
		  ON r.file_id = s.file_id
		 AND r.line BETWEEN s.start_line AND s.end_line
		JOIN files f ON f.id = s.file_id
		WHERE s.name = ?
		  AND s.kind IN ('class','struct','enum','interface','protocol','trait','record','object','mixin','actor','impl')
		  AND r.kind = ?
		  AND NOT EXISTS (
			SELECT 1 FROM symbols s3
			WHERE s3.file_id = r.file_id
			  AND s3.start_line <= r.line
			  AND s3.end_line   >= r.line
			  AND s3.kind IN ('class','struct','enum','interface','protocol','trait','record','object','mixin','actor','impl')
			  AND (s3.end_line - s3.start_line) < (s.end_line - s.start_line)
		  )
		ORDER BY f.rel_path, r.line
		LIMIT ?
	`, typeName, symbols.RefKindImplements, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ImplementorResult
	for rows.Next() {
		var r ImplementorResult
		var resolvedInt int
		if err := rows.Scan(&r.File, &r.RelPath, &r.Line, &r.Language, &r.Implementer, &r.Target, &resolvedInt); err != nil {
			return nil, err
		}
		r.Resolved = resolvedInt != 0
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
	// Parent is the current BFS target this importer matched against.
	// At depth=1 it is the original query target; at deeper hops it is
	// the rel_path of the previously discovered importer.
	Parent string `json:"parent,omitempty"`
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
						r.Parent = target
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
			SELECT s.name, s.kind, f.path, f.rel_path, s.start_line, s.end_line, s.parent, s.depth, s.signature, s.language
			FROM symbols s JOIN files f ON s.file_id = f.id
			WHERE s.name = ? AND s.kind IN ('struct','interface','class','type','enum','trait')
		`, name)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r SymbolResult
			if err := rows.Scan(&r.Name, &r.Kind, &r.File, &r.RelPath, &r.StartLine, &r.EndLine, &r.Parent, &r.Depth, &r.Signature, &r.Language); err != nil {
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
						r.Parent = t
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

// TraceResult represents a single edge in a downward call trace.
type TraceResult struct {
	Caller  string `json:"caller"`   // the function making the call
	Callee  string `json:"callee"`   // the function being called
	File    string `json:"file"`     // abs path where the call happens
	RelPath string `json:"rel_path"` // relative path
	Line    int    `json:"line"`     // line of the call
	Depth   int    `json:"depth"`    // hop distance from root
}

// FindTrace performs downward call graph traversal using BFS.
// Starting from a symbol, it finds what that symbol calls, then what those call, etc.
//
// kinds filters which ref kinds count as trace edges. Nil/empty defaults to
// {"call"} so trace surfaces only invocation edges, not every identifier.
// Pass a broader set (e.g. {"call","use"}) to include type mentions and
// other non-call identifier references.
func (s *Store) FindTrace(symbolName string, depth, limit int, kinds ...string) ([]TraceResult, error) {
	if depth <= 0 {
		depth = 3
	}
	if depth > 5 {
		depth = 5
	}
	if len(kinds) == 0 {
		kinds = []string{symbols.RefKindCall}
	}
	kindPlaceholders := strings.Repeat("?,", len(kinds))
	kindPlaceholders = kindPlaceholders[:len(kindPlaceholders)-1]

	// Resolve the root symbol to get its file and line range.
	type symLoc struct {
		name      string
		file      string
		startLine int
		endLine   int
	}

	resolveSymbol := func(name string) []symLoc {
		rows, err := s.db.Query(`
			SELECT s.name, f.path, s.start_line, s.end_line
			FROM symbols s JOIN files f ON s.file_id = f.id
			WHERE s.name = ?
		`, name)
		if err != nil {
			return nil
		}
		defer rows.Close()
		var locs []symLoc
		for rows.Next() {
			var loc symLoc
			if err := rows.Scan(&loc.name, &loc.file, &loc.startLine, &loc.endLine); err != nil {
				continue
			}
			locs = append(locs, loc)
		}
		return locs
	}

	// Find refs inside a symbol's line range (its callees).
	calleesOf := func(loc symLoc) []TraceResult {
		args := []interface{}{loc.file, loc.startLine, loc.endLine}
		for _, k := range kinds {
			args = append(args, k)
		}
		rows, err := s.db.Query(`
			SELECT r.name, f.path, f.rel_path, r.line
			FROM refs r JOIN files f ON r.file_id = f.id
			WHERE f.path = ? AND r.line >= ? AND r.line <= ?
			  AND r.kind IN (`+kindPlaceholders+`)
		`, args...)
		if err != nil {
			return nil
		}
		defer rows.Close()
		var results []TraceResult
		for rows.Next() {
			var tr TraceResult
			if err := rows.Scan(&tr.Callee, &tr.File, &tr.RelPath, &tr.Line); err != nil {
				continue
			}
			tr.Caller = loc.name
			results = append(results, tr)
		}
		return results
	}

	// Filter out builtins and stdlib noise — keep only project-defined symbols.
	isProjectSymbol := func(name string) bool {
		// Skip Go builtins, common stdlib methods, and type casts.
		switch name {
		case "len", "cap", "make", "append", "close", "delete", "copy", "new", "panic", "recover",
			"int", "int8", "int16", "int32", "int64", "uint", "uint8", "uint16", "uint32", "uint64",
			"float32", "float64", "string", "bool", "byte", "rune", "error", "nil",
			"Errorf", "Sprintf", "Fprintf", "Printf", "Println",
			"Error", "String", "Close", "Read", "Write",
			"Lock", "Unlock", "RLock", "RUnlock",
			"Add", "Load", "Store", "Done", "Wait",
			"Begin", "Commit", "Rollback", "Exec", "Query", "QueryRow", "Scan",
			"Now", "Since", "Sleep",
			"Join", "Split", "Contains", "HasPrefix", "HasSuffix", "TrimPrefix", "TrimSuffix",
			"Open", "Create", "Remove", "Stat", "Lstat", "ReadFile", "WriteFile",
			"Abs", "Dir", "Base", "Ext", "Rel",
			"Go", "Next", "Rows":
			return false
		}
		// Skip single-letter or very short names (usually loop vars or generics).
		if len(name) <= 2 {
			return false
		}
		return true
	}

	seen := make(map[string]bool)
	var results []TraceResult
	currentLocs := resolveSymbol(symbolName)

	for d := 1; d <= depth && len(currentLocs) > 0 && len(results) < limit; d++ {
		var nextLocs []symLoc
		for _, loc := range currentLocs {
			callees := calleesOf(loc)
			for _, tr := range callees {
				if tr.Callee == loc.name || !isProjectSymbol(tr.Callee) {
					continue
				}
				key := loc.name + "→" + tr.Callee
				if seen[key] {
					continue
				}
				seen[key] = true

				tr.Depth = d
				results = append(results, tr)

				// Resolve the callee for the next depth level.
				for _, nextLoc := range resolveSymbol(tr.Callee) {
					nextLocs = append(nextLocs, nextLoc)
				}

				if len(results) >= limit {
					return results, nil
				}
			}
		}
		currentLocs = nextLocs
	}

	return results, nil
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
