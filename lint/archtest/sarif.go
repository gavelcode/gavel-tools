package archtest

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gavelcode/gavel-tools/lint/sarif"
)

const (
	sarifVersion   = "2.1.0"
	sarifSchema    = "https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/sarif-2.1/schema/sarif-schema-2.1.0.json"
	sarifLevel     = "error"
	dirPermission  = 0o755
	filePermission = 0o644
)

// WriteSARIFWithInvocation writes the report carrying an explicit invocation, so
// a wrapper that could only analyze its inputs partially can record
// executionSuccessful=false and why, instead of failing silently.
func WriteSARIFWithInvocation(reportPath, toolName string, violations []Violation, invocation sarif.Invocation) error {
	rules := collectRuleDescriptors(violations)
	results := buildResults(violations)

	document := sarifLog{
		Schema:  sarifSchema,
		Version: sarifVersion,
		Runs: []sarifRun{
			{
				Tool: sarifTool{
					Driver: sarifDriver{
						Name:  toolName,
						Rules: rules,
					},
				},
				Results:     results,
				Invocations: []sarif.Invocation{invocation},
			},
		},
	}

	sarifBytes, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal SARIF: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(reportPath), dirPermission); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	if err := os.WriteFile(reportPath, sarifBytes, filePermission); err != nil {
		return fmt.Errorf("write SARIF: %w", err)
	}

	return nil
}

func collectRuleDescriptors(violations []Violation) []sarifRuleDescriptor {
	seenRules := make(map[string]bool)
	var rules []sarifRuleDescriptor
	for _, violation := range violations {
		if seenRules[violation.RuleName] {
			continue
		}
		seenRules[violation.RuleName] = true
		rules = append(rules, sarifRuleDescriptor{
			ID:                   violation.RuleName,
			DefaultConfiguration: sarifConfiguration{Level: sarifLevel},
		})
	}
	return rules
}

func buildResults(violations []Violation) []sarifResult {
	results := make([]sarifResult, 0, len(violations))
	for _, violation := range violations {
		results = append(results, toSarifResult(violation))
	}
	return results
}

func toSarifResult(violation Violation) sarifResult {
	return sarifResult{
		RuleID:  violation.RuleName,
		Level:   sarifLevel,
		Message: sarifMessage{Text: violation.Message},
		Locations: []sarifLocation{
			{
				PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifactLocation{URI: violation.SourceFile},
					Region:           &sarifRegion{StartLine: violation.SourceLine},
				},
			},
		},
		Fingerprints: map[string]string{
			"gavel/v1": fingerprintFor(violation),
		},
		Properties: sarifProperties{
			SourcePkg: violation.SourcePkg,
			TargetPkg: violation.TargetPkg,
		},
	}
}

func fingerprintFor(v Violation) string {
	input := fmt.Sprintf("archtest:%s:%s:%s", v.RuleName, v.SourceFile, v.TargetPkg)
	hash := sha256.Sum256([]byte(input))
	return fmt.Sprintf("%x", hash[:16])
}

type sarifLog struct {
	Schema  string     `json:"$schema"`
	Version string     `json:"version"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool        sarifTool          `json:"tool"`
	Results     []sarifResult      `json:"results"`
	Invocations []sarif.Invocation `json:"invocations,omitempty"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name  string                `json:"name"`
	Rules []sarifRuleDescriptor `json:"rules"`
}

type sarifRuleDescriptor struct {
	ID                   string             `json:"id"`
	DefaultConfiguration sarifConfiguration `json:"defaultConfiguration"`
}

type sarifConfiguration struct {
	Level string `json:"level"`
}

type sarifResult struct {
	RuleID       string            `json:"ruleId"`
	Level        string            `json:"level"`
	Message      sarifMessage      `json:"message"`
	Locations    []sarifLocation   `json:"locations"`
	Fingerprints map[string]string `json:"fingerprints"`
	Properties   sarifProperties   `json:"properties"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine"`
}

type sarifProperties struct {
	SourcePkg string `json:"sourcePkg"`
	TargetPkg string `json:"targetPkg"`
}
