package archtest_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gavelcode/gavel-tools/lint/archtest"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name       string
		yaml       string
		wantLayers map[string][]string
		wantRules  []archtest.Rule
		wantCycles bool
		wantErr    bool
		wantErrIs  error
	}{
		{
			name: "validV2WithLayersRulesAndDetectCycles",
			yaml: `
layers:
  domain: ["internal/domain/..."]
  application: ["internal/application/..."]
  infrastructure: ["internal/infrastructure/..."]
  userinterface: ["internal/userinterface/..."]
rules:
  - name: domain-imports-nothing
    source: domain
    deny: [application, infrastructure, userinterface]
detect_cycles: true
`,
			wantLayers: map[string][]string{
				"domain":         {"internal/domain/..."},
				"application":    {"internal/application/..."},
				"infrastructure": {"internal/infrastructure/..."},
				"userinterface":  {"internal/userinterface/..."},
			},
			wantRules: []archtest.Rule{
				{Name: "domain-imports-nothing", Source: "domain", Deny: []string{"application", "infrastructure", "userinterface"}},
			},
			wantCycles: true,
		},
		{
			name: "validV1WithVersionModuleAndGeneric",
			yaml: `
version: 1
module: "github.com/example/app"
layers:
  domain: ["internal/domain/..."]
  application: ["internal/application/..."]
rules:
  - name: domain-no-app
    source: domain
    deny: [application]
generic:
  no_circular_deps: true
`,
			wantLayers: map[string][]string{
				"domain":      {"internal/domain/..."},
				"application": {"internal/application/..."},
			},
			wantRules: []archtest.Rule{
				{Name: "domain-no-app", Source: "domain", Deny: []string{"application"}},
			},
			wantCycles: true,
		},
		{
			name:    "missingFileReturnsError",
			yaml:    "",
			wantErr: true,
		},
		{
			name: "invalidYAMLReturnsError",
			yaml: `
layers:
  domain: [
`,
			wantErr: true,
		},
		{
			name: "emptyLayersReturnsError",
			yaml: `
layers: {}
rules: []
`,
			wantErr:   true,
			wantErrIs: archtest.ErrInvalidConfig,
		},
		{
			name: "v2WithoutDetectCyclesDefaultsFalse",
			yaml: `
layers:
  domain: ["internal/domain/..."]
rules: []
`,
			wantLayers: map[string][]string{
				"domain": {"internal/domain/..."},
			},
			wantRules:  []archtest.Rule{},
			wantCycles: false,
		},
		{
			name: "ruleReferencingUndefinedSourceLayerReturnsError",
			yaml: `
layers:
  domain: ["internal/domain/..."]
rules:
  - name: bad-rule
    source: nonexistent
    deny: [domain]
`,
			wantErr:   true,
			wantErrIs: archtest.ErrInvalidConfig,
		},
		{
			name: "ruleReferencingUndefinedDenyLayerReturnsError",
			yaml: `
layers:
  domain: ["internal/domain/..."]
rules:
  - name: bad-rule
    source: domain
    deny: [nonexistent]
`,
			wantErr:   true,
			wantErrIs: archtest.ErrInvalidConfig,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.name == "missingFileReturnsError" {
				_, err := archtest.LoadConfig(filepath.Join(t.TempDir(), "nonexistent.yml"))
				require.Error(t, err)
				return
			}

			path := writeTestFile(t, testCase.yaml)
			cfg, err := archtest.LoadConfig(path)

			if testCase.wantErr {
				require.Error(t, err)
				if testCase.wantErrIs != nil {
					assert.ErrorIs(t, err, testCase.wantErrIs)
				}
				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.wantLayers, cfg.Layers)
			assert.Equal(t, testCase.wantCycles, cfg.DetectCycles)
			require.Len(t, cfg.Rules, len(testCase.wantRules))
			for i, want := range testCase.wantRules {
				assert.Equal(t, want.Name, cfg.Rules[i].Name)
				assert.Equal(t, want.Source, cfg.Rules[i].Source)
				assert.Equal(t, want.Deny, cfg.Rules[i].Deny)
			}
		})
	}
}

func TestLoadConfig_V1WithInvalidRuleReturnsError(t *testing.T) {
	path := writeTestFile(t, `
version: 1
module: "github.com/example/app"
layers:
  domain: ["internal/domain/..."]
rules:
  - name: bad-v1-rule
    source: nonexistent
    deny: [domain]
`)

	_, err := archtest.LoadConfig(path)

	require.Error(t, err)
	assert.ErrorIs(t, err, archtest.ErrInvalidConfig)
}

func writeTestFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "architecture.yml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}
