package main

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gavelcode/gavel-tools/lint/sarif"
)

type bugCollection struct {
	Version string        `xml:"version,attr"`
	Bugs    []bugInstance `xml:"BugInstance"`
}

type bugInstance struct {
	Type        string       `xml:"type,attr"`
	Priority    string       `xml:"priority,attr"`
	Category    string       `xml:"category,attr"`
	LongMessage string       `xml:"LongMessage"`
	Short       string       `xml:"ShortMessage"`
	SourceLine  []sourceLine `xml:"SourceLine"`
}

type sourceLine struct {
	SourcePath string `xml:"sourcepath,attr"`
	Start      int    `xml:"start,attr"`
	End        int    `xml:"end,attr"`
}

type sarifLog struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
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
	Name    string      `json:"name"`
	Version string      `json:"version,omitempty"`
	Rules   []sarifRule `json:"rules,omitempty"`
}

type sarifRule struct {
	ID               string       `json:"id"`
	Name             string       `json:"name,omitempty"`
	ShortDescription sarifMessage `json:"shortDescription,omitempty"`
}

type sarifResult struct {
	RuleID    string          `json:"ruleId"`
	Level     string          `json:"level"`
	Message   sarifMessage    `json:"message"`
	Locations []sarifLocation `json:"locations,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           sarifRegion           `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine int `json:"startLine,omitempty"`
	EndLine   int `json:"endLine,omitempty"`
}

const (
	missingArgCode = 2
	dirPermission  = 0o755
)

func main() { os.Exit(execute()) }

func execute() int {
	spotbugs := flag.String("spotbugs", "", "Path to the pinned SpotBugs executable")
	outputPath := flag.String("out", "", "SARIF output path")
	flag.Parse()

	if *outputPath == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return missingArgCode
	}
	jarFiles := flag.Args()
	if len(jarFiles) == 0 {
		fmt.Fprintln(os.Stderr, "missing jars")
		return missingArgCode
	}

	if err := run(*spotbugs, *outputPath, jarFiles); err != nil {
		fmt.Fprintf(os.Stderr, "run spotbugs: %v\n", err)
		return 1
	}
	return 0
}

func run(spotbugs, outputPath string, jarFiles []string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	if spotbugs == "" {
		bin, err := exec.LookPath("spotbugs")
		if err != nil {
			return errors.New("spotbugs not found in PATH and --spotbugs was not provided")
		}
		spotbugs = bin
	}
	spotbugs = resolveBazelExternal(spotbugs)

	xmlPath := outputPath + ".xml"
	defer func() { _ = os.Remove(xmlPath) }()
	arguments := append([]string{
		"-textui",
		"-xml:withMessages",
		"-output",
		xmlPath,
	}, jarFiles...)
	cmd := exec.Command(spotbugs, arguments...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return writeSARIF(outputPath, failedSARIF(fmt.Sprintf("SpotBugs failed to run: %v", err)))
	}

	bugReport, err := readXML(xmlPath)
	if err != nil {
		return writeSARIF(outputPath, failedSARIF(fmt.Sprintf("SpotBugs output could not be parsed: %v", err)))
	}
	return writeSARIF(outputPath, toSARIF(bugReport))
}

func readXML(filePath string) (bugCollection, error) {
	body, err := os.ReadFile(filePath)
	if err != nil {
		return bugCollection{}, fmt.Errorf("read spotbugs xml: %w", err)
	}
	var bugReport bugCollection
	if err := xml.Unmarshal(body, &bugReport); err != nil {
		return bugCollection{}, fmt.Errorf("decode spotbugs xml: %w", err)
	}
	return bugReport, nil
}

func toSARIF(bugReport bugCollection) sarifLog {
	rules := make(map[string]sarifRule)
	results := make([]sarifResult, 0, len(bugReport.Bugs))
	for _, bugInstance := range bugReport.Bugs {
		rules[bugInstance.Type] = sarifRule{
			ID:   bugInstance.Type,
			Name: bugInstance.Category,
			ShortDescription: sarifMessage{
				Text: bugInstance.Short,
			},
		}
		results = append(results, toResult(bugInstance))
	}

	return sarifLog{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs: []sarifRun{{
			Tool: sarifTool{
				Driver: sarifDriver{
					Name:    "SpotBugs",
					Version: bugReport.Version,
					Rules:   ruleList(rules),
				},
			},
			Results:     results,
			Invocations: []sarif.Invocation{sarif.Successful()},
		}},
	}
}

// failedSARIF reports that SpotBugs could not produce trustworthy results,
// carrying the concrete reason so a consumer can fix the environment.
func failedSARIF(reason string) sarifLog {
	return sarifLog{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs: []sarifRun{{
			Tool:        sarifTool{Driver: sarifDriver{Name: "SpotBugs"}},
			Results:     []sarifResult{},
			Invocations: []sarif.Invocation{sarif.Failed(reason)},
		}},
	}
}

func toResult(bugInstance bugInstance) sarifResult {
	message := bugInstance.LongMessage
	if message == "" {
		message = bugInstance.Short
	}
	return sarifResult{
		RuleID:    bugInstance.Type,
		Level:     levelForPriority(bugInstance.Priority),
		Message:   sarifMessage{Text: message},
		Locations: locationsFor(bugInstance.SourceLine),
	}
}

func locationsFor(lines []sourceLine) []sarifLocation {
	if len(lines) == 0 {
		return nil
	}
	lineText := lines[0]
	if lineText.SourcePath == "" {
		return nil
	}
	return []sarifLocation{{
		PhysicalLocation: sarifPhysicalLocation{
			ArtifactLocation: sarifArtifactLocation{URI: filepath.ToSlash(lineText.SourcePath)},
			Region: sarifRegion{
				StartLine: lineText.Start,
				EndLine:   lineText.End,
			},
		},
	}}
}

func levelForPriority(priority string) string {
	switch priority {
	case "1":
		return "error"
	case "2":
		return "warning"
	default:
		return "note"
	}
}

func ruleList(rules map[string]sarifRule) []sarifRule {
	outputPath := make([]sarifRule, 0, len(rules))
	for _, rule := range rules {
		outputPath = append(outputPath, rule)
	}
	return outputPath
}

func writeSARIF(filePath string, sarifDoc sarifLog) (err error) {
	sarifFile, createErr := os.Create(filePath)
	if createErr != nil {
		return fmt.Errorf("create sarif: %w", createErr)
	}
	defer func() {
		if closeErr := sarifFile.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	encoder := json.NewEncoder(sarifFile)
	encoder.SetIndent("", "  ")
	if encodeErr := encoder.Encode(sarifDoc); encodeErr != nil {
		return fmt.Errorf("encode sarif: %w", encodeErr)
	}
	return nil
}

func resolveBazelExternal(filePath string) string {
	if _, err := os.Stat(filePath); err == nil {
		return filePath
	}
	if suffix, ok := strings.CutPrefix(filePath, "external/"); ok {
		alternate := filepath.Join("..", "..", filePath)
		if _, err := os.Stat(alternate); err == nil {
			return alternate
		}
		matches, err := filepath.Glob(filepath.Join("..", "..", "external", "*"+suffix))
		if err == nil && len(matches) > 0 {
			return matches[0]
		}
	}
	return filePath
}
