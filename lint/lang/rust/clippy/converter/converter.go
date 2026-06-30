package converter

import (
	"bufio"
	"bytes"
	"encoding/json"
	"strings"
)

func Convert(input []byte) ([]byte, error) {
	diagnostics := parseDiagnostics(input)
	sarif := toSARIF(diagnostics)
	return json.MarshalIndent(sarif, "", "  ")
}

type diagnostic struct {
	RuleID    string
	Level     string
	Message   string
	FilePath  string
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
}

type rustcMessage struct {
	MessageType string      `json:"$message_type"`
	Message     string      `json:"message"`
	Code        *rustcCode  `json:"code"`
	Level       string      `json:"level"`
	Spans       []rustcSpan `json:"spans"`
}

type rustcCode struct {
	Code string `json:"code"`
}

type rustcSpan struct {
	FileName    string `json:"file_name"`
	LineStart   int    `json:"line_start"`
	LineEnd     int    `json:"line_end"`
	ColumnStart int    `json:"column_start"`
	ColumnEnd   int    `json:"column_end"`
	IsPrimary   bool   `json:"is_primary"`
}

func parseDiagnostics(data []byte) []diagnostic {
	var result []diagnostic
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		inputLine := bytes.TrimSpace(scanner.Bytes())
		if len(inputLine) == 0 {
			continue
		}

		var parsedMessage rustcMessage
		if err := json.Unmarshal(inputLine, &parsedMessage); err != nil {
			continue
		}
		if parsedMessage.MessageType != "diagnostic" {
			continue
		}
		if parsedMessage.Code == nil || parsedMessage.Code.Code == "" {
			continue
		}
		if parsedMessage.Level != "warning" && parsedMessage.Level != "error" {
			continue
		}

		sourceSpan := primarySpan(parsedMessage.Spans)
		if sourceSpan == nil {
			continue
		}

		result = append(result, diagnostic{
			RuleID:    parsedMessage.Code.Code,
			Level:     parsedMessage.Level,
			Message:   parsedMessage.Message,
			FilePath:  sourceSpan.FileName,
			StartLine: sourceSpan.LineStart,
			StartCol:  sourceSpan.ColumnStart,
			EndLine:   sourceSpan.LineEnd,
			EndCol:    sourceSpan.ColumnEnd,
		})
	}
	return result
}

func primarySpan(spans []rustcSpan) *rustcSpan {
	for i := range spans {
		if spans[i].IsPrimary {
			return &spans[i]
		}
	}
	if len(spans) > 0 {
		return &spans[0]
	}
	return nil
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

func toSARIF(diagnostics []diagnostic) sarifReport {
	ruleSet := make(map[string]bool)
	var rules []sarifRule
	var results []sarifResult

	for _, diagnosticEntry := range diagnostics {
		if !ruleSet[diagnosticEntry.RuleID] {
			ruleSet[diagnosticEntry.RuleID] = true
			rules = append(rules, sarifRule{ID: diagnosticEntry.RuleID})
		}

		results = append(results, sarifResult{
			RuleID:  diagnosticEntry.RuleID,
			Level:   diagnosticEntry.Level,
			Message: sarifMessage{Text: diagnosticEntry.Message},
			Locations: []sarifLocation{{
				PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifact{URI: toURI(diagnosticEntry.FilePath)},
					Region: sarifRegion{
						StartLine:   diagnosticEntry.StartLine,
						StartColumn: diagnosticEntry.StartCol,
						EndLine:     diagnosticEntry.EndLine,
						EndColumn:   diagnosticEntry.EndCol,
					},
				},
			}},
		})
	}

	if rules == nil {
		rules = []sarifRule{}
	}
	if results == nil {
		results = []sarifResult{}
	}

	return sarifReport{
		Version: "2.1.0",
		Schema:  "https://schemastore.azurewebsites.net/schemas/json/sarif-2.1.0.json",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name:  "clippy",
				Rules: rules,
			}},
			Results: results,
		}},
	}
}

func toURI(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}
