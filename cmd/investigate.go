package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var investigateCmd = &cobra.Command{
	Use:   "investigate <symbol>",
	Short: "Kind-adaptive investigation — returns the right context for what a symbol is",
	Long: `Investigate a symbol and get back the right shape of information
based on what it is. No need to choose between search, show, refs,
or impact — cymbal looks at the symbol's kind and returns what matters.

  function/method → source + callers + shallow impact
  class/struct/type/interface → source + members + references
  ambiguous → auto-resolves to best match, notes alternatives

Supports disambiguation:
  cymbal investigate Config              # auto-picks best match
  cymbal investigate config.go:Config    # file hint
  cymbal investigate auth.Middleware      # parent/package hint

Examples:
  cymbal investigate OpenStore
  cymbal investigate SymbolResult
  cymbal investigate config.Load
  cymbal investigate Foo Bar Baz     # batch: investigate multiple symbols`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)
		jsonOut := getJSONFlag(cmd)

		if jsonOut && len(args) > 1 {
			var all []any
			for _, name := range args {
				data := investigateOne(dbPath, name)
				all = append(all, data)
			}
			return writeJSON(all)
		}

		for i, name := range args {
			if i > 0 {
				fmt.Println()
			}
			if err := investigateOnePrint(dbPath, name, jsonOut); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", name, err)
			}
		}
		return nil
	},
}

func investigateOne(dbPath, name string) map[string]any {
	res, err := flexResolve(dbPath, name)
	if err != nil {
		return map[string]any{"symbol": name, "error": err.Error()}
	}
	if len(res.Results) == 0 {
		return map[string]any{"symbol": name, "error": "not found"}
	}
	sym := res.Results[0]
	result, err := index.InvestigateResolved(dbPath, sym)
	if err != nil {
		return map[string]any{"symbol": name, "error": err.Error()}
	}
	data := map[string]any{"result": result}
	if res.TotalFound > 1 {
		data["matches"] = res.TotalFound
	}
	if res.Fuzzy {
		data["fuzzy"] = true
	}
	return data
}

func investigateOnePrint(dbPath, name string, jsonOut bool) error {
	res, err := flexResolve(dbPath, name)
	if err != nil {
		return err
	}
	if len(res.Results) == 0 {
		return fmt.Errorf("symbol not found: %s", name)
	}

	sym := res.Results[0]
	result, err := index.InvestigateResolved(dbPath, sym)
	if err != nil {
		return err
	}

	if jsonOut {
		data := map[string]any{"result": result}
		if res.TotalFound > 1 {
			data["matches"] = res.TotalFound
		}
		if res.Fuzzy {
			data["fuzzy"] = true
		}
		return writeJSON(data)
	}

	var content strings.Builder

	content.WriteString("# Source\n")
	src := strings.TrimRight(result.Source, "\n")
	content.WriteString(src)
	content.WriteByte('\n')

	if len(result.Members) > 0 {
		fmt.Fprintf(&content, "\n# Members (%d)\n", len(result.Members))
		for _, m := range result.Members {
			fmt.Fprintf(&content, "  %-12s %s", m.Kind, m.Name)
			if m.Signature != "" {
				sig := m.Signature
				// Truncate multi-line signatures to first line.
				if nl := strings.IndexByte(sig, '\n'); nl >= 0 {
					sig = sig[:nl] + " ..."
				}
				fmt.Fprintf(&content, " %s", sig)
			}
			fmt.Fprintf(&content, "  %s:%d\n", m.RelPath, m.StartLine)
		}
	}

	if len(result.Refs) > 0 {
		var refs []refLine
		for _, r := range result.Refs {
			refs = append(refs, refLine{
				relPath: r.RelPath,
				line:    r.Line,
				text:    strings.TrimSpace(readSourceLine(r.File, r.Line)),
			})
		}
		lines, _ := dedupRefLines(refs)
		label := "References"
		if result.Kind == "function" {
			label = "Callers"
		}
		fmt.Fprintf(&content, "\n# %s (%d)\n", label, len(lines))
		for _, l := range lines {
			content.WriteString(l)
			content.WriteByte('\n')
		}
	}

	if len(result.Impact) > 0 {
		fmt.Fprintf(&content, "\n# Impact (depth 2)\n")
		for _, imp := range result.Impact {
			fmt.Fprintf(&content, "  [%d] %s → %s  %s:%d\n",
				imp.Depth, imp.Caller, imp.Symbol, imp.RelPath, imp.Line)
		}
	}

	if len(result.Implementors) > 0 {
		fmt.Fprintf(&content, "\n# Implementors (%d)\n", len(result.Implementors))
		for _, imp := range result.Implementors {
			name := imp.Implementer
			if name == "" {
				name = "(anonymous)"
			}
			tag := ""
			if !imp.Resolved {
				tag = "  (external)"
			}
			fmt.Fprintf(&content, "  %s  %s:%d%s\n", name, imp.RelPath, imp.Line, tag)
		}
	}

	if len(result.Implements) > 0 {
		fmt.Fprintf(&content, "\n# Implements (%d)\n", len(result.Implements))
		for _, imp := range result.Implements {
			tag := ""
			if !imp.Resolved {
				tag = "  (external)"
			}
			fmt.Fprintf(&content, "  %s  %s:%d%s\n", imp.Target, imp.RelPath, imp.Line, tag)
		}
	}

	meta := []kv{
		{"symbol", sym.Name},
		{"kind", sym.Kind},
		{"investigate", result.Kind},
		{"file", fmt.Sprintf("%s:%d", sym.RelPath, sym.StartLine)},
	}
	if res.TotalFound > 1 {
		also := make([]string, 0, len(res.Results)-1)
		for _, r := range res.Results[1:] {
			also = append(also, fmt.Sprintf("%s:%d", r.RelPath, r.StartLine))
		}
		meta = append(meta, kv{"matches", fmt.Sprintf("%d (also: %s)", res.TotalFound, strings.Join(also, ", "))})
	}
	if res.Fuzzy {
		meta = append(meta, kv{"fuzzy", "true"})
	}
	frontmatter(meta, content.String())
	return nil
}

func init() {
	rootCmd.AddCommand(investigateCmd)
}
