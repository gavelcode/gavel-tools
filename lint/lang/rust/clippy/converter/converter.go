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
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var msg rustcMessage
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if msg.MessageType != "diagnostic" {
			continue
		}
		if msg.Code == nil || msg.Code.Code == "" {
			continue
		}
		if msg.Level != "warning" && msg.Level != "error" {
			continue
		}

		span := primarySpan(msg.Spans)
		if span == nil {
			continue
		}

		result = append(result, diagnostic{
			RuleID:    msg.Code.Code,
			Level:     msg.Level,
			Message:   msg.Message,
			FilePath:  span.FileName,
			StartLine: span.LineStart,
			StartCol:  span.ColumnStart,
			EndLine:   span.LineEnd,
			EndCol:    span.ColumnEnd,
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

	for _, diag := range diagnostics {
		if !ruleSet[diag.RuleID] {
			ruleSet[diag.RuleID] = true
			rules = append(rules, sarifRule{ID: diag.RuleID})
		}

		results = append(results, sarifResult{
			RuleID:  diag.RuleID,
			Level:   diag.Level,
			Message: sarifMessage{Text: diag.Message},
			Locations: []sarifLocation{{
				PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifact{URI: toURI(diag.FilePath)},
					Region: sarifRegion{
						StartLine:   diag.StartLine,
						StartColumn: diag.StartCol,
						EndLine:     diag.EndLine,
						EndColumn:   diag.EndCol,
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

