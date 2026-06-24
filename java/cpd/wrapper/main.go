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
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
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
	pmd := flag.String("pmd", "", "Path to the pinned PMD executable")
	out := flag.String("out", "", "SARIF output path")
	minimumTokens := flag.Int("minimum-tokens", defaultMinTokens, "Minimum token length for duplicate detection")
	flag.Parse()

	if *out == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return missingArgCode
	}
	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing Java source files")
		return missingArgCode
	}

	if err := run(*pmd, *out, *minimumTokens, files); err != nil {
		fmt.Fprintf(os.Stderr, "run cpd: %v\n", err)
		return 1
	}
	return 0
}

func run(pmd, out string, minimumTokens int, files []string) error {
	if err := os.MkdirAll(filepath.Dir(out), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	if pmd == "" {
		bin, err := exec.LookPath("pmd")
		if err != nil {
			return errors.New("pmd not found in PATH and --pmd was not provided")
		}
		pmd = bin
	}
	pmd = resolveBazelExternal(pmd)

	fileList, err := writeFileList(files)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(fileList) }()

	xmlPath := out + ".xml"
	defer func() { _ = os.Remove(xmlPath) }()

	args := []string{
		"cpd",
		"--format=xml",
		fmt.Sprintf("--minimum-tokens=%d", minimumTokens),
		"--no-fail-on-violation",
		"--file-list=" + fileList,
	}
	cmd := exec.Command(pmd, args...)
	xmlOutput, err := os.Create(xmlPath)
	if err != nil {
		return fmt.Errorf("create xml output: %w", err)
	}
	cmd.Stdout = xmlOutput
	cmd.Stderr = os.Stderr
	cmd.Env = commandEnv()
	if runErr := cmd.Run(); runErr != nil {
		_ = xmlOutput.Close()
		return fmt.Errorf("%s %v: %w", pmd, args, runErr)
	}
	_ = xmlOutput.Close()

	doc, err := readCPDXML(xmlPath)
	if err != nil {
		return err
	}
	return writeSARIF(out, toSARIF(doc))
}

func readCPDXML(path string) (pmdCPD, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return pmdCPD{}, fmt.Errorf("read cpd xml: %w", err)
	}
	var doc pmdCPD
	if err := xml.Unmarshal(body, &doc); err != nil {
		return pmdCPD{}, fmt.Errorf("decode cpd xml: %w", err)
	}
	return doc, nil
}

func toSARIF(doc pmdCPD) sarifLog {
	results := make([]sarifResult, 0, len(doc.Duplications))
	for _, dup := range doc.Duplications {
		results = append(results, toResult(dup))
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
			Results: results,
		}},
	}
}

func toResult(dup duplication) sarifResult {
	locations := make([]sarifLocation, 0, len(dup.Files))
	filePaths := make([]string, 0, len(dup.Files))
	for _, file := range dup.Files {
		locations = append(locations, sarifLocation{
			PhysicalLocation: sarifPhysicalLocation{
				ArtifactLocation: sarifArtifactLocation{URI: filepath.ToSlash(file.Path)},
				Region:           sarifRegion{StartLine: file.Line},
			},
		})
		filePaths = append(filePaths, filepath.Base(file.Path))
	}

	message := fmt.Sprintf("Duplicated block of %d lines (%d tokens) across %s.",
		dup.Lines, dup.Tokens, strings.Join(filePaths, ", "))

	return sarifResult{
		RuleID:    "cpd/duplicate-code",
		Level:     "warning",
		Message:   sarifMessage{Text: message},
		Locations: locations,
	}
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

	for _, path := range files {
		if _, writeErr := fmt.Fprintln(listFile, path); writeErr != nil {
			return "", fmt.Errorf("write file list: %w", writeErr)
		}
	}
	return listFile.Name(), nil
}

func commandEnv() []string {
	env := sanitizedEnv()
	if _, ok := lookupEnv(env, "JAVA_HOME"); ok {
		return env
	}

	javaHome := os.Getenv("JAVA_HOME")
	if javaHome != "" {
		return append(env, "JAVA_HOME="+javaHome)
	}
	return env
}

func sanitizedEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "JAVA_HOME=") {
			continue
		}
		env = append(env, item)
	}
	return env
}

func lookupEnv(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, item := range env {
		if val, ok := strings.CutPrefix(item, prefix); ok {
			return val, true
		}
	}
	return "", false
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
