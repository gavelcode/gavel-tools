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
	exitCodeMisuse = 2
	dirPermission  = 0o755
	expectedParts  = 2
)

func main() { os.Exit(execute()) }

func execute() int {
	config := flag.String("config", "", "Path to architecture.yml")
	outputPath := flag.String("out", "", "SARIF output path")
	flag.Parse()

	if *config == "" {
		fmt.Fprintln(os.Stderr, "missing --config")
		return exitCodeMisuse
	}
	if *outputPath == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return exitCodeMisuse
	}

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing Python source files")
		return exitCodeMisuse
	}

	if err := run(*config, *outputPath, files); err != nil {
		fmt.Fprintf(os.Stderr, "run python archtest: %v\n", err)
		return 1
	}
	return 0
}

func run(configPath, outputPath string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	config, err := archtest.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	var allViolations []archtest.Violation
	var failures []string
	for _, sourceFile := range files {
		violations, err := evaluateFile(sourceFile, config)
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
	if err := archtest.WriteSARIFWithInvocation(outputPath, "python_archtest", allViolations, invocation); err != nil {
		return fmt.Errorf("write sarif: %w", err)
	}
	return nil
}

func evaluateFile(sourceFile string, config archtest.Config) ([]archtest.Violation, error) {
	pkgDir := filepath.Dir(sourceFile)
	layer := archtest.MatchLayer(pkgDir, config.Layers)
	if layer == "" {
		return nil, nil
	}

	imports, err := parsePythonImports(sourceFile)
	if err != nil {
		return nil, err
	}

	return archtest.Evaluate(sourceFile, layer, imports, config.Layers, config.Rules), nil
}

func parsePythonImports(filePath string) (_ []archtest.Import, err error) {
	sourceFile, openErr := os.Open(filePath)
	if openErr != nil {
		return nil, fmt.Errorf("open %s: %w", filePath, openErr)
	}
	defer func() {
		if closeErr := sourceFile.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	fileDir := filepath.Dir(filePath)
	var imports []archtest.Import
	scanner := bufio.NewScanner(sourceFile)
	lineNum := 0
	inTripleQuote := false
	tripleQuoteChar := ""
	inParenImport := false

	for scanner.Scan() {
		lineNum++
		lineText := scanner.Text()
		if advanceTripleQuote(lineText, &inTripleQuote, &tripleQuoteChar) {
			continue
		}
		trimmed := strings.TrimSpace(stripInlineComment(lineText))
		collectPythonImport(trimmed, fileDir, lineNum, &inParenImport, &imports)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", filePath, err)
	}
	return imports, nil
}

// collectPythonImport records an `import`/`from` statement, tracking whether the
// parser is inside a parenthesised multi-line import across calls.
func collectPythonImport(trimmed, fileDir string, lineNum int, inParenImport *bool, imports *[]archtest.Import) {
	if *inParenImport {
		if strings.Contains(trimmed, ")") {
			*inParenImport = false
		}
		return
	}
	if trimmed == "" {
		return
	}
	if strings.HasPrefix(trimmed, "from ") {
		*inParenImport = handlePythonFrom(trimmed, fileDir, lineNum, imports)
		return
	}
	if strings.HasPrefix(trimmed, "import ") {
		*imports = append(*imports, parseImportStatement(trimmed, lineNum)...)
	}
}

// advanceTripleQuote tracks Python triple-quoted string state, reporting whether
// the line is inside (or opens/closes) one and should be skipped.
func advanceTripleQuote(lineText string, inTripleQuote *bool, tripleQuoteChar *string) bool {
	if *inTripleQuote {
		if containsTripleQuote(lineText, *tripleQuoteChar) {
			*inTripleQuote = false
			*tripleQuoteChar = ""
		}
		return true
	}
	if tripleQuote := detectTripleQuote(lineText); tripleQuote != "" {
		if strings.Count(lineText, tripleQuote)%2 != 0 {
			*inTripleQuote = true
			*tripleQuoteChar = tripleQuote
		}
		return true
	}
	return false
}

// handlePythonFrom records a `from … import …` and reports whether the import
// continues across lines (open parenthesis or backslash).
func handlePythonFrom(trimmed, fileDir string, lineNum int, imports *[]archtest.Import) bool {
	importPath, continuation := parseFromImport(trimmed, fileDir, lineNum)
	if importPath.Path != "" {
		*imports = append(*imports, importPath)
	}
	return continuation || strings.HasSuffix(trimmed, "\\")
}

func parseFromImport(lineText, fileDir string, lineNum int) (archtest.Import, bool) {
	rest := strings.TrimPrefix(lineText, "from ")
	parts := strings.SplitN(rest, " import ", expectedParts)
	if len(parts) < expectedParts {
		return archtest.Import{}, false
	}

	module := strings.TrimSpace(parts[0])
	hasParen := strings.Contains(parts[1], "(") && !strings.Contains(parts[1], ")")

	path := resolveModulePath(module, fileDir)
	return archtest.Import{Path: path, Line: lineNum}, hasParen
}

func parseImportStatement(lineText string, lineNum int) []archtest.Import {
	rest := strings.TrimPrefix(lineText, "import ")
	rest = strings.TrimSuffix(rest, "\\")
	rest = strings.TrimSpace(rest)

	var imports []archtest.Import
	for part := range strings.SplitSeq(rest, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		name, _, _ := strings.Cut(part, " as ")
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		path := dotsToSlashes(name)
		imports = append(imports, archtest.Import{Path: path, Line: lineNum})
	}
	return imports
}

func resolveModulePath(module, fileDir string) string {
	if !strings.HasPrefix(module, ".") {
		return dotsToSlashes(module)
	}

	leadingDots := 0
	for _, ch := range module {
		if ch == '.' {
			leadingDots++
		} else {
			break
		}
	}

	remainder := module[leadingDots:]
	baseDir := fileDir
	for i := 1; i < leadingDots; i++ {
		baseDir = filepath.Dir(baseDir)
	}

	if remainder == "" {
		return filepath.ToSlash(baseDir)
	}

	suffix := dotsToSlashes(remainder)
	return filepath.ToSlash(filepath.Join(baseDir, suffix))
}

func dotsToSlashes(module string) string {
	return strings.ReplaceAll(module, ".", "/")
}

func stripInlineComment(lineText string) string {
	inString := false
	stringChar := byte(0)
	for offset := 0; offset < len(lineText); offset++ {
		character := lineText[offset]
		if inString {
			if character == '\\' {
				offset++
				continue
			}
			if character == stringChar {
				inString = false
			}
			continue
		}
		if character == '\'' || character == '"' {
			inString = true
			stringChar = character
			continue
		}
		if character == '#' {
			return lineText[:offset]
		}
	}
	return lineText
}

func detectTripleQuote(lineText string) string {
	trimmed := strings.TrimSpace(lineText)
	if strings.Contains(trimmed, `"""`) {
		return `"""`
	}
	if strings.Contains(trimmed, `'''`) {
		return `'''`
	}
	return ""
}

func containsTripleQuote(lineText, quote string) bool {
	return strings.Contains(lineText, quote)
}
