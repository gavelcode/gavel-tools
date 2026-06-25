package archtest_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gavelcode/gavel-tools/lint/archtest"
)

func TestEvaluate(t *testing.T) {
	layers := map[string][]string{
		"domain":         {"internal/domain/..."},
		"application":    {"internal/application/..."},
		"infrastructure": {"internal/infrastructure/..."},
		"userinterface":     {"internal/userinterface/..."},
	}

	rules := []archtest.Rule{
		{
			Name:   "domain-imports-nothing",
			Source: "domain",
			Deny:   []string{"application", "infrastructure", "userinterface"},
		},
		{
			Name:   "application-no-infra",
			Source: "application",
			Deny:   []string{"infrastructure", "userinterface"},
		},
	}

	tests := []struct {
		name           string
		sourceFile     string
		sourceLayer    string
		imports        []archtest.Import
		wantViolations int
		wantRuleName   string
		wantTargetPkg  string
	}{
		{
			name:        "deniedImportCreatesViolation",
			sourceFile:  "internal/domain/order.go",
			sourceLayer: "domain",
			imports: []archtest.Import{
				{Path: "internal/application/handler.go", Line: 5},
			},
			wantViolations: 1,
			wantRuleName:   "domain-imports-nothing",
			wantTargetPkg:  "application",
		},
		{
			name:        "allowedImportCreatesNoViolation",
			sourceFile:  "internal/application/handler.go",
			sourceLayer: "application",
			imports: []archtest.Import{
				{Path: "internal/domain/order.go", Line: 3},
			},
			wantViolations: 0,
		},
		{
			name:        "importOutsideAllLayersIsSkipped",
			sourceFile:  "internal/domain/order.go",
			sourceLayer: "domain",
			imports: []archtest.Import{
				{Path: "fmt", Line: 3},
				{Path: "github.com/external/lib", Line: 4},
			},
			wantViolations: 0,
		},
		{
			name:        "selfLayerImportCreatesNoViolation",
			sourceFile:  "internal/domain/order.go",
			sourceLayer: "domain",
			imports: []archtest.Import{
				{Path: "internal/domain/value.go", Line: 5},
			},
			wantViolations: 0,
		},
		{
			name:        "multipleViolationsFromMultipleImports",
			sourceFile:  "internal/domain/order.go",
			sourceLayer: "domain",
			imports: []archtest.Import{
				{Path: "internal/application/handler.go", Line: 5},
				{Path: "internal/infrastructure/repo.go", Line: 6},
				{Path: "internal/userinterface/http.go", Line: 7},
			},
			wantViolations: 3,
		},
		{
			name:        "noRulesForSourceLayerProducesNoViolations",
			sourceFile:  "internal/userinterface/handler.go",
			sourceLayer: "userinterface",
			imports: []archtest.Import{
				{Path: "internal/domain/order.go", Line: 3},
			},
			wantViolations: 0,
		},
		{
			name:           "emptyImportsProducesNoViolations",
			sourceFile:     "internal/domain/order.go",
			sourceLayer:    "domain",
			imports:        []archtest.Import{},
			wantViolations: 0,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			violations := archtest.Evaluate(testCase.sourceFile, testCase.sourceLayer, testCase.imports, layers, rules)

			require.Len(t, violations, testCase.wantViolations)

			if testCase.wantViolations > 0 && testCase.wantRuleName != "" {
				assert.Equal(t, testCase.wantRuleName, violations[0].RuleName)
			}
			if testCase.wantViolations > 0 && testCase.wantTargetPkg != "" {
				assert.Equal(t, testCase.wantTargetPkg, violations[0].TargetPkg)
			}
		})
	}
}

func TestEvaluateStripsModulePrefix(t *testing.T) {
	layers := map[string][]string{
		"domain":      {"core/domain/..."},
		"userinterface": {"core/userinterface/..."},
	}
	rules := []archtest.Rule{
		{Name: "ui-no-domain", Source: "userinterface", Deny: []string{"domain"}},
	}
	imports := []archtest.Import{
		{Path: "github.com/usegavel/gavel/core/domain/project/model", Line: 5},
	}

	violations := archtest.EvaluateWithModule(
		"core/userinterface/api/v1/server/casefile/handler.go",
		"userinterface",
		imports,
		layers,
		rules,
		"github.com/usegavel/gavel",
	)

	require.Len(t, violations, 1)
	assert.Equal(t, "ui-no-domain", violations[0].RuleName)
	assert.Equal(t, "domain", violations[0].TargetPkg)
}

func TestEvaluateWithModuleIgnoresExternalImports(t *testing.T) {
	layers := map[string][]string{
		"domain": {"core/domain/..."},
	}
	rules := []archtest.Rule{
		{Name: "domain-nothing", Source: "domain", Deny: []string{}},
	}
	imports := []archtest.Import{
		{Path: "github.com/google/uuid", Line: 3},
		{Path: "fmt", Line: 1},
	}

	violations := archtest.EvaluateWithModule(
		"core/domain/model.go",
		"domain",
		imports,
		layers,
		rules,
		"github.com/usegavel/gavel",
	)

	assert.Empty(t, violations)
}

func TestEvaluateViolationFields(t *testing.T) {
	layers := map[string][]string{
		"domain":      {"internal/domain/..."},
		"application": {"internal/application/..."},
	}

	rules := []archtest.Rule{
		{Name: "no-app", Source: "domain", Deny: []string{"application"}},
	}

	imports := []archtest.Import{
		{Path: "internal/application/service.go", Line: 10},
	}

	violations := archtest.Evaluate("internal/domain/model.go", "domain", imports, layers, rules)

	require.Len(t, violations, 1)
	violation := violations[0]
	assert.Equal(t, "no-app", violation.RuleName)
	assert.Equal(t, "internal/domain/model.go", violation.SourceFile)
	assert.Equal(t, 10, violation.SourceLine)
	assert.Equal(t, "domain", violation.SourcePkg)
	assert.Equal(t, "application", violation.TargetPkg)
	assert.Contains(t, violation.Message, "denied")
}
