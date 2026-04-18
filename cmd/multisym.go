package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// Multi-symbol ergonomics shared by show, impls, impact, and trace.
//
// The goal is to let an agent ask one question about many symbols in a single
// turn. Design rules (see plans/ discussion):
//
//   * Single-symbol callers see no behavior change.
//   * Multi-symbol mode is triggered when len(args) > 1 OR --stdin is set.
//   * Each symbol is resolved independently. A not-found on one symbol warns
//     and continues; exit 0 as long as at least one succeeds.
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
