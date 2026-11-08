package index

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/1broseidon/cymbal/symbols"
)

// Definitions are never capped. Dependency queries overfetch the global budget
// by one row; the selector reports the first unadmitted record. Frontier probes
// are uncapped so a numeric depth boundary is not itself called truncation.
// Cached rows retain alternate predecessors without querying a node again.
type affectedQueries struct {
	store       *Store
	maxEvidence int
	ranges      map[string]enclosingRanges
	symbolKeys  map[string]AffectedTestSymbol
	files       map[string]string
	byFile      map[string][]AffectedTestSymbol
	byName      map[string][]AffectedTestSymbol
	refCache    map[string][]affectedReference
	importCache map[string][]affectedImport
}
type affectedReference struct {
	Path                 string
	Line                 int
	Name, Language, Kind string
}
type affectedImport struct{ Path, Raw string }

func newAffectedQueries(s *Store, maxEvidence int) (*affectedQueries, error) {
	q := &affectedQueries{store: s, maxEvidence: maxEvidence, ranges: map[string]enclosingRanges{}, symbolKeys: map[string]AffectedTestSymbol{}, files: map[string]string{}, byFile: map[string][]AffectedTestSymbol{}, byName: map[string][]AffectedTestSymbol{}, refCache: map[string][]affectedReference{}, importCache: map[string][]affectedImport{}}
	rows, err := s.db.Query(`SELECT rel_path, language FROM files ORDER BY rel_path`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p, l string
		if err := rows.Scan(&p, &l); err != nil {
			rows.Close()
			return nil, err
		}
		q.files[strings.ReplaceAll(p, "\\", "/")] = l
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	depths := map[string]int{}
	rows, err = s.db.Query(`SELECT f.rel_path,s.name,s.kind,s.language,s.start_line,s.end_line,s.depth FROM symbols s JOIN files f ON s.file_id=f.id ORDER BY f.rel_path,s.start_line,s.end_line,s.name,s.kind,s.language`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v AffectedTestSymbol
		var depth int
		if err := rows.Scan(&v.Path, &v.Name, &v.Kind, &v.Language, &v.StartLine, &v.EndLine, &depth); err != nil {
			return nil, err
		}
		v.Path = strings.ReplaceAll(v.Path, "\\", "/")
		depths[affectedKey(v)] = depth
		q.byFile[v.Path] = append(q.byFile[v.Path], v)
		q.byName[v.Name] = append(q.byName[v.Name], v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for p, ss := range q.byFile {
		ordered := append([]AffectedTestSymbol{}, ss...)
		sort.Slice(ordered, func(i, j int) bool {
			di, dj := ordered[i].EndLine-ordered[i].StartLine, ordered[j].EndLine-ordered[j].StartLine
			if di != dj {
				return di < dj
			}
			return affectedKey(ordered[i]) < affectedKey(ordered[j])
		})
		for _, s := range ordered {
			key := affectedKey(s)
			q.symbolKeys[key] = s
			if symbols.IsNavigableDefinition(s.Kind, depths[key]) {
				q.ranges[p] = append(q.ranges[p], enclosingRange{name: key, start: s.StartLine, end: s.EndLine})
			}
		}
	}
	return q, nil
}
func affectedDefinitionMatches(seed, s AffectedTestSymbol) bool {
	return s.Name == seed.Name && (seed.Kind == "" || seed.Kind == s.Kind) && (seed.Language == "" || seed.Language == s.Language) && (seed.StartLine == 0 || seed.StartLine == s.StartLine) && (seed.EndLine == 0 || seed.EndLine == s.EndLine)
}
func (q *affectedQueries) live(seed AffectedTestSymbol) bool {
	for _, s := range q.byFile[seed.Path] {
		if affectedDefinitionMatches(seed, s) {
			return true
		}
	}
	return false
}

// Complete omitted identity fields only when the live path has one matching
// definition. Never borrow another path's definition or rewrite old/deleted
// identities. This also deduplicates sparse and full inputs for the same seed.
func (q *affectedQueries) completeSeed(seed AffectedTestSymbol) AffectedTestSymbol {
	if seed.Deleted || seed.Name == "" {
		return seed
	}
	var match AffectedTestSymbol
	count := 0
	for _, s := range q.byFile[seed.Path] {
		if affectedDefinitionMatches(seed, s) {
			match = s
			count++
		}
	}
	if count == 1 {
		return match
	}
	return seed
}
func (q *affectedQueries) definitionCount(name string, langs []string) int {
	n := 0
	for _, s := range q.byName[name] {
		if inLangs(s.Language, langs) {
			n++
		}
	}
	return n
}
func (q *affectedQueries) enclosing(p string, line int) (AffectedTestSymbol, bool, bool) {
	// Line-only references cannot distinguish equally narrow definitions on
	// one line. Keep a canonical explanation, but report that uncertainty.
	key := q.ranges[p].at(line)
	s, ok := q.symbolKeys[key]
	ambiguous := false
	if ok {
		for _, r := range q.ranges[p] {
			if r.name != key && r.start <= line && line <= r.end && r.end-r.start == s.EndLine-s.StartLine {
				ambiguous = true
				break
			}
		}
	}
	return s, ok, ambiguous
}

// A depth probe must see past already visited nodes. Other reads need at most
// budget+1 rows, including records shared with earlier seeds. Scope is applied
// before the SQL limit so unrelated languages cannot conceal admitted rows.
func (q *affectedQueries) queryLimit(probe bool) int {
	if probe || q.maxEvidence == 0 || q.maxEvidence == int(^uint(0)>>1) {
		return -1
	}
	return q.maxEvidence + 1
}

func (q *affectedQueries) references(name string, langs []string, probe bool) ([]affectedReference, error) {
	key := name + affectedKey(langs) + fmt.Sprint(probe)
	if v, ok := q.refCache[key]; ok {
		return v, nil
	}
	query := `SELECT DISTINCT f.rel_path,r.line,r.name,r.language,r.kind FROM refs r JOIN files f ON r.file_id=f.id WHERE r.name=?`
	args := []any{name}
	if len(langs) > 0 {
		query += " AND r.language IN (" + strings.TrimSuffix(strings.Repeat("?,", len(langs)), ",") + ")"
		for _, l := range langs {
			args = append(args, l)
		}
	}
	query += ` ORDER BY f.rel_path,r.line,r.name,r.language,r.kind LIMIT ?`
	args = append(args, q.queryLimit(probe))
	rows, err := q.store.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []affectedReference{}
	for rows.Next() {
		var r affectedReference
		if err := rows.Scan(&r.Path, &r.Line, &r.Name, &r.Language, &r.Kind); err != nil {
			return nil, err
		}
		r.Path = strings.ReplaceAll(r.Path, "\\", "/")
		if inLangs(r.Language, langs) {
			out = append(out, r)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	q.refCache[key] = out
	return out, nil
}
func (q *affectedQueries) importers(target string, probe bool) ([]affectedImport, error) {
	cacheKey := target + fmt.Sprint(probe)
	if v, ok := q.importCache[cacheKey]; ok {
		return v, nil
	}
	stem := strings.TrimSuffix(path.Base(target), path.Ext(target))
	dir := path.Dir(target)
	if dir == "." {
		dir = ""
	}
	// instr/substr are literal. '%' and '_' in a path are never SQL wildcards.
	// Keep the legacy basename substring convention, plus the nonempty package
	// directory suffix needed for e.g. imports of example.org/project/pkg.
	rows, err := q.store.db.Query(`SELECT DISTINCT f.rel_path,i.raw_path FROM imports i JOIN files f ON i.file_id=f.id WHERE substr(i.raw_path,-length(?))=? OR (?<>'' AND instr(i.raw_path,?)>0) OR (?<>'' AND substr(i.raw_path,-length(?))=?) ORDER BY f.rel_path,i.raw_path LIMIT ?`, target, target, stem, stem, dir, dir, dir, q.queryLimit(probe))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []affectedImport{}
	for rows.Next() {
		var r affectedImport
		if err := rows.Scan(&r.Path, &r.Raw); err != nil {
			return nil, err
		}
		r.Path = strings.ReplaceAll(r.Path, "\\", "/")
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	q.importCache[cacheKey] = out
	return out, nil
}
