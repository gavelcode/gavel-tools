package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gavelcode/gavel-tools/lint/archtest"
	"github.com/gavelcode/gavel-tools/lint/sarif"
)

const (
	exitUsageError = 2
	dirPermission  = 0o755
)

func main() { os.Exit(execute()) }

func execute() int {
	config := flag.String("config", "", "Path to architecture.yml")
	outputFlag := flag.String("out", "", "SARIF output path")
	modulePrefix := flag.String("module-prefix", "", "Go module path used to strip import prefixes; falls back to go.mod when empty")
	flag.Parse()

	if *config == "" {
		fmt.Fprintln(os.Stderr, "missing --config")
		return exitUsageError
	}
	if *outputFlag == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return exitUsageError
	}

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing Go source files")
		return exitUsageError
	}

	if err := run(*config, *outputFlag, *modulePrefix, files); err != nil {
		fmt.Fprintf(os.Stderr, "run archtest: %v\n", err)
		return 1
	}
	return 0
}

func run(configPath, outputPath, modulePrefix string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	config, err := archtest.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if modulePrefix == "" {
		modulePrefix = detectModulePrefix()
	}

	var allViolations []archtest.Violation
	var failures []string
	for _, sourceFile := range files {
		violations, err := evaluateFile(sourceFile, config, modulePrefix)
		if err != nil {
			failures = append(failures, fmt.Sprintf("could not analyze %s: %v", sourceFile, err))
			continue
		}
		allViolations = append(allViolations, violations...)
	}

	invocation := sarif.Successful()
	if len(failures) > 0 {
		invocation = sarif.Failed(failures...)
	}
	if err := archtest.WriteSARIFWithInvocation(outputPath, "gavel-archtest", allViolations, invocation); err != nil {
		return fmt.Errorf("write sarif: %w", err)
	}
	return nil
}

func evaluateFile(sourceFile string, config archtest.Config, modulePrefix string) ([]archtest.Violation, error) {
	pkgDir := filepath.Dir(sourceFile)
	layer := archtest.MatchLayer(pkgDir, config.Layers)
	if layer == "" {
		return nil, nil
	}

	imports, err := parseGoImports(sourceFile)
	if err != nil {
		return nil, err
	}

	return archtest.EvaluateWithModule(sourceFile, layer, imports, config.Layers, config.Rules, modulePrefix), nil
}

func detectModulePrefix() string {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if mod, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(mod)
		}
	}
	return ""
}

func parseGoImports(filePath string) (_ []archtest.Import, err error) {
	sourceFile, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filePath, err)
	}
	defer func() {
		if closeErr := sourceFile.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	var imports []archtest.Import
	scanner := bufio.NewScanner(sourceFile)
	lineNum := 0
	inBlock := false
	inBlockComment := false

	for scanner.Scan() {
		lineNum++
		trimmed, skip := cleanCommentLine(strings.TrimSpace(scanner.Text()), &inBlockComment)
		if skip {
			continue
		}
		inBlock = appendGoImportLine(trimmed, lineNum, inBlock, &imports)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", filePath, err)
	}
	return imports, nil
}

// cleanCommentLine advances the block-comment state and strips comments,
// returning the code left on the line and whether the caller should skip it.
func cleanCommentLine(trimmed string, inBlockComment *bool) (string, bool) {
	if *inBlockComment {
		idx := strings.Index(trimmed, "*/")
		if idx < 0 {
			return "", true
		}
		*inBlockComment = false
		trimmed = strings.TrimSpace(trimmed[idx+2:])
		if trimmed == "" {
			return "", true
		}
	}
	trimmed = stripBlockComments(trimmed)
	if strings.HasPrefix(trimmed, "//") {
		return "", true
	}
	if strings.Contains(trimmed, "/*") && !strings.Contains(trimmed, "*/") {
		*inBlockComment = true
		trimmed = strings.TrimSpace(trimmed[:strings.Index(trimmed, "/*")])
		if trimmed == "" {
			return "", true
		}
	}
	return trimmed, false
}

// appendGoImportLine records any import on the line and returns whether the
// parser is inside an `import (` block after it.
func appendGoImportLine(trimmed string, lineNum int, inBlock bool, imports *[]archtest.Import) bool {
	if inBlock {
		if trimmed == ")" {
			return false
		}
		addGoImport(trimmed, lineNum, imports)
		return true
	}
	if strings.HasPrefix(trimmed, "import (") || trimmed == "import(" {
		return true
	}
	if rest, ok := strings.CutPrefix(trimmed, "import "); ok {
		addGoImport(rest, lineNum, imports)
	}
	return false
}

func addGoImport(source string, lineNum int, imports *[]archtest.Import) {
	if imp, ok := extractImportPath(source); ok && imp != "C" {
		*imports = append(*imports, archtest.Import{Path: imp, Line: lineNum})
	}
}

func extractImportPath(source string) (string, bool) {
	source = strings.TrimSpace(source)
	if source == "" {
		return "", false
	}

	quoteStart := strings.IndexByte(source, '"')
	if quoteStart < 0 {
		return "", false
	}
	quoteEnd := strings.IndexByte(source[quoteStart+1:], '"')
	if quoteEnd < 0 {
		return "", false
	}

	return source[quoteStart+1 : quoteStart+1+quoteEnd], true
}

func stripBlockComments(source string) string {
	for {
		start := strings.Index(source, "/*")
		if start < 0 {
			return source
		}
		end := strings.Index(source[start+2:], "*/")
		if end < 0 {
			return source
		}
		source = source[:start] + source[start+2+end+2:]
	}
}
