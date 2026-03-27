package cmd

import (
	"os"
	"path/filepath"

	"github.com/1broseidon/cymbal/internal/index"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cymbal",
	Short: "Fast code indexer and symbol discovery tool",
	Long: `Cymbal is a blazing-fast code indexer, parser, and symbol discovery CLI.
It uses tree-sitter for multi-language AST parsing and SQLite for indexed storage,
designed to be called by AI agents and developer tools.`,
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringP("db", "d", "", "path to cymbal database (default: auto per-repo)")
	rootCmd.PersistentFlags().Bool("json", false, "output as JSON")
}

// getDBPath returns the database path. If --db is set, use it.
// Otherwise, detect git root from CWD and compute per-repo path.
func getDBPath(cmd *cobra.Command) string {
	if p, _ := cmd.Flags().GetString("db"); p != "" {
		return p
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fallbackDBPath()
	}
	root, err := index.FindGitRoot(cwd)
	if err != nil {
		return fallbackDBPath()
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return fallbackDBPath()
	}
	dbPath, err := index.RepoDBPath(abs)
	if err != nil {
		return fallbackDBPath()
	}
	return dbPath
}

func fallbackDBPath() string {
	dbPath, err := index.RepoDBPath("_fallback")
	if err != nil {
		// Last resort: temp directory, never a relative path in the project.
		return filepath.Join(os.TempDir(), "cymbal", "cymbal.db")
	}
	return dbPath
}

func getJSONFlag(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("json")
	return v
}
