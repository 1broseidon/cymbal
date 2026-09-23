package index

import "github.com/1broseidon/cymbal/internal/pathmatch"

// PathFilter selects repo-relative paths using the CLI's substring and glob rules.
// Includes are combined with OR; any matching exclude rejects the path.
type PathFilter struct {
	Include []string
	Exclude []string
}

func (p PathFilter) active() bool {
	return len(p.Include) > 0 || len(p.Exclude) > 0
}

// Matches reports whether a repo-relative path passes the filter.
func (p PathFilter) Matches(path string) bool {
	path = pathmatch.Normalize(path)
	return (len(p.Include) == 0 || pathmatch.MatchAny(path, p.Include)) &&
		!pathmatch.MatchAny(path, p.Exclude)
}

// TextFiles returns eligible indexed files in repo-relative path order.
// Both text-search backends use this inventory, including persisted index exclusions.
func TextFiles(dbPath, language string, paths PathFilter) ([]FileInfo, error) {
	store, err := openCached(dbPath)
	if err != nil {
		return nil, err
	}
	files, err := store.AllFiles(language)
	if err != nil {
		return nil, err
	}
	filtered := files[:0]
	for _, file := range files {
		if paths.Matches(file.RelPath) {
			filtered = append(filtered, file)
		}
	}
	return filtered, nil
}
