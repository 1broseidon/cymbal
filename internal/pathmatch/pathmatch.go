package pathmatch

import (
	gopath "path"
	"path/filepath"
	"strings"
)

// Normalize converts a path to slash-separated repo-relative form.
func Normalize(rel string) string {
	rel = filepath.ToSlash(rel)
	return strings.TrimPrefix(rel, "./")
}

// MatchAny reports whether rel matches any path pattern.
//
// Patterns with no glob metacharacters keep the existing substring semantics
// used by CLI path filters. Glob patterns support ** as a recursive segment.
func MatchAny(rel string, patterns []string) bool {
	rel = Normalize(rel)
	for _, pattern := range patterns {
		pattern = Normalize(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if !hasGlobMeta(pattern) && strings.Contains(rel, pattern) {
			return true
		}
		if MatchGlob(pattern, rel) {
			return true
		}
	}
	return false
}

func hasGlobMeta(pattern string) bool {
	return strings.ContainsAny(pattern, "*?[")
}

// MatchGlob reports whether rel matches glob. Both paths should be slash
// separated; callers may pass unnormalized paths, which are normalized here.
func MatchGlob(glob, rel string) bool {
	return matchSegments(splitSegments(Normalize(glob)), splitSegments(Normalize(rel)))
}

func splitSegments(path string) []string {
	path = strings.Trim(path, "/")
	if path == "" {
		return nil
	}
	return strings.Split(path, "/")
}

func matchSegments(globSegs, relSegs []string) bool {
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
			if matchSegments(globSegs[1:], relSegs[i:]) {
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
	return matchSegments(globSegs[1:], relSegs[1:])
}
