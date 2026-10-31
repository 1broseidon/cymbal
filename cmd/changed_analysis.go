package cmd

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/1broseidon/cymbal/lang"
	"github.com/1broseidon/cymbal/parser"
	"github.com/1broseidon/cymbal/symbols"
)

// maxChangedParseBytes mirrors the walker's large-file skip threshold.
const maxChangedParseBytes = 3407872 // 3.25 MiB

// changedFile is one file's diff: which old- and new-side line numbers changed,
// and the path on each side ("" when the side is /dev/null).
type changedFile struct {
	OldPath, NewPath string
	OldLines         map[int]bool
	NewLines         map[int]bool
	Binary           bool
}

// parseChangedFiles walks unified diff output into per-file changed-line sets.
// New-side line numbers track '+' (added) lines; old-side track '-' (deleted)
// lines. Attributing on changed lines — never whole hunks (which include
// unchanged context) — keeps neighbouring symbols from being falsely flagged.
func parseChangedFiles(diff string) []changedFile {
	var files []changedFile
	var cur *changedFile
	oldLine, newLine := 0, 0
	inHunk := false
	flush := func() {
		if cur != nil {
			files = append(files, *cur)
			cur = nil
		}
	}

	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			cur = &changedFile{OldLines: map[int]bool{}, NewLines: map[int]bool{}}
			inHunk = false
		case strings.HasPrefix(line, "diff --cc "), strings.HasPrefix(line, "diff --combined "):
			// Combined diff (merge conflict): not analyzable as a two-side diff.
			// Flush and absorb the whole block; RunE counts these for a warning.
			flush()
			inHunk = false
		case cur == nil:
			// preamble before the first file header
		case strings.HasPrefix(line, "@@"):
			if o, n, ok := parseHunkStarts(line); ok {
				oldLine, newLine = o, n
				inHunk = true
			}
		case inHunk:
			// Hunk body lines are matched before the header prefixes: a deleted
			// line whose content starts with "-- " renders as "--- ..." (and an
			// added "++ " as "+++ ..."), which must count as a changed line, not
			// reopen the file header — headers only occur before the first hunk.
			switch {
			case strings.HasPrefix(line, "\\"):
				// "\ No newline at end of file"
			case strings.HasPrefix(line, "+"):
				if newLine > 0 {
					cur.NewLines[newLine] = true
				}
				newLine++
			case strings.HasPrefix(line, "-"):
				if oldLine > 0 {
					cur.OldLines[oldLine] = true
				}
				oldLine++
			default:
				// context line (leading space) or blank
				oldLine++
				newLine++
			}
		case strings.HasPrefix(line, "Binary files "):
			cur.Binary = true
		case strings.HasPrefix(line, "--- "):
			cur.OldPath = diffPathSide(strings.TrimPrefix(line, "--- "))
		case strings.HasPrefix(line, "+++ "):
			cur.NewPath = diffPathSide(strings.TrimPrefix(line, "+++ "))
		default:
			// metadata between header and first hunk (index, mode, rename …)
		}
	}
	flush()
	return files
}

// countConflictBlocks counts combined-diff file blocks ("diff --cc"/"diff
// --combined"), which git emits for unmerged paths mid-conflict. They cannot be
// attributed as a two-side diff, so `changed` surfaces them as a warning
// instead of silently ignoring them.
func countConflictBlocks(diff string) int {
	n := 0
	for _, line := range strings.Split(diff, "\n") {
		if strings.HasPrefix(line, "diff --cc ") || strings.HasPrefix(line, "diff --combined ") {
			n++
		}
	}
	return n
}

// diffPathSide decodes one side of a diff file header: it un-C-quotes the path
// (git escapes tabs, quotes, backslashes, and — without core.quotePath=false —
// high bytes), maps /dev/null to "", and strips the a/ or b/ prefix. It must
// not trim surrounding whitespace, which can be part of a filename — except
// exactly one trailing tab, which git appends as a header/annotation separator
// when the (unquoted) filename contains a space; a filename containing a real
// tab is always C-quoted, so the quoted form never carries a separator tab.
func diffPathSide(p string) string {
	p = strings.TrimSuffix(p, "\r")
	if !strings.HasPrefix(p, `"`) {
		p = strings.TrimSuffix(p, "\t")
	}
	if strings.HasPrefix(p, `"`) {
		if dec, err := strconv.Unquote(p); err == nil {
			p = dec
		}
	}
	if p == "/dev/null" {
		return ""
	}
	switch {
	case strings.HasPrefix(p, "a/"):
		return p[2:]
	case strings.HasPrefix(p, "b/"):
		return p[2:]
	}
	return p
}

// parseHunkStarts reads the old- and new-side starting line numbers from a
// unified diff hunk header "@@ -oldStart[,c] +newStart[,c] @@". Zero is a valid
// start (empty side of an add/delete), so callers must not require > 0.
func parseHunkStarts(header string) (oldStart, newStart int, ok bool) {
	minus := strings.IndexByte(header, '-')
	plus := strings.IndexByte(header, '+')
	if minus < 0 || plus < 0 {
		return 0, 0, false
	}
	return leadingInt(header[minus+1:]), leadingInt(header[plus+1:]), true
}

func leadingInt(s string) int {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	n, _ := strconv.Atoi(s[:i])
	return n
}

// readNewSide returns the new-side content: the staged blob under --staged, the
// working-tree file otherwise. A working-tree symlink is rejected — git's blob
// for a symlink is the link target text, so following it would parse the wrong
// file's content.
func readNewSide(repoRoot string, staged bool, path string) ([]byte, error) {
	if staged {
		return catFileBlob(repoRoot, ":"+path)
	}
	full := filepath.Join(repoRoot, path)
	if fi, err := os.Lstat(full); err == nil && fi.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("symlink: %s", path)
	}
	return os.ReadFile(full)
}

// readOldSide returns the base-revision blob for a path.
func readOldSide(repoRoot, oldRev, path string) ([]byte, error) {
	return catFileBlob(repoRoot, oldRev+":"+path)
}

func catFileBlob(repoRoot, spec string) ([]byte, error) {
	return exec.Command("git", "-C", repoRoot, "cat-file", "blob", spec).Output()
}

// parseBlobSymbols parses in-memory content for a path into symbols. The bool is
// false when the content can't or shouldn't be parsed (too large, binary, a
// recognised-but-not-parseable language, or a parser error) — distinct from a
// successful parse that simply yields no symbols (true, empty slice). Callers
// must not treat a parse failure as "no symbols here", or deletions inferred
// from an empty new side would be wrong. Line numbers are 1-based, matching the
// diff; CRLF is left intact (git and tree-sitter both count LF rows).
func parseBlobSymbols(src []byte, path string) ([]symbols.Symbol, bool) {
	syms, code, _ := parseBlobSymbolsDetailed(src, path)
	return syms, code == ""
}

func parseBlobSymbolsDetailed(src []byte, path string) ([]symbols.Symbol, string, string) {
	if len(src) > maxChangedParseBytes {
		return nil, "oversized", "blob exceeds the attribution size limit"
	}
	if bytes.IndexByte(src, 0) >= 0 {
		return nil, "binary", "blob contains NUL bytes"
	}
	l := lang.Default.ForFile(path)
	if l == nil && filepath.Ext(path) == "" {
		l = lang.Default.ForShebang(src)
	}
	if l == nil || !l.Parseable() {
		return nil, "unsupported", "blob language is not supported for parsing"
	}
	src = bytes.TrimPrefix(src, []byte{0xEF, 0xBB, 0xBF})
	res, err := parser.ParseBytes(src, path, l.Name)
	if err != nil {
		return nil, "parse_failed", err.Error()
	}
	if res == nil {
		return nil, "parse_failed", "parser returned no result"
	}
	return res.Symbols, "", ""
}

// innermostChanged returns the most specific navigable definition containing
// each changed line (smallest range wins, so a method beats its enclosing
// class), deduplicated by name. Function-local declarations are skipped so a
// change inside a body is attributed to the function, not a local on that line.
func innermostChanged(syms []symbols.Symbol, lines map[int]bool) []symbols.Symbol {
	seen := map[string]bool{}
	var out []symbols.Symbol
	for _, sym := range innermostChangedDefinitions(syms, lines) {
		if !seen[sym.Name] {
			seen[sym.Name] = true
			out = append(out, sym)
		}
	}
	return out
}

// innermostChangedDefinitions retains distinct same-name definitions. Only the
// legacy wrapper collapses names; selectors need path and location identity.
func innermostChangedDefinitions(syms []symbols.Symbol, lines map[int]bool) []symbols.Symbol {
	if len(lines) == 0 {
		return nil
	}
	sorted := make([]int, 0, len(lines))
	for line := range lines {
		sorted = append(sorted, line)
	}
	sort.Ints(sorted)

	// Per changed line, the most specific containing definition. Visiting only
	// the changed lines inside each symbol's range (binary search into the
	// sorted lines) avoids the O(lines × symbols) scan on huge rewrites.
	best := make(map[int]*symbols.Symbol, len(sorted))
	for i := range syms {
		s := &syms[i]
		if !symbols.IsNavigableDefinition(s.Kind, s.Depth) {
			continue
		}
		for j := sort.SearchInts(sorted, s.StartLine); j < len(sorted) && sorted[j] <= s.EndLine; j++ {
			line := sorted[j]
			if b := best[line]; b == nil || (s.EndLine-s.StartLine) < (b.EndLine-b.StartLine) {
				best[line] = s
			}
		}
	}

	seen := map[*symbols.Symbol]bool{}
	var out []symbols.Symbol
	for _, line := range sorted {
		s := best[line]
		if s != nil && !seen[s] {
			seen[s] = true
			out = append(out, *s)
		}
	}
	return out
}

// changedSeed retains blob-side definition identity, not just its name. An old
// seed is not a proven live definition when parsing the new side failed.
type changedSeed struct {
	Symbol  symbols.Symbol
	Deleted bool
	Side    string // "old" or "new"; Symbol.File belongs to this side
	// NewSymbol is the unique surviving definition in the new blob. It keeps
	// deletion-only edits usable when their old line range no longer exists.
	NewSymbol         *symbols.Symbol
	IdentityUncertain bool // same-name multiplicity changed between blobs
}

// changedInputFile includes metadata-only inputs, even without patch headers.
// Paths are repository-relative and empty for a side that does not exist.
type changedInputFile struct {
	OldPath, NewPath string
	Status           string // Git name-status code, including rename similarity
	Binary           bool
	Conflicted       bool
	HasChangedLines  bool // changed text lines; binary and conflict flags are separate
}

// changedLimitation records attribution gaps independently of any index API.
// Side is empty for file-level limitations; Path is always repository-relative.
type changedLimitation struct {
	Code, Message, Path, Side string
}

type changedAnalysis struct {
	Base            string
	Files           []changedInputFile
	Seeds           []changedSeed
	Limitations     []changedLimitation
	SkippedFiles    int // legacy changed counter, not a count of limitations
	ConflictedFiles int // legacy count of combined patch blocks
}

// readChangedAnalysis reads the actual diffed blobs, without consulting the
// index. root is the repository root; staged/base have changedDiffArgs semantics.
// Git failures are operational errors, not successful empty analyses.
func readChangedAnalysis(root string, staged bool, base string) (*changedAnalysis, error) {
	args, label, err := changedDiffArgs(staged, base)
	if err != nil {
		return nil, err
	}
	patch, err := changedGitOutput(root, args)
	if err != nil {
		return nil, err
	}
	names, err := changedGitOutput(root, args, "--name-status", "-z")
	if err != nil {
		return nil, err
	}
	files, err := changedNameStatus(names)
	if err != nil {
		return nil, err
	}
	// Numstat supplies binary flags with unambiguous NUL-delimited paths. Patch
	// binary headers have two space-separated paths and cannot safely supply them.
	numstat, err := changedGitOutput(root, args, "--numstat", "-z")
	if err != nil {
		return nil, err
	}
	binary, err := changedBinaryPaths(numstat)
	if err != nil {
		return nil, err
	}
	patches := parseChangedFiles(string(patch))
	contentPaths := map[string]bool{}
	for _, p := range patches {
		if len(p.OldLines) > 0 || len(p.NewLines) > 0 {
			if p.OldPath != "" {
				contentPaths[p.OldPath] = true
			}
			if p.NewPath != "" {
				contentPaths[p.NewPath] = true
			}
		}
	}
	result := &changedAnalysis{
		Base: label, Files: files, Seeds: []changedSeed{}, Limitations: []changedLimitation{},
		ConflictedFiles: countConflictBlocks(string(patch)),
	}
	oldRev := ""
	if staged {
		oldRev = "HEAD"
	} else if base != "" {
		oldRev = base
	}
	for i := range result.Files {
		f := &result.Files[i]
		f.Binary = binary[f.OldPath] || binary[f.NewPath]
		f.HasChangedLines = contentPaths[f.OldPath] || contentPaths[f.NewPath]
		path := f.NewPath
		if path == "" {
			path = f.OldPath
		}
		if f.OldPath != "" && f.NewPath != "" && f.OldPath != f.NewPath {
			result.limit("renamed", "file renamed from "+f.OldPath, path, "")
		}
		if f.Conflicted {
			result.limit("conflicted", "unmerged file cannot be attributed", path, "")
			continue
		}
		if f.Binary {
			result.limit("binary", "binary diff cannot be attributed", path, "")
			continue
		}
		if !f.HasChangedLines {
			result.limit("metadata_only", "diff has no changed content lines", path, "")
		}
	}
	// Attribute patch blocks rather than assuming one block per inventory entry.
	// A type change can emit separate deletion and addition blocks for one path.
	// This also preserves the legacy per-block skip and deletion rules.
	for _, p := range patches {
		if p.Binary {
			result.SkippedFiles++
			continue
		}
		if p.OldPath == "" && p.NewPath == "" {
			continue
		}
		result.attributePatch(root, staged, oldRev, p)
	}

	return result, nil
}

func (a *changedAnalysis) attributePatch(root string, staged bool, oldRev string, p changedFile) {
	fileParsed, newParsed := false, false
	newNames := map[string]bool{}
	type definitionKey struct{ name, kind, parent string }
	key := func(s symbols.Symbol) definitionKey { return definitionKey{s.Name, s.Kind, s.Parent} }
	newDefinitions := map[definitionKey][]symbols.Symbol{}
	if p.NewPath != "" {
		if syms, ok := a.readSide(root, staged, oldRev, p.NewPath, "new"); ok {
			fileParsed, newParsed = true, true
			for _, s := range syms {
				newDefinitions[key(s)] = append(newDefinitions[key(s)], s)
				newNames[s.Name] = true
			}
			for _, s := range innermostChangedDefinitions(syms, p.NewLines) {
				a.Seeds = append(a.Seeds, changedSeed{Symbol: s, Side: "new"})
			}
			a.outsideSymbols(syms, p.NewLines, p.NewPath, "new")
		}
	}
	if p.OldPath != "" && len(p.OldLines) > 0 {
		if syms, ok := a.readSide(root, staged, oldRev, p.OldPath, "old"); ok {
			fileParsed = true
			oldCounts := map[definitionKey]int{}
			for _, s := range syms {
				oldCounts[key(s)]++
			}
			for _, s := range innermostChangedDefinitions(syms, p.OldLines) {
				gone := p.NewPath == "" || (newParsed && !newNames[s.Name])
				seed := changedSeed{Symbol: s, Deleted: gone, Side: "old"}
				current := newDefinitions[key(s)]
				seed.IdentityUncertain = newParsed && len(current) > 0 && len(current) != oldCounts[key(s)]
				if !seed.IdentityUncertain {
					if len(current) == 1 {
						seed.NewSymbol = &current[0]
					} else {
						// Equal multiplicity: retain a unique same-location
						// definition when the extractor omits its parent.
						for _, candidate := range current {
							if candidate.StartLine == s.StartLine {
								if seed.NewSymbol != nil {
									seed.NewSymbol = nil
									break
								}
								seed.NewSymbol = &candidate
							}
						}
					}
				}
				a.Seeds = append(a.Seeds, seed)
			}
			a.outsideSymbols(syms, p.OldLines, p.OldPath, "old")
		}
	}
	if !fileParsed {
		a.SkippedFiles++
	}
}

func changedGitOutput(root string, args []string, format ...string) ([]byte, error) {
	// Insert output flags immediately after diff, before any ref/path separator.
	full := []string{"-C", root}
	for _, arg := range args {
		full = append(full, arg)
		if arg == "diff" && len(format) > 0 {
			full = append(full, format...)
			format = nil
		}
	}
	out, err := exec.Command("git", full...).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return nil, fmt.Errorf("git diff: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("git diff: %w", err)
	}
	return out, nil
}

// changedNameStatus decodes --name-status -z, including two-path rename records.
// Unmerged paths can also have an M record; retain one conflicted input for both.
func changedNameStatus(data []byte) ([]changedInputFile, error) {
	parts := strings.Split(string(data), "\x00")
	files := []changedInputFile{}
	byPath := map[string]int{}
	for i := 0; i < len(parts)-1; {
		status := parts[i]
		i++
		if status == "" || i >= len(parts)-1 {
			return nil, fmt.Errorf("invalid git name-status record")
		}
		oldPath, newPath := parts[i], parts[i]
		i++
		switch status[0] {
		case 'R', 'C':
			if i >= len(parts)-1 {
				return nil, fmt.Errorf("invalid git rename record")
			}
			newPath = parts[i]
			i++
		case 'A':
			oldPath = ""
		case 'D':
			newPath = ""
		}
		f := changedInputFile{OldPath: oldPath, NewPath: newPath, Status: status, Conflicted: status[0] == 'U'}
		if j, exists := byPath[newPath]; exists && newPath != "" {
			if f.Conflicted {
				files[j] = f
			}
			continue
		}
		byPath[newPath] = len(files)
		files = append(files, f)
	}
	return files, nil
}

// changedBinaryPaths decodes --numstat -z. A rename uses an empty path field
// followed by the old and new paths, each terminated by NUL.
func changedBinaryPaths(data []byte) (map[string]bool, error) {
	parts := strings.Split(string(data), "\x00")
	paths := map[string]bool{}
	for i := 0; i < len(parts)-1; {
		fields := strings.SplitN(parts[i], "\t", 3)
		i++
		if len(fields) != 3 {
			return nil, fmt.Errorf("invalid git numstat record")
		}
		names := []string{fields[2]}
		if fields[2] == "" {
			if i+1 >= len(parts)-1 {
				return nil, fmt.Errorf("invalid git numstat rename record")
			}
			names = parts[i : i+2]
			i += 2
		}
		if fields[0] == "-" && fields[1] == "-" {
			for _, path := range names {
				paths[path] = true
			}
		}
	}
	return paths, nil
}

func (a *changedAnalysis) limit(code, message, path, side string) {
	a.Limitations = append(a.Limitations, changedLimitation{Code: code, Message: message, Path: path, Side: side})
}

func (a *changedAnalysis) readSide(root string, staged bool, oldRev, path, side string) ([]symbols.Symbol, bool) {
	var blob []byte
	var err error
	if side == "new" {
		blob, err = readNewSide(root, staged, path)
	} else {
		blob, err = readOldSide(root, oldRev, path)
	}
	if err != nil {
		a.limit("unreadable", err.Error(), path, side)
		return nil, false
	}
	syms, code, message := parseBlobSymbolsDetailed(blob, path)
	if code != "" {
		a.limit(code, message, path, side)
		return nil, false
	}
	return syms, true
}

func (a *changedAnalysis) outsideSymbols(syms []symbols.Symbol, lines map[int]bool, path, side string) {
	// Every changed line must be inside a navigable definition, not merely one
	// line in the hunk. A parsed package/import/comment edit still has a gap.
	// Sorted starts and prefix maximum ends give coverage in O(log symbols)
	// per line, even when a rewrite touches thousands of definitions.
	units := make([]symbols.Symbol, 0, len(syms))
	for _, sym := range syms {
		if symbols.IsNavigableDefinition(sym.Kind, sym.Depth) {
			units = append(units, sym)
		}
	}
	sort.Slice(units, func(i, j int) bool { return units[i].StartLine < units[j].StartLine })
	ends := make([]int, len(units))
	for i, sym := range units {
		ends[i] = sym.EndLine
		if i > 0 && ends[i-1] > ends[i] {
			ends[i] = ends[i-1]
		}
	}
	uncovered := 0
	for line := range lines {
		i := sort.Search(len(units), func(i int) bool { return units[i].StartLine > line })
		if i == 0 || ends[i-1] < line {
			uncovered++
		}
	}
	if uncovered > 0 {
		a.limit("outside_symbols", fmt.Sprintf("%d changed line(s) outside navigable definitions", uncovered), path, side)
	}
}

// legacyProjection intentionally collapses identities by name. Old-side live
// modifications use the new path, including renames, as changed always has.
func (a *changedAnalysis) legacyProjection() (changed map[string]map[string]struct{}, changedOrder []string, deleted map[string]map[string]struct{}, deletedOrder []string) {
	changed, deleted = map[string]map[string]struct{}{}, map[string]map[string]struct{}{}
	newPaths := map[string]string{}
	for _, f := range a.Files {
		if f.NewPath != "" {
			newPaths[f.OldPath] = f.NewPath
		}
	}
	for _, seed := range a.Seeds {
		m, path := changed, seed.Symbol.File
		if seed.Deleted {
			m = deleted
		} else if seed.Side == "old" && newPaths[path] != "" {
			path = newPaths[path]
		}
		if m[seed.Symbol.Name] == nil {
			m[seed.Symbol.Name] = map[string]struct{}{}
		}
		m[seed.Symbol.Name][path] = struct{}{}
	}
	for name := range changed {
		changedOrder = append(changedOrder, name)
	}
	for name := range deleted {
		deletedOrder = append(deletedOrder, name)
	}
	sort.Strings(changedOrder)
	sort.Strings(deletedOrder)
	return
}
