package archtest_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gavelcode/gavel-tools/tools/archtest"
)

func TestWriteSARIF(t *testing.T) {
	tests := []struct {
		name        string
		toolName    string
		violations  []archtest.Violation
		wantResults int
		wantRules   int
	}{
		{
			name:     "violationsProduceValidSARIF",
			toolName: "archtest",
			violations: []archtest.Violation{
				{
					RuleName:   "domain-imports-nothing",
					SourceFile: "internal/domain/order.go",
					SourceLine: 5,
					SourcePkg:  "domain",
					TargetPkg:  "application",
					Message:    "domain imports application (denied)",
				},
				{
					RuleName:   "domain-imports-nothing",
					SourceFile: "internal/domain/value.go",
					SourceLine: 3,
					SourcePkg:  "domain",
					TargetPkg:  "infrastructure",
					Message:    "domain imports infrastructure (denied)",
				},
			},
			wantResults: 2,
			wantRules:   1,
		},
		{
			name:        "emptyViolationsProduceValidSARIF",
			toolName:    "archtest",
			violations:  []archtest.Violation{},
			wantResults: 0,
			wantRules:   0,
		},
		{
			name:     "multipleRulesDeduplicatedInDescriptors",
			toolName: "archtest",
			violations: []archtest.Violation{
				{
					RuleName:   "rule-a",
					SourceFile: "a.go",
					SourceLine: 1,
					SourcePkg:  "pkg-a",
					TargetPkg:  "pkg-b",
					Message:    "msg-a",
				},
				{
					RuleName:   "rule-b",
					SourceFile: "b.go",
					SourceLine: 2,
					SourcePkg:  "pkg-c",
					TargetPkg:  "pkg-d",
					Message:    "msg-b",
				},
			},
			wantResults: 2,
			wantRules:   2,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			outPath := filepath.Join(t.TempDir(), "output.sarif")
			err := archtest.WriteSARIF(outPath, testCase.toolName, testCase.violations)
			require.NoError(t, err)

			data, err := os.ReadFile(outPath)
			require.NoError(t, err)

			doc := unmarshalSARIFDoc(t, data)
			assert.Equal(t, "2.1.0", doc.Version)
			assert.Contains(t, doc.Schema, "sarif-schema-2.1.0")
			require.Len(t, doc.Runs, 1)

			run := doc.Runs[0]
			assert.Equal(t, testCase.toolName, run.Tool.Driver.Name)
			assert.Len(t, run.Tool.Driver.Rules, testCase.wantRules)
			assert.Len(t, run.Results, testCase.wantResults)

			for _, result := range run.Results {
				assert.Equal(t, "error", result.Level)
				assert.NotEmpty(t, result.Fingerprints["gavel/v1"])
				require.Len(t, result.Locations, 1)
				assert.NotEmpty(t, result.Locations[0].PhysicalLocation.ArtifactLocation.URI)
			}
		})
	}
}

func TestWriteSARIFResultStructure(t *testing.T) {
	violation := archtest.Violation{
		RuleName:   "no-infra",
		SourceFile: "internal/domain/order.go",
		SourceLine: 42,
		SourcePkg:  "domain",
		TargetPkg:  "infrastructure",
		Message:    "domain imports infrastructure",
	}

	outPath := filepath.Join(t.TempDir(), "result.sarif")
	err := archtest.WriteSARIF(outPath, "archtest", []archtest.Violation{violation})
	require.NoError(t, err)

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)

	doc := unmarshalSARIFDoc(t, data)
	require.Len(t, doc.Runs[0].Results, 1)

	result := doc.Runs[0].Results[0]
	assert.Equal(t, "no-infra", result.RuleID)
	assert.Equal(t, "domain imports infrastructure", result.Message.Text)
	assert.Equal(t, "internal/domain/order.go", result.Locations[0].PhysicalLocation.ArtifactLocation.URI)
	assert.Equal(t, 42, result.Locations[0].PhysicalLocation.Region.StartLine)
	assert.Equal(t, "domain", result.Properties.SourcePkg)
	assert.Equal(t, "infrastructure", result.Properties.TargetPkg)
}

func TestWriteSARIFFingerprintIsDeterministic(t *testing.T) {
	violation := archtest.Violation{
		RuleName:   "rule-1",
		SourceFile: "file.go",
		SourceLine: 1,
		SourcePkg:  "pkg-a",
		TargetPkg:  "pkg-b",
		Message:    "msg",
	}

	path1 := filepath.Join(t.TempDir(), "first.sarif")
	path2 := filepath.Join(t.TempDir(), "second.sarif")

	require.NoError(t, archtest.WriteSARIF(path1, "archtest", []archtest.Violation{violation}))
	require.NoError(t, archtest.WriteSARIF(path2, "archtest", []archtest.Violation{violation}))

	data1, err := os.ReadFile(path1)
	require.NoError(t, err)
	data2, err := os.ReadFile(path2)
	require.NoError(t, err)

	doc1 := unmarshalSARIFDoc(t, data1)
	doc2 := unmarshalSARIFDoc(t, data2)

	fp1 := doc1.Runs[0].Results[0].Fingerprints["gavel/v1"]
	fp2 := doc2.Runs[0].Results[0].Fingerprints["gavel/v1"]
	assert.Equal(t, fp1, fp2)
	assert.Len(t, fp1, 32)
}

func TestWriteSARIFFingerprintDiffersForDifferentViolations(t *testing.T) {
	firstViolation := archtest.Violation{
		RuleName:   "rule-1",
		SourceFile: "a.go",
		SourceLine: 1,
		SourcePkg:  "pkg-a",
		TargetPkg:  "pkg-b",
		Message:    "msg1",
	}
	secondViolation := archtest.Violation{
		RuleName:   "rule-1",
		SourceFile: "a.go",
		SourceLine: 1,
		SourcePkg:  "pkg-a",
		TargetPkg:  "pkg-c",
		Message:    "msg2",
	}

	outPath := filepath.Join(t.TempDir(), "diff.sarif")
	require.NoError(t, archtest.WriteSARIF(outPath, "archtest", []archtest.Violation{firstViolation, secondViolation}))

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)

	doc := unmarshalSARIFDoc(t, data)
	fp1 := doc.Runs[0].Results[0].Fingerprints["gavel/v1"]
	fp2 := doc.Runs[0].Results[1].Fingerprints["gavel/v1"]
	assert.NotEqual(t, fp1, fp2)
}

func TestWriteSARIF_MkdirAllError(t *testing.T) {
	err := archtest.WriteSARIF("/dev/null/impossible/output.sarif", "archtest", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output directory")
}

func TestWriteSARIF_WriteFileError(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "out")
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	require.NoError(t, os.Chmod(outDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(outDir, 0o755) })

	err := archtest.WriteSARIF(filepath.Join(outDir, "result.sarif"), "archtest", nil)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write SARIF")
}

type testSARIFDoc struct {
	Schema  string         `json:"$schema"`
	Version string         `json:"version"`
	Runs    []testSARIFRun `json:"runs"`
}

type testSARIFRun struct {
	Tool    testSARIFTool     `json:"tool"`
	Results []testSARIFResult `json:"results"`
}

type testSARIFTool struct {
	Driver testSARIFDriver `json:"driver"`
}

type testSARIFDriver struct {
	Name  string                    `json:"name"`
	Rules []testSARIFRuleDescriptor `json:"rules"`
}

type testSARIFRuleDescriptor struct {
	ID                   string                 `json:"id"`
	DefaultConfiguration testSARIFConfiguration `json:"defaultConfiguration"`
}

type testSARIFConfiguration struct {
	Level string `json:"level"`
}

type testSARIFResult struct {
	RuleID       string              `json:"ruleId"`
	Level        string              `json:"level"`
	Message      testSARIFMessage    `json:"message"`
	Locations    []testSARIFLocation `json:"locations"`
	Fingerprints map[string]string   `json:"fingerprints"`
	Properties   testSARIFProperties `json:"properties"`
}

type testSARIFMessage struct {
	Text string `json:"text"`
}

type testSARIFLocation struct {
	PhysicalLocation testSARIFPhysicalLocation `json:"physicalLocation"`
}

type testSARIFPhysicalLocation struct {
	ArtifactLocation testSARIFArtifactLocation `json:"artifactLocation"`
	Region           testSARIFRegion           `json:"region"`
}

type testSARIFArtifactLocation struct {
	URI string `json:"uri"`
}

type testSARIFRegion struct {
	StartLine int `json:"startLine"`
}

type testSARIFProperties struct {
	SourcePkg string `json:"sourcePkg"`
	TargetPkg string `json:"targetPkg"`
}

func unmarshalSARIFDoc(t *testing.T, data []byte) testSARIFDoc {
	t.Helper()
	var doc testSARIFDoc
	require.NoError(t, json.Unmarshal(data, &doc))
	return doc
}
