package archtest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gavelcode/gavel-tools/lint/archtest"
)

func TestMatchLayer(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		layers map[string][]string
		want   string
	}{
		{
			name: "exactDirectoryMatchWithRecursiveSuffix",
			path: "internal/domain",
			layers: map[string][]string{
				"domain": {"internal/domain/..."},
			},
			want: "domain",
		},
		{
			name: "recursiveMatchForNestedFile",
			path: "internal/domain/order/order.go",
			layers: map[string][]string{
				"domain": {"internal/domain/..."},
			},
			want: "domain",
		},
		{
			name: "noMatchForUnrelatedPath",
			path: "internal/other/foo.go",
			layers: map[string][]string{
				"domain":      {"internal/domain/..."},
				"application": {"internal/application/..."},
			},
			want: "",
		},
		{
			name: "doubleWildcardMatchesMultipleSegments",
			path: "src/main/java/com/example/domain/Order.java",
			layers: map[string][]string{
				"domain": {"src/main/java/**/domain/..."},
			},
			want: "domain",
		},
		{
			name: "doubleWildcardWithDeeperNesting",
			path: "src/main/java/com/example/deep/domain/model/Entity.java",
			layers: map[string][]string{
				"domain": {"src/main/java/**/domain/..."},
			},
			want: "domain",
		},
		{
			name: "multipleLayersMatchReturnsOneOfThem",
			path: "internal/domain/model.go",
			layers: map[string][]string{
				"layerA": {"internal/domain/..."},
				"layerB": {"internal/other/..."},
			},
			want: "layerA",
		},
		{
			name: "prefixOverlapWithoutSeparatorDoesNotMatch",
			path: "internal/domain_extra/foo.go",
			layers: map[string][]string{
				"domain": {"internal/domain/..."},
			},
			want: "",
		},
		{
			name:   "emptyLayersReturnsEmpty",
			path:   "internal/domain/model.go",
			layers: map[string][]string{},
			want:   "",
		},
		{
			name: "exactPatternWithoutRecursiveSuffix",
			path: "internal/domain",
			layers: map[string][]string{
				"domain": {"internal/domain"},
			},
			want: "domain",
		},
		{
			name: "exactPatternDoesNotMatchSubpath",
			path: "internal/domain/sub",
			layers: map[string][]string{
				"domain": {"internal/domain"},
			},
			want: "",
		},
		{
			name: "doubleWildcardNoMatchReturnsFalse",
			path: "src/main/java/com/example/service/Order.java",
			layers: map[string][]string{
				"domain": {"src/main/java/**/domain/..."},
			},
			want: "",
		},
		{
			name: "wildcardPatternExactSegmentMatch",
			path: "src/domain",
			layers: map[string][]string{
				"domain": {"src/**/domain"},
			},
			want: "domain",
		},
		{
			name: "wildcardPatternExhaustsPathBeforePattern",
			path: "src",
			layers: map[string][]string{
				"domain": {"src/**/domain/model"},
			},
			want: "",
		},
		{
			name: "wildcardPatternWithTrailingRecursiveSuffix",
			path: "src/main/java/domain/deep/nested",
			layers: map[string][]string{
				"domain": {"src/**/domain/..."},
			},
			want: "domain",
		},
		{
			name: "patternExhaustedBeforePath",
			path: "internal/domain/sub/pkg",
			layers: map[string][]string{
				"domain": {"internal/domain"},
			},
			want: "",
		},
		{
			name: "wildcardThenRecursiveSuffixAfterPathExhausted",
			path: "src/main/domain",
			layers: map[string][]string{
				"domain": {"src/**/domain/..."},
			},
			want: "domain",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := archtest.MatchLayer(tt.path, tt.layers)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMatchLayer_OverlappingPatternsPickMostSpecificDeterministically(t *testing.T) {
	layers := map[string][]string{
		"users":        {"**/com/acme/users/..."},
		"users-domain": {"**/com/acme/users/domain/..."},
	}

	for i := 0; i < 200; i++ {
		require.Equal(t, "users-domain", archtest.MatchLayer("core/com/acme/users/domain/model", layers), "call %d", i)
		require.Equal(t, "users", archtest.MatchLayer("core/com/acme/users/infra", layers), "call %d", i)
	}
}

func TestMatchLayer_EqualSpecificityPicksLexicallySmallestLayer(t *testing.T) {
	layers := map[string][]string{
		"zeta":  {"com/acme/..."},
		"alpha": {"com/acme/..."},
		"mid":   {"com/acme/..."},
	}

	for i := 0; i < 200; i++ {
		require.Equal(t, "alpha", archtest.MatchLayer("com/acme/x", layers), "call %d", i)
	}
}

func TestMatchLayer_LongerLiteralPatternBeatsWildcardPrefix(t *testing.T) {
	layers := map[string][]string{
		"wide":   {"**/domain/..."},
		"narrow": {"com/acme/users/domain/..."},
	}

	for i := 0; i < 200; i++ {
		require.Equal(t, "narrow", archtest.MatchLayer("com/acme/users/domain/model", layers), "call %d", i)
	}
}
