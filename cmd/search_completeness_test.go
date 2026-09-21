package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/1broseidon/cymbal/index"
)

func TestSearchAndRefsFilterBeforeLimit(t *testing.T) {
	repo, db := t.TempDir(), filepath.Join(t.TempDir(), "index.db")
	for i := range 151 {
		writeFile(t, repo, fmt.Sprintf("pkg%03d/handler.go", i), "package service\nfunc Handle() {}\nfunc Caller() { Handle() }\n")
	}
	if _, err := index.Index(repo, db, index.Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(index.CloseAll)
	for _, exact := range []bool{false, true} {
		for _, ignoreCase := range []bool{false, true} {
			query := "Handle"
			if ignoreCase {
				query = "handle"
			}
			// Repeated includes are ORed, then exclusions remove the early candidate.
			results, missing, err := searchSymbolQueries(db, []string{query}, "", "", exact, ignoreCase, 1,
				[]string{"pkg000/**", "pkg150/**"}, []string{"pkg000"})
			if err != nil || len(missing) != 0 || len(results) != 1 || filepath.ToSlash(results[0].RelPath) != "pkg150/handler.go" {
				t.Fatalf("exact=%v ignoreCase=%v results=%+v missing=%v err=%v", exact, ignoreCase, results, missing, err)
			}
		}
	}
	command := newRefsTestCommand(db)
	setTestFlag(t, command, "path", "pkg150/**")
	setTestFlag(t, command, "limit", "1")
	setTestFlag(t, command, "json", "true")
	stdout, _, err := captureProcessOutput(t, func() error { return refsCmd.RunE(command, []string{"Handle"}) })
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Results []index.RefResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	if len(envelope.Results) != 1 || filepath.ToSlash(envelope.Results[0].RelPath) != "pkg150/handler.go" {
		t.Fatalf("references=%+v", envelope.Results)
	}
}

func textParityRepo(t *testing.T) string {
	t.Helper()
	repo, db := t.TempDir(), filepath.Join(t.TempDir(), "index.db")
	writeFile(t, repo, "a.go", "package fixture\n// Alpha\n// Beta\n// café\n// "+strings.Repeat("x", 220)+"\n")
	writeFile(t, repo, "b.go", "package fixture\r\n// Alpha\r\n")
	writeFile(t, repo, "ignored.go", "package fixture\n// Alpha\n")
	writeFile(t, repo, ".gitignore", "ignored.go\n")
	writeFile(t, repo, "excluded.go", "package fixture\n// Alpha\n")
	writeFile(t, repo, "generated.pb.go", "package fixture\n// Alpha\n")
	writeFile(t, repo, "README.md", "Alpha\n")
	writeFile(t, repo, "front.ts", "// Alpha\n")
	if _, err := index.Index(repo, db, index.Options{Workers: 1, Exclude: []string{"excluded.go"}}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(index.CloseAll)
	return db
}

func decodeTextResults(t *testing.T, run func() error) []index.TextResult {
	t.Helper()
	stdout, _, err := captureProcessOutput(t, run)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Version string             `json:"version"`
		Results []index.TextResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(stdout), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Version != "0.1" {
		t.Fatalf("unexpected envelope: %s", stdout)
	}
	return envelope.Results
}

func TestTextSearchBackendParity(t *testing.T) {
	db := textParityRepo(t)
	rg, rgErr := exec.LookPath("rg")
	for _, tc := range []struct {
		pattern            string
		limit              int
		includes, excludes []string
	}{
		{"Alpha|Beta", 50, nil, nil},
		{"(?i)alpha", 1, nil, nil},
		{`^// \w+$`, 50, nil, nil},
		{`Alpha$`, 50, nil, nil},
		{"x+", 50, nil, nil},
		{"Alpha", 1, []string{"ignored.go"}, nil},
		{"Alpha", 1, []string{"a.go", "ignored.go"}, []string{"a.go"}},
	} {
		t.Run(tc.pattern+fmt.Sprint(tc.limit, tc.includes), func(t *testing.T) {
			goResults := decodeTextResults(t, func() error { return searchTextGo(db, tc.pattern, "go", tc.limit, true, tc.includes, tc.excludes) })
			for _, result := range goResults {
				if result.RelPath == "excluded.go" || result.RelPath == "generated.pb.go" || result.RelPath == "README.md" || result.RelPath == "front.ts" {
					t.Fatalf("ineligible file: %+v", result)
				}
			}
			if rgErr != nil {
				t.Log("ripgrep unavailable; native assertions still checked")
				return
			}
			rgResults := decodeTextResults(t, func() error { return searchTextRg(rg, db, tc.pattern, "go", tc.limit, true, tc.includes, tc.excludes) })
			if !reflect.DeepEqual(goResults, rgResults) {
				t.Fatalf("native=%+v\nripgrep=%+v", goResults, rgResults)
			}
		})
	}
	// Exercise automatic fallback as well as the direct backend helpers.
	t.Setenv("PATH", t.TempDir())
	results := decodeTextResults(t, func() error { return searchText(db, "Alpha|Beta", "go", 50, true, nil, nil) })
	if len(results) != 4 {
		t.Fatalf("fallback regex results=%+v", results)
	}
}

func TestTextSearchRejectsInvalidPatterns(t *testing.T) {
	db := textParityRepo(t)
	for _, run := range []func() error{
		func() error { return searchTextGo(db, "[", "", 20, false, nil, nil) },
		// Validation happens before starting ripgrep, even with an unusable executable.
		func() error { return searchTextRg("not-an-executable", db, "[", "", 20, false, nil, nil) },
	} {
		_, _, err := captureProcessOutput(t, run)
		if err == nil || !strings.Contains(err.Error(), "invalid search pattern") {
			t.Fatalf("error=%v", err)
		}
	}
}

func TestTextSearchReportsFileErrors(t *testing.T) {
	db := textParityRepo(t)
	files, err := index.TextFiles(db, "go", index.PathFilter{Include: []string{"a.go"}})
	if err != nil || len(files) != 1 {
		t.Fatalf("files=%v err=%v", files, err)
	}
	if err := os.Remove(files[0].Path); err != nil {
		t.Fatal(err)
	}
	_, _, err = captureProcessOutput(t, func() error { return searchTextGo(db, "Alpha", "go", 20, false, []string{"a.go"}, nil) })
	if err == nil || !strings.Contains(err.Error(), "reading a.go") {
		t.Fatalf("error=%v", err)
	}
	if rg, err := exec.LookPath("rg"); err == nil {
		_, _, err := captureProcessOutput(t, func() error { return searchTextRg(rg, db, "Alpha", "go", 20, false, []string{"a.go"}, nil) })
		if err == nil || !strings.Contains(err.Error(), "text search failed") {
			t.Fatalf("error=%v", err)
		}
	}
}
