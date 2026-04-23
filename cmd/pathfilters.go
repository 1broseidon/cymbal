package cmd

import (
	gopath "path"
	"path/filepath"
	"strings"
)

func normalizeRelPath(rel string) string {
	rel = filepath.ToSlash(rel)
	return strings.TrimPrefix(rel, "./")
}

func matchAnyPath(rel string, globs []string) bool {
	rel = normalizeRelPath(rel)
	for _, glob := range globs {
		glob = normalizeRelPath(strings.TrimSpace(glob))
		if glob == "" {
			continue
		}
		if !hasGlobMeta(glob) && strings.Contains(rel, glob) {
			return true
		}
		if matchPathGlob(glob, rel) {
			return true
		}
	}
	return false
}

func hasGlobMeta(glob string) bool {
	return strings.ContainsAny(glob, "*?[")
}

func matchPathGlob(glob, rel string) bool {
	return matchPathSegments(splitPathSegments(glob), splitPathSegments(rel))
}

func splitPathSegments(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func matchPathSegments(globSegs, relSegs []string) bool {
	if len(globSegs) == 0 {
		return len(relSegs) == 0
	}
	if globSegs[0] == "**" {
		for len(globSegs) > 1 && globSegs[1] == "**" {
			globSegs = globSegs[1:]
		}
		if len(globSegs) == 1 {
			return true
		}
		for i := 0; i <= len(relSegs); i++ {
			if matchPathSegments(globSegs[1:], relSegs[i:]) {
				return true
			}
		}
		return false
	}
	if len(relSegs) == 0 {
		return false
	}
	ok, err := gopath.Match(globSegs[0], relSegs[0])
	if err != nil || !ok {
		return false
	}
	return matchPathSegments(globSegs[1:], relSegs[1:])
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
