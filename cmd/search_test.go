package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeSearchMode(t *testing.T) {
	tests := []struct {
		name       string
		exact      bool
		ignoreCase bool
		textMode   bool
		wantExact  bool
		wantErr    bool
		errSubstr  string
	}{
		{
			name:      "plain fuzzy search unchanged",
			wantExact: false,
		},
		{
			name:      "exact search unchanged",
			exact:     true,
			wantExact: true,
		},
		{
			name:       "ignore-case implies exact",
			ignoreCase: true,
			wantExact:  true,
		},
		{
			name:       "ignore-case keeps explicit exact",
			exact:      true,
			ignoreCase: true,
			wantExact:  true,
		},
		{
			name:       "ignore-case with text mode errors",
			ignoreCase: true,
			textMode:   true,
			wantErr:    true,
			errSubstr:  "not supported with --text",
		},
		{
			name:       "ignore-case with exact and text mode errors",
			exact:      true,
			ignoreCase: true,
			textMode:   true,
			wantErr:    true,
			errSubstr:  "not supported with --text",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotExact, err := normalizeSearchMode(tc.exact, tc.ignoreCase, tc.textMode)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotExact != tc.wantExact {
				t.Fatalf("normalizeSearchMode(%t, %t, %t) = %t, want %t", tc.exact, tc.ignoreCase, tc.textMode, gotExact, tc.wantExact)
			}
		})
	}
}

func TestSplitSearchArgs(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		wantQuery string
		wantPaths []string
	}{
		{
			name:      "rg shaped text search treats trailing files as path filters",
			args:      []string{`os\.WriteFile\(`, "tools/file.go", "tools/patch.go"},
			wantQuery: `os\.WriteFile\(`,
			wantPaths: []string{"tools/file.go", "tools/patch.go"},
		},
		{
			name:      "multiword query stays intact without path operands",
			args:      []string{"error", "handling"},
			wantQuery: "error handling",
		},
		{
			name:      "glob path filter",
			args:      []string{"Handler", "internal/**/*.go"},
			wantQuery: "Handler",
			wantPaths: []string{"internal/**/*.go"},
		},
		{
			name:      "dot directory path becomes all paths glob",
			args:      []string{"Handler", "."},
			wantQuery: "Handler",
			wantPaths: []string{"**"},
		},
		{
			name:      "single path-shaped arg is still the query",
			args:      []string{"cmd/search.go"},
			wantQuery: "cmd/search.go",
		},
		{
			name:      "dotted symbol names are not file paths",
			args:      []string{"config.Load", "parser.Parse"},
			wantQuery: "config.Load parser.Parse",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotQuery, gotPaths := splitSearchArgs(tc.args)
			if gotQuery != tc.wantQuery {
				t.Fatalf("query = %q, want %q", gotQuery, tc.wantQuery)
			}
			if strings.Join(gotPaths, "\x00") != strings.Join(tc.wantPaths, "\x00") {
				t.Fatalf("paths = %#v, want %#v", gotPaths, tc.wantPaths)
			}
		})
	}
}

func TestNormalizeSearchPathOperandRelativeDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o755); err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	})

	if got := normalizeSearchPathOperand("subdir"); got != "subdir/**" {
		t.Fatalf("normalizeSearchPathOperand(subdir) = %q, want %q", got, "subdir/**")
	}
}
