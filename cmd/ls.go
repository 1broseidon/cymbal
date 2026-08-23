package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/1broseidon/cymbal/walker"
	"github.com/spf13/cobra"
)

var lsCmd = &cobra.Command{
	Use:   "ls [path|pattern]",
	Short: "Show file tree, indexed file names, repo list, or repo stats",
	Long: `Show the file tree of a directory (default), the flat list of indexed files
(--names), all indexed repos (--repos), or repo statistics (--stats).

--names lists repo-relative paths of the files cymbal indexed, one per line,
sorted — the code inventory after skip rules (generated/vendored/large files
excluded under default index options). The optional argument narrows it with the same semantics as the
--path/--exclude filters: substring for plain strings, glob with ** otherwise.
Filter by language with --lang (names as shown by 'cymbal ls --stats').`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		repos, _ := cmd.Flags().GetBool("repos")
		stats, _ := cmd.Flags().GetBool("stats")
		names, _ := cmd.Flags().GetBool("names")
		jsonOut := getJSONFlag(cmd)

		if repos {
			return lsRepos(jsonOut)
		}
		if stats {
			return lsStats(cmd, jsonOut)
		}
		if names {
			return lsNames(cmd, args, jsonOut)
		}
		return lsTree(cmd, args, jsonOut)
	},
}

func init() {
	lsCmd.Flags().Bool("repos", false, "list all indexed repositories")
	lsCmd.Flags().Bool("stats", false, "show repo overview (languages, file/symbol counts)")
	lsCmd.Flags().Bool("names", false, "flat list of indexed file paths (optionally narrowed by pattern)")
	lsCmd.Flags().String("lang", "", "with --names: only files of this language")
	lsCmd.Flags().Bool("null", false, "with --names: NUL-terminate entries (for xargs -0)")
	lsCmd.Flags().IntP("depth", "D", 0, "max tree depth (0 = unlimited)")
	rootCmd.AddCommand(lsCmd)
}

// lsNamesCheckFlags rejects contradictory output modes.
func lsNamesCheckFlags(nulSep, jsonOut bool) error {
	if nulSep && jsonOut {
		return fmt.Errorf("--null cannot be combined with --json")
	}
	return nil
}

// lsNames prints the sorted repo-relative paths of indexed files. Empty
// result is success (empty output; --json emits [], never null).
func lsNames(cmd *cobra.Command, args []string, jsonOut bool) error {
	dbPath := getDBPath(cmd)
	ensureFresh(dbPath)

	language, _ := cmd.Flags().GetString("lang")
	nulSep, _ := cmd.Flags().GetBool("null")
	pattern := ""
	if len(args) > 0 {
		pattern = args[0]
	}

	if err := lsNamesCheckFlags(nulSep, jsonOut); err != nil {
		return err
	}

	names, err := index.ListFileNames(dbPath, language, pattern)
	if err != nil {
		return err
	}
	if jsonOut {
		if names == nil {
			names = []string{}
		}
		return writeJSON(names)
	}
	sep := "\n"
	if nulSep {
		sep = "\x00"
	}
	for _, n := range names {
		fmt.Printf("%s%s", n, sep)
	}
	return nil
}

func lsRepos(jsonOut bool) error {
	repos, err := index.ListRepos()
	if err != nil {
		return err
	}

	if len(repos) == 0 {
		fmt.Fprintln(os.Stderr, "No indexed repositories. Run 'cymbal index <path>' first.")
		return nil
	}

	if jsonOut {
		return writeJSON(repos)
	}

	for _, r := range repos {
		fmt.Printf("%-50s  %d files  %d symbols\n",
			r.Path, r.FileCount, r.SymbolCount)
	}
	return nil
}

func lsStats(cmd *cobra.Command, jsonOut bool) error {
	dbPath := getDBPath(cmd)
	ensureFresh(dbPath)

	stats, err := index.RepoStats(dbPath)
	if err != nil {
		return err
	}

	if stats.Path == "" {
		return fmt.Errorf("no repo detected — run 'cymbal index <path>' or use --db")
	}

	var content strings.Builder
	for lang, count := range stats.Languages {
		fmt.Fprintf(&content, "%-16s %d files\n", lang, count)
	}

	return renderJSONOrFrontmatter(
		jsonOut,
		stats,
		[]kv{
			{"repo", stats.Path},
			{"files", fmt.Sprintf("%d", stats.FileCount)},
			{"symbols", fmt.Sprintf("%d", stats.SymbolCount)},
		},
		content.String(),
	)
}

func lsTree(cmd *cobra.Command, args []string, jsonOut bool) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	maxDepth, _ := cmd.Flags().GetInt("depth")

	tree, err := walker.BuildTree(absPath, maxDepth)
	if err != nil {
		return err
	}

	if jsonOut {
		return writeJSON(tree)
	}

	walker.PrintTree(os.Stdout, tree, "")
	return nil
}
