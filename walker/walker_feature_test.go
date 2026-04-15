package walker

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/1broseidon/cymbal/lang"
)

func createTestTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Source files
	files := map[string]string{
		"main.go":            "package main",
		"lib/utils.go":       "package lib",
		"lib/helpers.py":     "def helper(): pass",
		"frontend/app.js":    "function app() {}",
		"frontend/types.ts":  "interface Props {}",
		"src/lib.rs":         "fn main() {}",
		"src/nested/deep.go": "package deep",
	}

	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Directories that should be skipped
	skipDirFiles := map[string]string{
		".git/config":               "gitconfig",
		"node_modules/pkg/index.js": "module.exports = {}",
		"vendor/dep/dep.go":         "package dep",
		"__pycache__/cache.pyc":     "bytecode",
		".hidden/secret.go":         "package secret",
	}

	for path, content := range skipDirFiles {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	// Empty directory
	if err := os.MkdirAll(filepath.Join(dir, "empty_dir"), 0755); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestFeatureWalkerFindsSupportedFiles(t *testing.T) {
	dir := createTestTree(t)

	files, err := Walk(dir, 4, nil)
	if err != nil {
		t.Fatal(err)
	}

	// Should find all source files (no filter)
	foundPaths := make(map[string]bool)
	for _, f := range files {
		foundPaths[f.RelPath] = true
	}

	expected := []string{
		"main.go",
		filepath.Join("lib", "utils.go"),
		filepath.Join("lib", "helpers.py"),
		filepath.Join("frontend", "app.js"),
		filepath.Join("frontend", "types.ts"),
		filepath.Join("src", "lib.rs"),
		filepath.Join("src", "nested", "deep.go"),
	}

	for _, exp := range expected {
		if !foundPaths[exp] {
			t.Errorf("expected to find %s, but didn't. Found: %v", exp, foundPaths)
		}
	}
}

func TestFeatureWalkerSkipsGitDir(t *testing.T) {
	dir := createTestTree(t)

	files, err := Walk(dir, 4, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		if filepath.Base(filepath.Dir(f.Path)) == ".git" || f.RelPath == ".git/config" {
			t.Errorf("should not find files in .git: %s", f.RelPath)
		}
	}
}

func TestFeatureWalkerSkipsNodeModules(t *testing.T) {
	dir := createTestTree(t)

	files, err := Walk(dir, 4, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		for _, part := range filepath.SplitList(f.RelPath) {
			if part == "node_modules" {
				t.Errorf("should not find files in node_modules: %s", f.RelPath)
			}
		}
		if len(f.RelPath) > 12 && f.RelPath[:12] == "node_modules" {
			t.Errorf("should not find files in node_modules: %s", f.RelPath)
		}
	}
}

func TestFeatureWalkerSkipsVendor(t *testing.T) {
	dir := createTestTree(t)

	files, err := Walk(dir, 4, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		if len(f.RelPath) >= 6 && f.RelPath[:6] == "vendor" {
			t.Errorf("should not find files in vendor: %s", f.RelPath)
		}
	}
}

func TestFeatureWalkerSkipsPycache(t *testing.T) {
	dir := createTestTree(t)

	files, err := Walk(dir, 4, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		if len(f.RelPath) >= 11 && f.RelPath[:11] == "__pycache__" {
			t.Errorf("should not find files in __pycache__: %s", f.RelPath)
		}
	}
}

func TestFeatureWalkerSkipsDotDirs(t *testing.T) {
	dir := createTestTree(t)

	files, err := Walk(dir, 4, nil)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		if len(f.RelPath) >= 7 && f.RelPath[:7] == ".hidden" {
			t.Errorf("should not find files in dotdirs: %s", f.RelPath)
		}
	}
}

func TestFeatureWalkerLanguageDetection(t *testing.T) {
	tests := []struct {
		file string
		lang string
	}{
		{"test.go", "go"},
		{"test.py", "python"},
		{"test.js", "javascript"},
		{"test.jsx", "javascript"},
		{"test.ts", "typescript"},
		{"test.tsx", "typescript"},
		{"test.rs", "rust"},
		{"test.rb", "ruby"},
		{"test.java", "java"},
		{"test.c", "c"},
		{"test.cpp", "cpp"},
		{"test.cs", "csharp"},
		{"test.swift", "swift"},
		{"test.kt", "kotlin"},
		{"test.sh", "bash"},
		{"test.scala", "scala"},
		{"test.yaml", "yaml"},
		{"test.yml", "yaml"},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			lang := LangForFile(tt.file)
			if lang != tt.lang {
				t.Errorf("LangForFile(%q) = %q, want %q", tt.file, lang, tt.lang)
			}
		})
	}
}

func TestFeatureWalkerUnknownExtension(t *testing.T) {
	lang := LangForFile("test.xyz")
	if lang != "" {
		t.Errorf("expected empty language for unknown extension, got %q", lang)
	}
}

func TestFeatureWalkerRegistryIssue19Extensions(t *testing.T) {
	tests := []struct {
		file string
		lang string
	}{
		{"test.cxx", "cpp"},
		{"test.hxx", "cpp"},
		{"test.hh", "cpp"},
		{"test.mjs", "javascript"},
		{"test.cjs", "javascript"},
		{"test.mts", "typescript"},
		{"test.cts", "typescript"},
		{"test.pyw", "python"},
		{"test.kts", "kotlin"},
		{"test.rake", "ruby"},
		{"test.gemspec", "ruby"},
		{"test.sc", "scala"},
		{"test.tfvars", "hcl"},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			got := LangForFile(tt.file)
			if got != tt.lang {
				t.Errorf("LangForFile(%q) = %q, want %q", tt.file, got, tt.lang)
			}
		})
	}
}

func TestFeatureWalkerWithSupportedFilterSkipsRecognitionOnly(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"main.go":       "package main",
		"module.mjs":    "export const x = 1",
		"vars.tfvars":   "name = \"demo\"",
		"config.json":   "{}",
		"README.md":     "# hi",
		"Dockerfile":    "FROM alpine",
		"Makefile":      "all:\n\techo hi\n",
		"settings.toml": "title = \"demo\"",
	}
	for rel, content := range files {
		full := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	walked, err := Walk(dir, 2, lang.Default.Supported)
	if err != nil {
		t.Fatal(err)
	}

	got := make(map[string]string, len(walked))
	for _, f := range walked {
		got[f.RelPath] = f.Language
		if !lang.Default.Supported(f.Language) {
			t.Fatalf("Walk returned unsupported language %q for %s", f.Language, f.RelPath)
		}
	}

	want := map[string]string{
		"main.go":     "go",
		"module.mjs":  "javascript",
		"vars.tfvars": "hcl",
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d parseable files, got %d: %#v", len(want), len(got), got)
	}
	for rel, language := range want {
		if got[rel] != language {
			t.Errorf("Walk(..., lang.Default.Supported) missing or wrong for %s: got %q want %q", rel, got[rel], language)
		}
	}
	for _, rel := range []string{"config.json", "README.md", "Dockerfile", "Makefile", "settings.toml"} {
		if _, ok := got[rel]; ok {
			t.Errorf("Walk(..., lang.Default.Supported) should skip non-parseable file %s", rel)
		}
	}
}

func TestFeatureWalkerEmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	files, err := Walk(dir, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files in empty directory, got %d", len(files))
	}
}

func TestFeatureWalkerWithLangFilter(t *testing.T) {
	dir := createTestTree(t)

	// Only accept Go files
	goOnly := func(lang string) bool {
		return lang == "go"
	}

	files, err := Walk(dir, 4, goOnly)
	if err != nil {
		t.Fatal(err)
	}

	for _, f := range files {
		if f.Language != "go" {
			t.Errorf("expected only Go files, got %s (%s)", f.RelPath, f.Language)
		}
	}

	if len(files) != 3 {
		t.Errorf("expected 3 Go files, got %d", len(files))
	}
}

func TestFeatureWalkerFileEntryFields(t *testing.T) {
	dir := t.TempDir()
	content := "package main\nfunc main() {}\n"
	testFile := filepath.Join(dir, "main.go")
	if err := os.WriteFile(testFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	files, err := Walk(dir, 4, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}

	f := files[0]
	if f.Path != testFile {
		t.Errorf("expected Path %s, got %s", testFile, f.Path)
	}
	if f.RelPath != "main.go" {
		t.Errorf("expected RelPath 'main.go', got %s", f.RelPath)
	}
	if f.Language != "go" {
		t.Errorf("expected Language 'go', got %s", f.Language)
	}
	if f.Size != int64(len(content)) {
		t.Errorf("expected Size %d, got %d", len(content), f.Size)
	}
	if f.ModTime.IsZero() {
		t.Error("expected non-zero ModTime")
	}
}

func TestFeatureWalkerSpecialFilenames(t *testing.T) {
	tests := []struct {
		filename string
		lang     string
	}{
		{"Makefile", "make"},
		{"Dockerfile", "dockerfile"},
		{"Jenkinsfile", "groovy"},
		{"CMakeLists.txt", "cmake"},
	}

	for _, tt := range tests {
		t.Run(tt.filename, func(t *testing.T) {
			lang := LangForFile(tt.filename)
			if lang != tt.lang {
				t.Errorf("LangForFile(%q) = %q, want %q", tt.filename, lang, tt.lang)
			}
		})
	}
}

func TestFeatureWalkerResultsSorted(t *testing.T) {
	dir := createTestTree(t)

	files, err := Walk(dir, 4, nil)
	if err != nil {
		t.Fatal(err)
	}

	for i := 1; i < len(files); i++ {
		if files[i].RelPath < files[i-1].RelPath {
			t.Errorf("results not sorted: %s came after %s", files[i].RelPath, files[i-1].RelPath)
		}
	}
}
