package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
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
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
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
	out := flag.String("out", "", "SARIF output path")
	classpath := flag.String("classpath", "", "Compilation classpath (colon-separated)")
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

	if err := run(*errorProneJar, *dataflowJar, *javacFlag, *out, *classpath, files); err != nil {
		fmt.Fprintf(os.Stderr, "run error-prone: %v\n", err)
		return 1
	}
	return 0
}

func run(errorProneJar, dataflowJar, javacPath, out, classpath string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(out), dirPermission); err != nil {
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

	args := buildJavacArgs(processorpath, classpath, tmpDir, fileList)
	cmd := exec.Command(javac, args...)
	var stderr bytes.Buffer
	cmd.Stdout = os.Stdout
	cmd.Stderr = &stderr
	cmd.Env = os.Environ()

	_ = cmd.Run()

	stderrStr := stderr.String()
	if stderrStr != "" {
		fmt.Fprint(os.Stderr, stderrStr)
	}

	findings := parseDiagnostics(stderrStr)
	return writeSARIF(out, toSARIF(findings))
}

func buildJavacArgs(processorpath, classpath, tmpDir, fileList string) []string {
	args := make([]string, 0, len(javacAddExports)+baseTen)
	args = append(args, javacAddExports...)
	args = append(args,
		"-XDcompilePolicy=simple",
		"-XDaddTypeAnnotationsToSymbol=true",
		"--should-stop=ifError=FLOW",
		"-processorpath", processorpath,
		"-Xplugin:ErrorProne",
		"-d", tmpDir,
	)

	if classpath != "" {
		args = append(args, "-classpath", classpath)
	}

	args = append(args, "@"+fileList)
	return args
}

func findJavac() (string, error) {
	javaHome := os.Getenv("JAVA_HOME")
	if javaHome != "" {
		candidate := filepath.Join(javaHome, "bin", "javac")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	path, err := exec.LookPath("javac")
	if err != nil {
		return "", fmt.Errorf("javac not found: set JAVA_HOME or ensure javac is in PATH")
	}
	return path, nil
}

var diagnosticPattern = regexp.MustCompile(`^(.+):(\d+): (error|warning|note): \[(\w+)](.*)$`)

func parseDiagnostics(stderr string) []finding {
	findings := make([]finding, 0)
	for line := range strings.SplitSeq(stderr, "\n") {
		match := diagnosticPattern.FindStringSubmatch(line)
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

func toSARIF(findings []finding) sarifLog {
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
			Results: results,
		}},
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

	for _, path := range files {
		if _, writeErr := fmt.Fprintln(listFile, path); writeErr != nil {
			return "", fmt.Errorf("write file list: %w", writeErr)
		}
	}
	return listFile.Name(), nil
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
