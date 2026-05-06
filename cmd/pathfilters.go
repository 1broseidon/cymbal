package cmd

import "github.com/1broseidon/cymbal/internal/pathmatch"

func normalizeRelPath(rel string) string {
	return pathmatch.Normalize(rel)
}

func matchAnyPath(rel string, globs []string) bool {
	return pathmatch.MatchAny(rel, globs)
}

func widenPathFilterLimit(limit int, hasFilters bool) int {
	if !hasFilters {
		return limit
	}
	if limit <= 0 {
		return 500
	}
	w := limit * 5
	if w < 100 {
		w = 100
	}
	if w > 1000 {
		w = 1000
	}
	return w
}

func allowPath(rel string, includes, excludes []string) bool {
	rel = normalizeRelPath(rel)
	if len(includes) > 0 && !matchAnyPath(rel, includes) {
		return false
	}
	if len(excludes) > 0 && matchAnyPath(rel, excludes) {
		return false
	}
	return true
}

func filterByPath[T any](items []T, relPath func(T) string, includes, excludes []string) []T {
	if len(includes) == 0 && len(excludes) == 0 {
		return items
	}
	out := make([]T, 0, len(items))
	for _, item := range items {
		if !allowPath(relPath(item), includes, excludes) {
			continue
		}
		out = append(out, item)
	}
	return out
}
