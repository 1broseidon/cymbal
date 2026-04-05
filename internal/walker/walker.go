package walker

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileEntry is a file discovered during walking.
type FileEntry struct {
	Path     string
	RelPath  string
	Size     int64
	Language string
	ModTime  time.Time
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

// Language extension mapping.
var extToLang = map[string]string{
	".go":     "go",
	".py":     "python",
	".js":     "javascript",
	".jsx":    "javascript",
	".ts":     "typescript",
	".tsx":    "typescript",
	".rs":     "rust",
	".rb":     "ruby",
	".java":   "java",
	".c":      "c",
	".h":      "c",
	".cpp":    "cpp",
	".cc":     "cpp",
	".hpp":    "cpp",
	".cls":     "apex",
	".trigger": "apex",
	".cs":      "csharp",
	".swift":  "swift",
	".kt":     "kotlin",
	".lua":    "lua",
	".php":    "php",
	".sh":     "bash",
	".bash":   "bash",
	".zsh":    "bash",
	".zig":    "zig",
	".toml":   "toml",
	".yaml":   "yaml",
	".yml":    "yaml",
	".json":   "json",
	".md":     "markdown",
	".sql":    "sql",
	".proto":  "protobuf",
	".tf":     "hcl",
	".hcl":    "hcl",
	".ex":     "elixir",
	".exs":    "elixir",
	".erl":    "erlang",
	".hs":     "haskell",
	".ml":     "ocaml",
	".mli":    "ocaml",
	".scala":  "scala",
	".r":      "r",
	".R":      "r",
	".pl":     "perl",
	".pm":     "perl",
	".dart":   "dart",
	".vue":    "vue",
	".svelte": "svelte",
}

// LangForFile returns the language identifier for a file path.
func LangForFile(path string) string {
	ext := filepath.Ext(path)
	if lang, ok := extToLang[ext]; ok {
		return lang
	}
	// Check special filenames
	base := filepath.Base(path)
	switch base {
	case "Makefile", "makefile", "GNUmakefile":
		return "make"
	case "Dockerfile":
		return "dockerfile"
	case "Jenkinsfile":
		return "groovy"
	case "CMakeLists.txt":
		return "cmake"
	}
	return ""
}

// Walk concurrently discovers all source files under root.
// If langFilter is non-nil, only files whose language passes the filter are emitted.
// This avoids building FileEntry structs and stat-ing files that will be immediately
// skipped (e.g., .json, .md, .toml that the parser doesn't support).
func Walk(root string, workers int, langFilter func(string) bool) ([]FileEntry, error) {
	if workers <= 0 {
		workers = 8
	}

	var mu sync.Mutex
	var files []FileEntry

	ch := make(chan string, 256)
	var wg sync.WaitGroup

	// Spawn workers that process directories.
	for range workers {
		wg.Go(func() {
			for path := range ch {
				lang := LangForFile(path)
				if lang == "" {
					continue
				}
				if langFilter != nil && !langFilter(lang) {
					continue
				}
				info, err := os.Lstat(path)
				if err != nil {
					continue
				}
				rel, _ := filepath.Rel(root, path)
				entry := FileEntry{
					Path:     path,
					RelPath:  rel,
					Size:     info.Size(),
					Language: lang,
					ModTime:  info.ModTime(),
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
			return nil // skip errors
		}
		if d.IsDir() {
			name := d.Name()
			if skipDirs[name] || strings.HasPrefix(name, ".") && name != "." {
				return filepath.SkipDir
			}
			return nil
		}
		ch <- path
		return nil
	})
	close(ch)
	wg.Wait()

	if err != nil {
		return nil, err
	}

	sort.Slice(files, func(i, j int) bool {
		return files[i].RelPath < files[j].RelPath
	})
	return files, nil
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
