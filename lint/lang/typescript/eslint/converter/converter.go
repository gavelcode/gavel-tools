// Package converter turns ESLint's built-in JSON report into SARIF 2.1.0.
//
// ESLint's npm SARIF formatter does not materialize in a Bazel sandbox (its
// package is not on the resolution path of the action), so the hermetic aspect
// runs `eslint --format json` — a built-in formatter needing no extra package —
// and converts the result here, the same shape gavel uses for Rust's Clippy.
package converter

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	severityError  = 2
	levelError     = "error"
	levelWarning   = "warning"
	driverName     = "eslint"
	fallbackRuleID = "eslint"
)

// Convert reads `eslint --format json` output and returns SARIF 2.1.0.
func Convert(input []byte) ([]byte, error) {
	var files []eslintFile
	if err := json.Unmarshal(input, &files); err != nil {
		return nil, fmt.Errorf("parse eslint json: %w", err)
	}
	return json.MarshalIndent(toSARIF(files), "", "  ")
}

type eslintFile struct {
	FilePath string          `json:"filePath"`
	Messages []eslintMessage `json:"messages"`
}

type eslintMessage struct {
	RuleID    string `json:"ruleId"`
	Severity  int    `json:"severity"`
	Message   string `json:"message"`
	Line      int    `json:"line"`
	Column    int    `json:"column"`
	EndLine   int    `json:"endLine"`
	EndColumn int    `json:"endColumn"`
}

type sarifReport struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name  string      `json:"name"`
	Rules []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID string `json:"id"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifact `json:"artifactLocation"`
	Region           sarifRegion   `json:"region"`
}

type sarifArtifact struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn"`
	EndLine     int `json:"endLine"`
	EndColumn   int `json:"endColumn"`
}

func toSARIF(files []eslintFile) sarifReport {
	ruleSet := make(map[string]bool)
	rules := []sarifRule{}
	results := []sarifResult{}

	for _, file := range files {
		for _, msg := range file.Messages {
			ruleID := msg.RuleID
			if ruleID == "" {
				ruleID = fallbackRuleID
			}
			if !ruleSet[ruleID] {
				ruleSet[ruleID] = true
				rules = append(rules, sarifRule{ID: ruleID})
			}
			results = append(results, sarifResult{
				RuleID:  ruleID,
				Level:   level(msg.Severity),
				Message: sarifMessage{Text: msg.Message},
				Locations: []sarifLocation{{
					PhysicalLocation: sarifPhysicalLocation{
						ArtifactLocation: sarifArtifact{URI: toURI(file.FilePath)},
						Region: sarifRegion{
							StartLine:   msg.Line,
							StartColumn: msg.Column,
							EndLine:     msg.EndLine,
							EndColumn:   msg.EndColumn,
						},
					},
				}},
			})
		}
	}

	return sarifReport{
		Version: "2.1.0",
		Schema:  "https://schemastore.azurewebsites.net/schemas/json/sarif-2.1.0.json",
		Runs: []sarifRun{{
			Tool:    sarifTool{Driver: sarifDriver{Name: driverName, Rules: rules}},
			Results: results,
		}},
	}
}

func level(severity int) string {
	if severity == severityError {
		return levelError
	}
	return levelWarning
}

func toURI(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}
