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
	out := flag.String("out", "", "SARIF output path")
	flag.Parse()

	if *out == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return missingArgCode
	}
	jars := flag.Args()
	if len(jars) == 0 {
		fmt.Fprintln(os.Stderr, "missing jars")
		return missingArgCode
	}

	if err := run(*spotbugs, *out, jars); err != nil {
		fmt.Fprintf(os.Stderr, "run spotbugs: %v\n", err)
		return 1
	}
	return 0
}

func run(spotbugs, out string, jars []string) error {
	if err := os.MkdirAll(filepath.Dir(out), dirPermission); err != nil {
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

	xmlPath := out + ".xml"
	defer func() { _ = os.Remove(xmlPath) }()
	args := append([]string{
		"-textui",
		"-xml:withMessages",
		"-output",
		xmlPath,
	}, jars...)
	cmd := exec.Command(spotbugs, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return writeSARIF(out, failedSARIF(fmt.Sprintf("SpotBugs failed to run: %v", err)))
	}

	doc, err := readXML(xmlPath)
	if err != nil {
		return writeSARIF(out, failedSARIF(fmt.Sprintf("SpotBugs output could not be parsed: %v", err)))
	}
	return writeSARIF(out, toSARIF(doc))
}

func readXML(path string) (bugCollection, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return bugCollection{}, fmt.Errorf("read spotbugs xml: %w", err)
	}
	var doc bugCollection
	if err := xml.Unmarshal(body, &doc); err != nil {
		return bugCollection{}, fmt.Errorf("decode spotbugs xml: %w", err)
	}
	return doc, nil
}

func toSARIF(doc bugCollection) sarifLog {
	rules := make(map[string]sarifRule)
	results := make([]sarifResult, 0, len(doc.Bugs))
	for _, bug := range doc.Bugs {
		rules[bug.Type] = sarifRule{
			ID:   bug.Type,
			Name: bug.Category,
			ShortDescription: sarifMessage{
				Text: bug.Short,
			},
		}
		results = append(results, toResult(bug))
	}

	return sarifLog{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs: []sarifRun{{
			Tool: sarifTool{
				Driver: sarifDriver{
					Name:    "SpotBugs",
					Version: doc.Version,
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

func toResult(bug bugInstance) sarifResult {
	message := bug.LongMessage
	if message == "" {
		message = bug.Short
	}
	return sarifResult{
		RuleID:    bug.Type,
		Level:     levelForPriority(bug.Priority),
		Message:   sarifMessage{Text: message},
		Locations: locationsFor(bug.SourceLine),
	}
}

func locationsFor(lines []sourceLine) []sarifLocation {
	if len(lines) == 0 {
		return nil
	}
	line := lines[0]
	if line.SourcePath == "" {
		return nil
	}
	return []sarifLocation{{
		PhysicalLocation: sarifPhysicalLocation{
			ArtifactLocation: sarifArtifactLocation{URI: filepath.ToSlash(line.SourcePath)},
			Region: sarifRegion{
				StartLine: line.Start,
				EndLine:   line.End,
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
	out := make([]sarifRule, 0, len(rules))
	for _, rule := range rules {
		out = append(out, rule)
	}
	return out
}

func writeSARIF(path string, doc sarifLog) (err error) {
	sarifFile, createErr := os.Create(path)
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
	if encodeErr := encoder.Encode(doc); encodeErr != nil {
		return fmt.Errorf("encode sarif: %w", encodeErr)
	}
	return nil
}

func resolveBazelExternal(path string) string {
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if suffix, ok := strings.CutPrefix(path, "external/"); ok {
		alternate := filepath.Join("..", "..", path)
		if _, err := os.Stat(alternate); err == nil {
			return alternate
		}
		matches, err := filepath.Glob(filepath.Join("..", "..", "external", "*"+suffix))
		if err == nil && len(matches) > 0 {
			return matches[0]
		}
	}
	return path
}
