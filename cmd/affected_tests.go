package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

func newAffectedTestsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "affected-tests [file ...]",
		Short: "Select ranked test files with dependency evidence (advisory)",
		Long: `Select relevant test files from changed code, with ranked dependency evidence.
Defaults to unstaged changes. Use --staged or --base REF for another diff,
or pass files and/or --stdin for less precise whole-file analysis.

Selection is advisory. It does not prove unselected tests are safe to skip.
Partial or unavailable analysis recommends running the full suite.
Only the current repository index is used; sibling worktrees are not searched.`,
		RunE: runAffectedTests,
	}
	opts := index.DefaultAffectedTestsOptions()
	cmd.Flags().Bool("staged", false, "analyze staged changes (index vs HEAD)")
	cmd.Flags().String("base", "", "diff the working tree against a single ref")
	cmd.Flags().Bool("stdin", false, "read additional file paths from stdin, one per line")
	cmd.Flags().Bool("quiet", false, "print only selected paths to stdout; warnings go to stderr")
	cmd.Flags().IntP("depth", "D", opts.Depth, "max dependency depth (max 5)")
	cmd.Flags().IntP("limit", "n", opts.Limit, "max selected test files, after ranking")
	cmd.Flags().Int("max-symbols", opts.MaxSymbols, "max seed definitions (0 = unlimited)")
	cmd.Flags().Int("max-evidence", opts.MaxEvidence, "max examined dependency records (0 = unlimited)")
	cmd.Flags().StringArray("test-path", nil, "additional test path pattern (substring, or glob with **; repeatable)")
	addResolveScopeFlag(cmd)
	return cmd
}

func init() { rootCmd.AddCommand(newAffectedTestsCommand()) }

func runAffectedTests(cmd *cobra.Command, args []string) error {
	staged, _ := cmd.Flags().GetBool("staged")
	base, _ := cmd.Flags().GetString("base")
	stdin, _ := cmd.Flags().GetBool("stdin")
	quiet, _ := cmd.Flags().GetBool("quiet")
	jsonOut := getJSONFlag(cmd)
	explicit := len(args) > 0 || stdin
	if quiet && jsonOut {
		return fmt.Errorf("--json and --quiet are mutually exclusive")
	}
	if explicit && (cmd.Flags().Changed("staged") || cmd.Flags().Changed("base")) {
		return fmt.Errorf("file inputs cannot be combined with --staged or --base")
	}
	if _, _, err := changedDiffArgs(staged, base); err != nil {
		return err
	}
	opts := index.DefaultAffectedTestsOptions()
	opts.Depth, _ = cmd.Flags().GetInt("depth")
	opts.Limit, _ = cmd.Flags().GetInt("limit")
	opts.MaxSymbols, _ = cmd.Flags().GetInt("max-symbols")
	opts.MaxEvidence, _ = cmd.Flags().GetInt("max-evidence")
	opts.TestPaths, _ = cmd.Flags().GetStringArray("test-path")
	if opts.Depth <= 0 || opts.Limit <= 0 || opts.MaxSymbols < 0 || opts.MaxEvidence < 0 {
		return fmt.Errorf("depth and limit must be positive; max-symbols and max-evidence must not be negative")
	}
	var err error
	opts.ResolveScope, err = resolveScopeOrError(cmd)
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	root, err := gitRepoRoot(cwd)
	if err != nil {
		return fmt.Errorf("not a git repository: %w", err)
	}
	root = canonicalExistingPath(root)
	input := index.AffectedTestsInput{Precision: "hunk"}
	if explicit {
		paths := append([]string{}, args...)
		if stdin {
			paths, err = readAffectedPaths(cmd.InOrStdin(), paths)
			if err != nil {
				return err
			}
		}
		input.Precision = "file"
		input.Files, err = affectedInputPaths(root, cwd, paths)
		if err != nil {
			return err
		}
	} else {
		analysis, err := readChangedAnalysis(root, staged, base)
		if err != nil {
			return err
		}
		input = affectedDiffInput(analysis)
	}
	dbPath := getDBPath(cmd)
	// Refuse a foreign index before refreshing it. A missing DB is initialized
	// by ensureFresh, using this repository's root.
	if _, err := os.Stat(dbPath); err == nil {
		if err := affectedDBRoot(dbPath, root); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := ensureFresh(dbPath); err != nil {
		return err
	}
	if err := affectedDBRoot(dbPath, root); err != nil {
		return err
	}
	result, err := index.FindAffectedTests(dbPath, input, opts)
	if err != nil {
		return err
	}
	if jsonOut {
		return writeJSON(result)
	}
	if quiet {
		for _, test := range result.Tests {
			fmt.Println(test.Path)
		}
		fmt.Fprintln(os.Stderr, result.Fallback.Advisory)
		for _, gap := range result.Limitations {
			fmt.Fprintf(os.Stderr, "%s: %s %s\n", gap.Code, gap.Path, gap.Message)
		}
		if result.Fallback.Recommended {
			fmt.Fprintln(os.Stderr, result.Fallback.Reason)
		}
		return nil
	}
	var content strings.Builder
	for _, test := range result.Tests {
		fmt.Fprintf(&content, "%d. %s [%s]\n", test.Rank, test.Path, test.Confidence)
		for _, e := range test.Evidence {
			steps := make([]string, 0, len(e.Steps))
			for _, s := range e.Steps {
				label := s.Path
				if s.Line > 0 {
					label += fmt.Sprintf(":%d", s.Line)
				}
				if s.Name != "" {
					label += " " + s.Name
				}
				if s.ReferenceKind != "" {
					label += " [" + s.ReferenceKind + "]"
				}
				if s.Import != "" {
					label += " imports " + s.Import
				}
				steps = append(steps, label)
			}
			fmt.Fprintf(&content, "  %s: %s\n", e.Kind, strings.Join(steps, " -> "))
		}
	}
	for _, gap := range result.Limitations {
		fmt.Fprintf(&content, "warning: %s: %s %s\n", gap.Code, gap.Path, gap.Message)
	}
	if result.Fallback.Recommended {
		content.WriteString(result.Fallback.Reason + "\n")
	}
	content.WriteString(result.Fallback.Advisory + "\n" + result.Fallback.StaticAnalysis + "\n")
	frontmatter([]kv{{"status", result.Status}, {"selected_tests", fmt.Sprint(len(result.Tests))}, {"input_precision", result.InputPrecision}}, content.String())
	return nil
}

func affectedDBRoot(db, root string) error {
	store, err := index.OpenStore(db)
	if err != nil {
		return err
	}
	defer store.Close()
	stored, err := store.GetMeta("repo_root")
	if err != nil {
		return fmt.Errorf("reading index repository root: %w", err)
	}
	if stored == "" || canonicalExistingPath(stored) != root {
		return fmt.Errorf("index repository root %q does not match input repository %q", stored, root)
	}
	return nil
}

func readAffectedPaths(r io.Reader, paths []string) ([]string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	for scanner.Scan() {
		// ScanLines removes CRLF, but not spaces or leading '#'.
		if line := scanner.Text(); line != "" {
			paths = append(paths, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading file paths: %w", err)
	}
	return paths, nil
}

func affectedInputPaths(root, cwd string, paths []string) ([]string, error) {
	unique := map[string]bool{}
	for _, name := range paths {
		name = strings.ReplaceAll(name, "\\", "/")
		if name == "" {
			return nil, fmt.Errorf("empty file path")
		}
		if len(name) > 1 && name[1] == ':' && filepath.VolumeName(name) == "" {
			return nil, fmt.Errorf("file path is outside repository: %s", name)
		}
		if !filepath.IsAbs(name) {
			name = filepath.Join(cwd, name)
		}
		name = canonicalMissingPath(name)
		rel, err := filepath.Rel(root, name)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			return nil, fmt.Errorf("file path is outside repository: %s", name)
		}
		if fi, err := os.Stat(name); err == nil {
			if fi.IsDir() {
				return nil, fmt.Errorf("expected a file, not a directory: %s", name)
			}
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		unique[filepath.ToSlash(rel)] = true
	}
	out := make([]string, 0, len(unique))
	for p := range unique {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// Resolve the closest existing ancestor as well as the file: a missing path
// under a symlink must not bypass the repository-root check.
func canonicalMissingPath(p string) string {
	p = filepath.Clean(p)
	if _, err := os.Lstat(p); os.IsNotExist(err) {
		parent := filepath.Dir(p)
		if parent != p {
			return filepath.Join(canonicalMissingPath(parent), filepath.Base(p))
		}
	}
	return canonicalExistingPath(p)
}

func affectedDiffInput(a *changedAnalysis) index.AffectedTestsInput {
	input := index.AffectedTestsInput{Precision: "hunk", Files: []string{}, Symbols: []index.AffectedTestSymbol{}, Limitations: []index.AffectedTestsLimitation{}}
	for _, f := range a.Files {
		p := f.NewPath
		if p == "" {
			p = f.OldPath
		}
		input.Files = append(input.Files, p)
	}
	for _, seed := range a.Seeds {
		s := seed.Symbol
		if seed.IdentityUncertain {
			identity := index.AffectedTestSymbol{Path: s.File, Name: s.Name, Kind: s.Kind, Language: s.Language, StartLine: s.StartLine, EndLine: s.EndLine}
			input.Limitations = append(input.Limitations, index.AffectedTestsLimitation{Code: "ambiguous_blob_identity", Message: "Same-name definition counts changed between blobs; the old definition cannot be matched to a live seed.", Path: s.File, Seed: &identity})
			continue
		}
		// Attribution retains the old location, but graph lookup uses a unique
		// surviving new-blob definition when its range changed. The selector
		// still checks that identity against the working-tree index.
		if !seed.Deleted && seed.NewSymbol != nil {
			s = *seed.NewSymbol
		}
		input.Symbols = append(input.Symbols, index.AffectedTestSymbol{Path: s.File, Name: s.Name, Kind: s.Kind, Language: s.Language, StartLine: s.StartLine, EndLine: s.EndLine, Deleted: seed.Deleted})
	}
	for _, gap := range a.Limitations {
		message := gap.Message
		if gap.Side != "" {
			message = gap.Side + " blob: " + message
		}
		input.Limitations = append(input.Limitations, index.AffectedTestsLimitation{Code: gap.Code, Message: message, Path: gap.Path})
		// Old-side diagnostics keep their original location. Renamed files
		// also need file fallback at the new path where the live graph lives.
		if gap.Code == "outside_symbols" && gap.Side == "old" {
			for _, f := range a.Files {
				if f.OldPath == gap.Path && f.NewPath != "" && f.NewPath != f.OldPath {
					input.Limitations = append(input.Limitations, index.AffectedTestsLimitation{Code: gap.Code, Message: message, Path: f.NewPath})
				}
			}
		}
	}
	return input
}
