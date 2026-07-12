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

type finding struct {
	RuleID  string
	Level   string
	Message string
	File    string
	Line    int
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

var javacAddExports = []string{
	"-J--add-exports=jdk.compiler/com.sun.tools.javac.api=ALL-UNNAMED",
	"-J--add-exports=jdk.compiler/com.sun.tools.javac.file=ALL-UNNAMED",
	"-J--add-exports=jdk.compiler/com.sun.tools.javac.main=ALL-UNNAMED",
	"-J--add-exports=jdk.compiler/com.sun.tools.javac.model=ALL-UNNAMED",
	"-J--add-exports=jdk.compiler/com.sun.tools.javac.parser=ALL-UNNAMED",
	"-J--add-exports=jdk.compiler/com.sun.tools.javac.processing=ALL-UNNAMED",
	"-J--add-exports=jdk.compiler/com.sun.tools.javac.tree=ALL-UNNAMED",
	"-J--add-exports=jdk.compiler/com.sun.tools.javac.util=ALL-UNNAMED",
	"-J--add-opens=jdk.compiler/com.sun.tools.javac.code=ALL-UNNAMED",
	"-J--add-opens=jdk.compiler/com.sun.tools.javac.comp=ALL-UNNAMED",
}

const (
	missingArgCode = 2
	dirPermission  = 0o755
	baseTen        = 10
)

func main() { os.Exit(execute()) }

func execute() int {
	errorProneJar := flag.String("error-prone-jar", "", "Path to error_prone_core-with-dependencies.jar")
	dataflowJar := flag.String("dataflow-jar", "", "Path to dataflow-errorprone.jar")
	javacFlag := flag.String("javac", "", "Path to the javac binary")
	outputPath := flag.String("out", "", "SARIF output path")
	classpath := flag.String("classpath", "", "Compilation classpath (colon-separated)")
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

	if err := run(*errorProneJar, *dataflowJar, *javacFlag, *outputPath, *classpath, files); err != nil {
		fmt.Fprintf(os.Stderr, "run error-prone: %v\n", err)
		return 1
	}
	return 0
}

func run(errorProneJar, dataflowJar, javacPath, outputPath, classpath string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	errorProneJar = resolveBazelExternal(errorProneJar)
	if dataflowJar != "" {
		dataflowJar = resolveBazelExternal(dataflowJar)
	}

	var javac string
	var err error
	if javacPath != "" {
		javac = javacPath
	} else {
		javac, err = findJavac()
		if err != nil {
			return err
		}
	}

	tmpDir, err := os.MkdirTemp("", "gavel-errorprone-*")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(tmpDir) }()

	fileList, err := writeFileList(files)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(fileList) }()

	processorpath := errorProneJar
	if dataflowJar != "" {
		processorpath = errorProneJar + ":" + dataflowJar
	}

	arguments := buildJavacArgs(processorpath, classpath, tmpDir, fileList)
	command := exec.Command(javac, arguments...)
	var stderr bytes.Buffer
	command.Stdout = os.Stdout
	command.Stderr = &stderr
	command.Env = os.Environ()

	runErr := command.Run()

	stderrStr := stderr.String()
	if stderrStr != "" {
		fmt.Fprint(os.Stderr, stderrStr)
	}

	findings := parseDiagnostics(stderrStr)
	invocation := executionInvocation(runErr, detectCompilerErrors(stderrStr))
	return writeSARIF(outputPath, toSARIF(findings, invocation))
}

// detectCompilerErrors returns the genuine javac compilation errors in stderr —
// the ones without an Error Prone [CheckName] tag. Those mean javac could not
// fully compile the target, so Error Prone analyzed it only partially.
func detectCompilerErrors(stderr string) []string {
	var errorLines []string
	for lineText := range strings.SplitSeq(stderr, "\n") {
		if !strings.Contains(lineText, ": error:") {
			continue
		}
		if diagnosticPattern.MatchString(lineText) {
			continue
		}
		errorLines = append(errorLines, strings.TrimSpace(lineText))
	}
	return errorLines
}

// executionInvocation classifies the Error Prone run into the SARIF invocation
// the consumer needs. Two very different things look similar in stderr and must
// not be conflated: javac that could not launch at all is a hard failure that
// should fail the verdict; javac that launched but hit compilation errors (the
// usual cause being a target whose annotation processors — Lombok, etc. — we
// cannot replay) means Error Prone still ran and reported what it could, so the
// analysis is degraded, not failed. Degraded runs stay executionSuccessful=true
// and explain themselves as warnings, so a clean repo is never failed just for
// using annotation processors.
func executionInvocation(runErr error, compileErrors []string) sarif.Invocation {
	var exitErr *exec.ExitError
	if runErr != nil && !errors.As(runErr, &exitErr) {
		notes := []string{fmt.Sprintf("Error Prone could not run javac: %v", runErr)}
		if len(compileErrors) > 0 {
			notes = append(notes, compileErrorNote(compileErrors))
		}
		return sarif.Failed(notes...)
	}
	if len(compileErrors) > 0 {
		return sarif.Degraded(compileErrorNote(compileErrors))
	}
	return sarif.Successful()
}

func compileErrorNote(compileErrors []string) string {
	return fmt.Sprintf(
		"%d javac compilation error(s) prevented complete Error Prone analysis; results are incomplete. First: %s",
		len(compileErrors), compileErrors[0])
}

func buildJavacArgs(processorpath, classpath, tmpDir, fileList string) []string {
	arguments := make([]string, 0, len(javacAddExports)+baseTen)
	arguments = append(arguments, javacAddExports...)
	arguments = append(arguments,
		"-XDcompilePolicy=simple",
		"-XDaddTypeAnnotationsToSymbol=true",
		"--should-stop=ifError=FLOW",
		"-processorpath", processorpath,
		"-Xplugin:ErrorProne",
		"-d", tmpDir,
	)

	if classpath != "" {
		arguments = append(arguments, "-classpath", classpath)
	}

	arguments = append(arguments, "@"+fileList)
	return arguments
}

func findJavac() (string, error) {
	javaHome := os.Getenv("JAVA_HOME")
	if javaHome != "" {
		candidate := filepath.Join(javaHome, "bin", "javac")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	filePath, err := exec.LookPath("javac")
	if err != nil {
		return "", fmt.Errorf("javac not found: set JAVA_HOME or ensure javac is in PATH")
	}
	return filePath, nil
}

var diagnosticPattern = regexp.MustCompile(`^(.+):(\d+): (error|warning|note): \[(\w+)](.*)$`)

func parseDiagnostics(stderr string) []finding {
	findings := make([]finding, 0)
	for lineText := range strings.SplitSeq(stderr, "\n") {
		match := diagnosticPattern.FindStringSubmatch(lineText)
		if match == nil {
			continue
		}
		lineNum, err := strconv.Atoi(match[2])
		if err != nil {
			continue
		}
		findings = append(findings, finding{
			File:    match[1],
			Line:    lineNum,
			Level:   javacLevelToSARIF(match[3]),
			RuleID:  match[4],
			Message: strings.TrimSpace(match[5]),
		})
	}
	return findings
}

func javacLevelToSARIF(level string) string {
	switch level {
	case "error":
		return "error"
	case "warning":
		return "warning"
	default:
		return "note"
	}
}

func toSARIF(findings []finding, invocation sarif.Invocation) sarifLog {
	rules := make(map[string]sarifRule)
	results := make([]sarifResult, 0, len(findings))

	for _, diagnostic := range findings {
		rules[diagnostic.RuleID] = sarifRule{
			ID:               diagnostic.RuleID,
			ShortDescription: sarifMessage{Text: diagnostic.RuleID},
		}
		results = append(results, sarifResult{
			RuleID:  diagnostic.RuleID,
			Level:   diagnostic.Level,
			Message: sarifMessage{Text: diagnostic.Message},
			Locations: []sarifLocation{{
				PhysicalLocation: sarifPhysicalLocation{
					ArtifactLocation: sarifArtifactLocation{URI: filepath.ToSlash(diagnostic.File)},
					Region:           sarifRegion{StartLine: diagnostic.Line},
				},
			}},
		})
	}

	return sarifLog{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs: []sarifRun{{
			Tool: sarifTool{
				Driver: sarifDriver{
					Name:  "ErrorProne",
					Rules: ruleList(rules),
				},
			},
			Results:     results,
			Invocations: []sarif.Invocation{invocation},
		}},
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

func writeFileList(files []string) (_ string, err error) {
	listFile, createErr := os.CreateTemp("", "gavel-errorprone-files-*")
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
