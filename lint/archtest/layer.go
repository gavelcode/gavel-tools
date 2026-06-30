package archtest

import "strings"

const (
	recursiveSuffix = "..."
	wildcardSegment = "**"
	pathSeparator   = "/"
)

func MatchLayer(path string, layers map[string][]string) string {
	for name, patterns := range layers {
		for _, pattern := range patterns {
			if matchPattern(path, pattern) {
				return name
			}
		}
	}
	return ""
}

func matchPattern(path, pattern string) bool {
	if strings.Contains(pattern, wildcardSegment) {
		return matchWildcard(path, pattern)
	}

	return matchSimple(path, pattern)
}

func matchSimple(importPath, pattern string) bool {
	if !strings.HasSuffix(pattern, recursiveSuffix) {
		return importPath == pattern
	}

	prefix := strings.TrimSuffix(pattern, recursiveSuffix)
	prefix = strings.TrimSuffix(prefix, pathSeparator)

	return importPath == prefix || strings.HasPrefix(importPath, prefix+pathSeparator)
}

func matchWildcard(path, pattern string) bool {
	patternParts := strings.Split(pattern, pathSeparator)
	pathParts := strings.Split(path, pathSeparator)

	return matchParts(pathParts, patternParts, 0, 0)
}

func matchParts(pathParts, patternParts []string, patternIdx, pathIdx int) bool {
	for patternIdx < len(patternParts) && pathIdx < len(pathParts) {
		patternSegment := patternParts[patternIdx]

		if patternSegment == wildcardSegment {
			return matchDoubleWildcard(pathParts, patternParts, patternIdx, pathIdx)
		}

		if patternSegment == recursiveSuffix {
			return true
		}

		if patternSegment != pathParts[pathIdx] {
			return false
		}

		patternIdx++
		pathIdx++
	}

	if patternIdx < len(patternParts) && patternParts[patternIdx] == recursiveSuffix {
		return true
	}

	return patternIdx == len(patternParts) && pathIdx == len(pathParts)
}

func matchDoubleWildcard(pathParts, patternParts []string, patternIdx, pathIdx int) bool {
	remaining := patternParts[patternIdx+1:]
	for start := pathIdx; start < len(pathParts); start++ {
		if matchParts(pathParts, remaining, 0, start) {
			return true
		}
	}
	return false
}
