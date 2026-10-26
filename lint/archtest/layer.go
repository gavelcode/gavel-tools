package archtest

import "strings"

const (
	recursiveSuffix = "..."
	wildcardSegment = "**"
	pathSeparator   = "/"
)

// MatchLayer returns the layer whose pattern matches path, or "" when none does.
//
// When patterns of several layers match, the most specific one wins: the
// pattern with the most literal (non-wildcard) segments. Equal specificity is
// broken by the lexically smallest layer name, so the result never depends on
// map iteration order.
func MatchLayer(path string, layers map[string][]string) string {
	best, bestSpecificity := "", -1
	for name, patterns := range layers {
		specificity := bestMatchSpecificity(path, patterns)
		if specificity < 0 {
			continue
		}
		if specificity > bestSpecificity || (specificity == bestSpecificity && name < best) {
			best, bestSpecificity = name, specificity
		}
	}
	return best
}

// bestMatchSpecificity returns the highest specificity among the patterns that
// match path, or -1 when none matches.
func bestMatchSpecificity(path string, patterns []string) int {
	best := -1
	for _, pattern := range patterns {
		if matchPattern(path, pattern) {
			best = max(best, specificity(pattern))
		}
	}
	return best
}

// specificity counts the literal segments of a pattern.
func specificity(pattern string) int {
	count := 0
	for _, segment := range strings.Split(pattern, pathSeparator) {
		if segment != "" && segment != wildcardSegment && segment != recursiveSuffix {
			count++
		}
	}
	return count
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
