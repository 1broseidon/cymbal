package cmd

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/1broseidon/cymbal/index"
)

func TestSymbolJSONCarriesBodyHash(t *testing.T) {
	repo, db := t.TempDir(), filepath.Join(t.TempDir(), "index.db")
	writeFile(t, repo, "svc/service.go", "package svc\n\nfunc Target() int {\n\treturn helper()\n}\n\nfunc helper() int { return 1 }\n")
	if _, err := index.Index(repo, db, index.Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(index.CloseAll)
	file := filepath.Join(repo, "svc", "service.go")
	outline, err := index.FileOutline(db, file)
	if err != nil {
		t.Fatal(err)
	}
	var want string
	for _, sym := range outline {
		if sym.Name == "Target" {
			want = sym.BodyHash
		}
	}
	if want == "" {
		t.Fatalf("no body hash for Target in %+v", outline)
	}

	tests := []struct {
		name    string
		command *cobra.Command
		run     func(*cobra.Command) error
	}{
		{"show", newShowTestCommand(db), func(c *cobra.Command) error { return showCmd.RunE(c, []string{"Target"}) }},
		{"search", newSearchTestCommand(db), func(c *cobra.Command) error { return searchCmd.RunE(c, []string{"Target"}) }},
		{"outline", newOutlineTestCommand(db), func(c *cobra.Command) error { return outlineCmd.RunE(c, []string{file}) }},
		{"context", newContextTestCommand(db), func(c *cobra.Command) error { return contextCmd.RunE(c, []string{"Target"}) }},
		{"investigate", commandWithDB(db), func(c *cobra.Command) error { return investigateCmd.RunE(c, []string{"Target"}) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setTestFlag(t, tt.command, "json", "true")
			stdout, _, err := captureProcessOutput(t, func() error { return tt.run(tt.command) })
			if err != nil {
				t.Fatal(err)
			}
			var payload any
			if err := json.Unmarshal([]byte(stdout), &payload); err != nil {
				t.Fatalf("%v in %s", err, stdout)
			}
			got := bodyHashesOf(payload, "Target")
			if len(got) == 0 {
				t.Fatalf("no body_hash for Target in %s", stdout)
			}
			for _, hash := range got {
				if hash != want {
					t.Errorf("body_hash %q, want %q", hash, want)
				}
			}
		})
	}
}

// bodyHashesOf returns the body_hash of every JSON object named name, at any depth.
func bodyHashesOf(v any, name string) []string {
	var out []string
	switch v := v.(type) {
	case map[string]any:
		if v["name"] == name {
			if hash, ok := v["body_hash"].(string); ok {
				out = append(out, hash)
			}
		}
		for _, child := range v {
			out = append(out, bodyHashesOf(child, name)...)
		}
	case []any:
		for _, child := range v {
			out = append(out, bodyHashesOf(child, name)...)
		}
	}
	return out
}
