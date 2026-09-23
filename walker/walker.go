package walker

import (
	"io"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/1broseidon/cymbal/internal/pathmatch"
	"github.com/1broseidon/cymbal/lang"
)

// FileEntry is a file discovered during walking.
type FileEntry struct {
	Path     string
	RelPath  string
	Size     int64
	Language string
	ModTime  time.Time
}

// WalkOptions controls file discovery.
type WalkOptions struct {
	// Exclude skips files whose repo-relative path matches any pattern.
	Exclude []string
	// IncludeGenerated disables the default generated-file skip rules.
	IncludeGenerated bool
	// IncludeLargeFiles disables the default large-source-file skip rule.
	IncludeLargeFiles bool
	// RelBase, if set, is the base directory for RelPath computation and
	// exclude matching instead of the walk root. Set it to the repo root
	// when walking a subtree so paths stay repo-relative.
	RelBase string
}

// WalkStats reports files omitted by walk-time path filters.
type WalkStats struct {
	FilesExcluded int
	BytesExcluded int64
}

// TreeNode represents a directory/file tree.
type TreeNode struct {
	Name     string      `json:"name"`
	Path     string      `json:"path"`
	IsDir    bool        `json:"is_dir"`
	Children []*TreeNode `json:"children,omitempty"`
}

// Known directories to skip.
var skipDirs = map[string]bool{
	".git":         true,
	"node_modules": true,
	"vendor":       true,
	".venv":        true,
	"venv":         true,
	"__pycache__":  true,
	".tox":         true,
	".mypy_cache":  true,
	"dist":         true,
	"build":        true,
	".next":        true,
	".nuxt":        true,
	"target":       true, // Rust/Java
	".idea":        true,
	".vscode":      true,
}

const defaultMaxSourceFileBytes int64 = 3*1024*1024 + 256*1024

// LangForFile returns the language identifier for a file path.
// It delegates to the unified language registry in lang.Default. It looks at the
// path only, so an extensionless script that Walk classifies by its `#!` line
// returns "" here.
func LangForFile(path string) string {
	return lang.Default.LangForFile(path)
}

// Walk concurrently discovers all source files under root.
// If langFilter is non-nil, only files whose language passes the filter are emitted.
// This avoids building FileEntry structs and stat-ing files that will be immediately
// skipped (e.g., .json, .md, .toml that the parser doesn't support).
func Walk(root string, workers int, langFilter func(string) bool) ([]FileEntry, error) {
	files, _, err := WalkWithOptions(root, workers, langFilter, WalkOptions{})
	return files, err
}

// WalkWithOptions concurrently discovers all source files under root.
func WalkWithOptions(root string, workers int, langFilter func(string) bool, opts WalkOptions) ([]FileEntry, WalkStats, error) {
	if workers <= 0 {
		workers = 8
	}
	relBase := opts.RelBase
	if relBase == "" {
		relBase = root
	}

	var mu sync.Mutex
	var files []FileEntry
	var stats WalkStats

	type task struct {
		path     string
		relPath  string
		language string
		size     int64
		modTime  time.Time
		sniff    fs.DirEntry // set when the language comes from the `#!` line, read by the worker
	}

	ch := make(chan task, 256)
	var wg sync.WaitGroup

	// Spawn workers that process directories.
	for range workers {
		wg.Go(func() {
			for work := range ch {
				if work.sniff != nil {
					info, err := work.sniff.Info()
					// The path checks ran in the walk. The file's language is unknown
					// until it is read, so a too-large one is dropped uncounted.
					if err != nil || isTooLarge(info, opts) {
						continue
					}
					work.language = sniffLanguage(work.path)
					if work.language == "" || langFilter != nil && !langFilter(work.language) {
						continue
					}
					work.size, work.modTime = info.Size(), info.ModTime()
				}
				entry := FileEntry{
					Path:     work.path,
					RelPath:  work.relPath,
					Size:     work.size,
					Language: work.language,
					ModTime:  work.modTime,
				}
				mu.Lock()
				files = append(files, entry)
				mu.Unlock()
			}
		})
	}

	// Walk the tree, sending file paths to workers.
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if skipDirs[name] || strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			return nil
		}

		lang := LangForFile(path)
		if lang == "" {
			// An extensionless regular file may still be a script. A worker stats
			// it and reads its `#!` line, so that cost is spread over the pool, and
			// a path-excluded file is never stat-ed or opened. The regular-file
			// check matters: opening a FIFO would block.
			if filepath.Ext(d.Name()) != "" || !d.Type().IsRegular() {
				return nil
			}
			rel, err := filepath.Rel(relBase, path)
			if err != nil {
				rel = path
			}
			if !shouldExcludePath(rel, opts) {
				ch <- task{path: path, relPath: rel, sniff: d}
			}
			return nil
		}
		if langFilter != nil && !langFilter(lang) {
			return nil
		}

		rel, err := filepath.Rel(relBase, path)
		if err != nil {
			rel = path
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if shouldExcludeFile(rel, info, opts) {
			addExcludedFile(&stats, info)
			return nil
		}

		ch <- task{
			path:     path,
			relPath:  rel,
			language: lang,
			size:     info.Size(),
			modTime:  info.ModTime(),
		}
		return nil
	})
	close(ch)
	wg.Wait()

	if err != nil {
		return nil, WalkStats{}, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].RelPath < files[j].RelPath
	})
	return files, stats, nil
}

// sniffLanguage is langForShebang, swappable so tests can see which files are read.
var sniffLanguage = langForShebang

// langForShebang classifies an extensionless file by its `#!` line. Only files
// the name cannot classify reach here, so the extra open is paid per
// extensionless file, not per file walked.
func langForShebang(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	head := make([]byte, lang.ShebangMaxBytes)
	n, _ := io.ReadFull(f, head)
	if l := lang.Default.ForShebang(head[:n]); l != nil {
		return l.Name
	}
	return ""
}

func shouldExcludeFile(rel string, info fs.FileInfo, opts WalkOptions) bool {
	return shouldExcludePath(rel, opts) || isTooLarge(info, opts)
}

func isTooLarge(info fs.FileInfo, opts WalkOptions) bool {
	return !opts.IncludeLargeFiles && info.Size() > defaultMaxSourceFileBytes
}

// shouldExcludePath is the part of shouldExcludeFile that needs only the path.
func shouldExcludePath(rel string, opts WalkOptions) bool {
	if pathmatch.MatchAny(rel, opts.Exclude) {
		return true
	}
	if opts.IncludeGenerated {
		return false
	}
	return isGeneratedFile(rel)
}

func addExcludedFile(stats *WalkStats, info fs.FileInfo) {
	stats.FilesExcluded++
	if info != nil {
		stats.BytesExcluded += info.Size()
	}
}

func isGeneratedFile(rel string) bool {
	rel = strings.ToLower(pathmatch.Normalize(rel))
	base := pathpkg.Base(rel)

	if isTreeSitterGeneratedFile(rel, base) {
		return true
	}
	if isGeneratedPath(rel, base) {
		return true
	}
	return false
}

func isTreeSitterGeneratedFile(rel, base string) bool {
	if !strings.Contains(rel, "/src/") {
		return false
	}
	if !(strings.Contains(rel, "tree-sitter") || strings.Contains(rel, "/tsgrammars/") || strings.HasPrefix(rel, "internal/tsgrammars/")) {
		return false
	}
	if base == "parser.c" {
		return true
	}
	return strings.HasPrefix(base, "parser_abi") && strings.HasSuffix(base, ".c")
}

func isGeneratedPath(rel, base string) bool {
	for _, segment := range strings.Split(rel, "/") {
		if segment == "__generated__" || segment == "generated" {
			return true
		}
	}

	for _, suffix := range []string{
		".pb.go", "_pb2.py", "_pb2_grpc.py",
		"_generated.go", "_gen.go", ".gen.go",
		".generated.go", ".generated.ts", ".generated.js",
		".gen.ts", "_pb.d.ts", "_grpc.pb.go",
		".g.dart", ".min.js",
	} {
		if strings.HasSuffix(base, suffix) {
			return true
		}
	}
	return strings.Contains(base, "_generated.")
}

// BuildTree constructs a tree representation of the directory.
func BuildTree(root string, maxDepth int) (*TreeNode, error) {
	rootNode := &TreeNode{
		Name:  filepath.Base(root),
		Path:  root,
		IsDir: true,
	}

	err := buildTreeRecursive(rootNode, root, 1, maxDepth)
	if err != nil {
		return nil, err
	}
	return rootNode, nil
}

func buildTreeRecursive(node *TreeNode, dirPath string, depth, maxDepth int) error {
	if maxDepth > 0 && depth > maxDepth {
		return nil
	}

	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil
	}

	for _, e := range entries {
		name := e.Name()
		if skipDirs[name] || (strings.HasPrefix(name, ".") && name != ".") {
			continue
		}
		if e.Type()&os.ModeSymlink != 0 {
			continue
		}

		child := &TreeNode{
			Name:  name,
			Path:  filepath.Join(dirPath, name),
			IsDir: e.IsDir(),
		}

		if e.IsDir() {
			buildTreeRecursive(child, child.Path, depth+1, maxDepth)
		}

		node.Children = append(node.Children, child)
	}
	return nil
}

// PrintTree writes an ASCII tree to the writer.
func PrintTree(w io.Writer, node *TreeNode, prefix string) {
	if node == nil {
		return
	}

	io.WriteString(w, node.Name+"\n")

	for i, child := range node.Children {
		isLast := i == len(node.Children)-1
		connector := "├── "
		childPrefix := "│   "
		if isLast {
			connector = "└── "
			childPrefix = "    "
		}

		io.WriteString(w, prefix+connector)
		PrintTree(w, child, prefix+childPrefix)
	}
}
