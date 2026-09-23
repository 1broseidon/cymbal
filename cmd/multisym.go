package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

// ambiguousSymbolLanguages returns, for each requested name that is defined in
// more than one language, the sorted list of those languages. Names defined in
// a single language (or none) are omitted. Used by trace/impact to flag a
// starting symbol that spans languages — resolve-scope governs how a call
// resolves per language, it does not pick between the same-named symbols.
func ambiguousSymbolLanguages(plan DBPlan, names []string) map[string][]string {
	out := map[string][]string{}
	for _, name := range names {
		if _, ok := out[name]; ok {
			continue
		}
		entry, _ := findSymbolEntry(plan, name)
		langs, err := index.SymbolLanguages(entry.Path, name)
		if err != nil || len(langs) < 2 {
			continue
		}
		sort.Strings(langs)
		out[name] = langs
	}
	return out
}

// formatSymbolLanguages renders ambiguousSymbolLanguages for frontmatter, e.g.
// "App=go,tsx; Bar=java,kotlin". Empty when no name spans languages.
func formatSymbolLanguages(m map[string][]string) string {
	if len(m) == 0 {
		return ""
	}
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, n := range names {
		parts = append(parts, n+"="+strings.Join(m[n], ","))
	}
	return strings.Join(parts, "; ")
}

// Multi-symbol ergonomics shared by show, impls, impact, and trace.
//
// The goal is to let an agent ask one question about many symbols in a single
// turn. Design rules (see plans/ discussion):
//
//   * Single-symbol callers see no behavior change.
//   * Multi-symbol mode is triggered when len(args) > 1 OR --stdin is set.
//   * Each symbol is resolved independently, and a not-found on one does not
//     stop the rest. show exits 0 as long as one succeeds; impact, trace and
//     impls, like refs and investigate, report a name the index has never
//     seen through the exit status (see keepIndexed).
//   * User's argument order is preserved — we never sort alphabetically.
//   * --limit applies per symbol, never as a total cap across symbols.
//   * For commands that return shared edges (impact/trace), rows are deduped
//     by (file, line, callee-or-caller) and the list of originating symbols is
//     recorded in a `hit_symbols` attribution list. Denser output is the whole
//     point of this feature.

// addStdinFlag registers --stdin on a command so callers can pipe
// newline-separated symbol names in:  cymbal outline foo.go --names | cymbal show --stdin
func addStdinFlag(cmd *cobra.Command) {
	cmd.Flags().Bool("stdin", false, "read additional symbol names (newline-separated) from stdin")
}

// collectSymbols merges positional args with --stdin input. Duplicates are
// removed while preserving first-seen order, and empty / comment-prefixed
// lines from stdin are skipped.
func collectSymbols(cmd *cobra.Command, args []string) ([]string, error) {
	useStdin, _ := cmd.Flags().GetBool("stdin")
	seen := make(map[string]struct{}, len(args))
	out := make([]string, 0, len(args))
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" || strings.HasPrefix(s, "#") {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	for _, a := range args {
		add(a)
	}
	if useStdin {
		sc := bufio.NewScanner(os.Stdin)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		for sc.Scan() {
			add(sc.Text())
		}
		if err := sc.Err(); err != nil {
			return nil, fmt.Errorf("reading --stdin: %w", err)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no symbol names provided (positional args or --stdin)")
	}
	return out, nil
}

// errSymbolNotFound marks a requested name the index has never seen, so a typo
// exits nonzero instead of looking like an empty answer.
var errSymbolNotFound = errors.New("symbol not found")

// requireIndexed returns errSymbolNotFound unless the index has a definition of
// name or any reference to it, such as a call to an external function. A name
// it knows is answered even when the answer is empty.
func requireIndexed(dbPath, name string) error {
	langs, err := index.SymbolLanguages(dbPath, name)
	if err != nil || len(langs) > 0 {
		return err
	}
	refs, err := index.FindReferencesWithPaths(dbPath, name, 1, index.PathFilter{})
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return fmt.Errorf("%w: %s", errSymbolNotFound, name)
	}
	return nil
}

// keepIndexed returns the names requireIndexed accepts, in order, and a failure
// for each of the rest. dbForName routes a name to the database its query runs
// against.
func keepIndexed(names []string, dbForName func(string) string) ([]string, []error) {
	known := make([]string, 0, len(names))
	var failures []error
	for _, name := range names {
		if err := requireIndexed(dbForName(name), name); err != nil {
			failures = append(failures, nameFailure(name, err))
			continue
		}
		known = append(known, name)
	}
	return known, failures
}

// nameFailure labels a batch failure with its name. A not-found error already
// carries the name.
func nameFailure(name string, err error) error {
	if errors.Is(err, errSymbolNotFound) {
		return err
	}
	return fmt.Errorf("%s: %w", name, err)
}

// multiSymbolBanner prints a "═══ <symbol> ═══" separator before each section
// in human output. Skipped when there's only one symbol so single-arg output
// is unchanged.
func multiSymbolBanner(name string, first bool) {
	if first {
		return
	}
	fmt.Println()
}

// multiSymbolHeader is the per-symbol header emitted inside a multi run before
// falling through to the normal frontmatter output. Deliberately minimal so
// it layers cleanly on top of the existing single-symbol rendering paths.
func multiSymbolHeader(name string) {
	fmt.Printf("═══ %s ═══\n", name)
}
