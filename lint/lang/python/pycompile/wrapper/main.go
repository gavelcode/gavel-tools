package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gavelcode/gavel-tools/lint/sarif"
)

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
	Name            string      `json:"name"`
	InformationURI  string      `json:"informationUri,omitempty"`
	SemanticVersion string      `json:"semanticVersion,omitempty"`
	Rules           []sarifRule `json:"rules,omitempty"`
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
}

type finding struct {
	RuleID  string
	Level   string
	Message string
	File    string
	Line    int
}

const (
	exitCodeMisuse     = 2
	expectedMatchParts = 2
	dirPermission      = 0o755
	filePermission     = 0o644
)

var pythonBinary string

func main() { os.Exit(execute()) }

func execute() int {
	python := flag.String("python", "", "Path to the python3 binary")
	outputPath := flag.String("out", "", "SARIF output path")
	flag.Parse()
	if *outputPath == "" {
		fmt.Fprintln(os.Stderr, "--out is required")
		return exitCodeMisuse
	}

	pythonBinary = resolvePython(*python)

	findings, failures := analyze(flag.Args())
	invocation := sarif.Successful()
	if len(failures) > 0 {
		invocation = sarif.Failed(failures...)
	}
	if err := writeSARIF(*outputPath, findings, invocation); err != nil {
		fmt.Fprintf(os.Stderr, "write SARIF: %v\n", err)
		return 1
	}
	return 0
}

func analyze(paths []string) ([]finding, []string) {
	findings := make([]finding, 0)
	var failures []string
	for _, filePath := range paths {
		compiled, failure := compileFindings(filePath)
		findings = append(findings, compiled...)
		if failure != "" {
			failures = append(failures, failure)
		}
		findings = append(findings, evalFindings(filePath)...)
	}
	return findings, failures
}

func resolvePython(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if p, err := exec.LookPath("python3"); err == nil {
		return p
	}
	return "python3"
}

func compileFindings(filePath string) ([]finding, string) {
	cmd := exec.Command(pythonBinary, "-m", "py_compile", filePath)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err == nil {
		return nil, ""
	}

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		// The interpreter itself could not run — a tool failure, not a user
		// syntax error — so it must surface as executionSuccessful=false, never
		// as a finding.
		return nil, fmt.Sprintf("py_compile could not run the interpreter on %s: %v", filePath, err)
	}

	lineNumber := parsePythonErrorLine(stderr.String())
	return []finding{{
		RuleID:  "python/pycompile",
		Level:   "error",
		Message: strings.TrimSpace(stderr.String()),
		File:    filePath,
		Line:    lineNumber,
	}}, ""
}

func evalFindings(filePath string) []finding {
	body, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(body), "\n")
	findings := make([]finding, 0)
	for index, line := range lines {
		if strings.Contains(line, "eval(") {
			findings = append(findings, finding{
				RuleID:  "python/builtin-eval",
				Level:   "warning",
				Message: "Use of eval executes dynamic code and should be avoided unless strictly controlled.",
				File:    filePath,
				Line:    index + 1,
			})
		}
	}
	return findings
}

var pythonLinePattern = regexp.MustCompile(`line ([0-9]+)`)

func parsePythonErrorLine(stderr string) int {
	match := pythonLinePattern.FindStringSubmatch(stderr)
	if len(match) != expectedMatchParts {
		return 1
	}
	line, err := strconv.Atoi(match[1])
	if err != nil {
		return 1
	}
	return line
}

func writeSARIF(filePath string, findings []finding, invocation sarif.Invocation) error {
	results := make([]sarifResult, 0, len(findings))
	for _, findingEntry := range findings {
		results = append(results, sarifResult{
			RuleID:  findingEntry.RuleID,
			Level:   findingEntry.Level,
			Message: sarifMessage{Text: findingEntry.Message},
			Locations: []sarifLocation{{
				PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifactLocation{URI: filepath.ToSlash(findingEntry.File)},
					Region:           sarifRegion{StartLine: findingEntry.Line},
				},
			}},
		})
	}

	sarifDoc := sarifLog{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs: []sarifRun{{
			Tool: sarifTool{Driver: sarifDriver{
				Name: "python-pycompile",
				Rules: []sarifRule{
					{
						ID:               "python/pycompile",
						Name:             "py_compile",
						ShortDescription: sarifMessage{Text: "Python source failed bytecode compilation."},
					},
					{
						ID:               "python/builtin-eval",
						Name:             "builtin eval",
						ShortDescription: sarifMessage{Text: "Python source uses eval."},
					},
				},
			}},
			Results:     results,
			Invocations: []sarif.Invocation{invocation},
		}},
	}

	if err := os.MkdirAll(filepath.Dir(filePath), dirPermission); err != nil {
		return err
	}
	body, err := json.MarshalIndent(sarifDoc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filePath, append(body, '\n'), filePermission)
}
