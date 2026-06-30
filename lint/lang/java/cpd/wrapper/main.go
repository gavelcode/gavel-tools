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

type pmdCPD struct {
	XMLName      xml.Name      `xml:"pmd-cpd"`
	Duplications []duplication `xml:"duplication"`
}

type duplication struct {
	Lines        int       `xml:"lines,attr"`
	Tokens       int       `xml:"tokens,attr"`
	Files        []cpdFile `xml:"file"`
	CodeFragment string    `xml:"codefragment"`
}

type cpdFile struct {
	Line int    `xml:"line,attr"`
	Path string `xml:"path,attr"`
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
	Name  string      `json:"name"`
	Rules []sarifRule `json:"rules,omitempty"`
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

const (
	defaultMinTokens = 100
	missingArgCode   = 2
	dirPermission    = 0o755
)

func main() { os.Exit(execute()) }

func execute() int {
	pmdPath := flag.String("pmd", "", "Path to the pinned PMD executable")
	outputPath := flag.String("out", "", "SARIF output path")
	minimumTokens := flag.Int("minimum-tokens", defaultMinTokens, "Minimum token length for duplicate detection")
	flag.Parse()

	if *outputPath == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return missingArgCode
	}
	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing Java source files")
		return missingArgCode
	}

	if err := run(*pmdPath, *outputPath, *minimumTokens, files); err != nil {
		fmt.Fprintf(os.Stderr, "run cpd: %v\n", err)
		return 1
	}
	return 0
}

func run(pmdPath, outputPath string, minimumTokens int, files []string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	if pmdPath == "" {
		bin, err := exec.LookPath("pmd")
		if err != nil {
			return errors.New("pmd not found in PATH and --pmd was not provided")
		}
		pmdPath = bin
	}
	pmdPath = resolveBazelExternal(pmdPath)

	fileList, err := writeFileList(files)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(fileList) }()

	xmlPath := outputPath + ".xml"
	defer func() { _ = os.Remove(xmlPath) }()

	arguments := []string{
		"cpd",
		"--format=xml",
		fmt.Sprintf("--minimum-tokens=%d", minimumTokens),
		"--no-fail-on-violation",
		"--file-list=" + fileList,
	}
	command := exec.Command(pmdPath, arguments...)
	xmlOutput, err := os.Create(xmlPath)
	if err != nil {
		return fmt.Errorf("create xml output: %w", err)
	}
	command.Stdout = xmlOutput
	command.Stderr = os.Stderr
	command.Env = commandEnv()
	if runErr := command.Run(); runErr != nil {
		_ = xmlOutput.Close()
		return writeSARIF(outputPath, failedSARIF(fmt.Sprintf("CPD failed to run: %v", runErr)))
	}
	_ = xmlOutput.Close()

	cpdReport, err := readCPDXML(xmlPath)
	if err != nil {
		return writeSARIF(outputPath, failedSARIF(fmt.Sprintf("CPD output could not be parsed: %v", err)))
	}
	return writeSARIF(outputPath, toSARIF(cpdReport))
}

func readCPDXML(filePath string) (pmdCPD, error) {
	body, err := os.ReadFile(filePath)
	if err != nil {
		return pmdCPD{}, fmt.Errorf("read cpd xml: %w", err)
	}
	var cpdReport pmdCPD
	if err := xml.Unmarshal(body, &cpdReport); err != nil {
		return pmdCPD{}, fmt.Errorf("decode cpd xml: %w", err)
	}
	return cpdReport, nil
}

func toSARIF(cpdReport pmdCPD) sarifLog {
	results := make([]sarifResult, 0, len(cpdReport.Duplications))
	for _, duplicationEntry := range cpdReport.Duplications {
		results = append(results, toResult(duplicationEntry))
	}

	return sarifLog{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs: []sarifRun{{
			Tool: sarifTool{
				Driver: sarifDriver{
					Name: "CPD",
					Rules: []sarifRule{{
						ID:               "cpd/duplicate-code",
						Name:             "DuplicateCode",
						ShortDescription: sarifMessage{Text: "Duplicated block of code detected."},
					}},
				},
			},
			Results:     results,
			Invocations: []sarif.Invocation{sarif.Successful()},
		}},
	}
}

// failedSARIF reports that CPD could not produce trustworthy results, carrying
// the concrete reason so a consumer can fix the environment.
func failedSARIF(reason string) sarifLog {
	return sarifLog{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs: []sarifRun{{
			Tool:        sarifTool{Driver: sarifDriver{Name: "CPD"}},
			Results:     []sarifResult{},
			Invocations: []sarif.Invocation{sarif.Failed(reason)},
		}},
	}
}

func toResult(duplicationEntry duplication) sarifResult {
	locations := make([]sarifLocation, 0, len(duplicationEntry.Files))
	filePaths := make([]string, 0, len(duplicationEntry.Files))
	for _, cpdFileEntry := range duplicationEntry.Files {
		locations = append(locations, sarifLocation{
			PhysicalLocation: sarifPhysicalLocation{
				ArtifactLocation: sarifArtifactLocation{URI: filepath.ToSlash(cpdFileEntry.Path)},
				Region:           sarifRegion{StartLine: cpdFileEntry.Line},
			},
		})
		filePaths = append(filePaths, filepath.Base(cpdFileEntry.Path))
	}

	message := fmt.Sprintf("Duplicated block of %d lines (%d tokens) across %s.",
		duplicationEntry.Lines, duplicationEntry.Tokens, strings.Join(filePaths, ", "))

	return sarifResult{
		RuleID:    "cpd/duplicate-code",
		Level:     "warning",
		Message:   sarifMessage{Text: message},
		Locations: locations,
	}
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

func writeFileList(files []string) (_ string, err error) {
	listFile, createErr := os.CreateTemp("", "gavel-cpd-files-*")
	if createErr != nil {
		return "", fmt.Errorf("create file list: %w", createErr)
	}
	defer func() {
		if closeErr := listFile.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	for _, filePath := range files {
		if _, writeErr := fmt.Fprintln(listFile, filePath); writeErr != nil {
			return "", fmt.Errorf("write file list: %w", writeErr)
		}
	}
	return listFile.Name(), nil
}

func commandEnv() []string {
	environment := sanitizedEnv()
	if _, ok := lookupEnv(environment, "JAVA_HOME"); ok {
		return environment
	}

	javaHome := os.Getenv("JAVA_HOME")
	if javaHome != "" {
		return append(environment, "JAVA_HOME="+javaHome)
	}
	return environment
}

func sanitizedEnv() []string {
	environment := make([]string, 0, len(os.Environ()))
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "JAVA_HOME=") {
			continue
		}
		environment = append(environment, item)
	}
	return environment
}

func lookupEnv(environment []string, key string) (string, bool) {
	prefix := key + "="
	for _, item := range environment {
		if val, ok := strings.CutPrefix(item, prefix); ok {
			return val, true
		}
	}
	return "", false
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
