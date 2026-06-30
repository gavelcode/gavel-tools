package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gavelcode/gavel-tools/lint/archtest"
	"github.com/gavelcode/gavel-tools/lint/sarif"
)

const (
	expectedArgCount = 2
	dirPermission    = 0o755
)

var (
	esImportRe            = regexp.MustCompile(`^\s*import\s+(?:type\s+)?(?:\{[^}]*\}|[^{}\s]+)\s+from\s+['"]([^'"]+)['"]`)
	esImportOnlyRe        = regexp.MustCompile(`^\s*import\s+['"]([^'"]+)['"]`)
	esImportFromPartialRe = regexp.MustCompile(`^\s*import\s+(?:type\s+)?(?:\{[^}]*)?$`)
	fromClauseRe          = regexp.MustCompile(`.*}\s*from\s+['"]([^'"]+)['"]`)
	requireRe             = regexp.MustCompile(`require\(\s*['"]([^'"]+)['"]\s*\)`)
	dynamicImportRe       = regexp.MustCompile(`import\(\s*['"]([^'"]+)['"]\s*\)`)
)

func main() { os.Exit(execute()) }

func execute() int {
	config := flag.String("config", "", "Path to architecture.yml")
	outputPath := flag.String("out", "", "SARIF output path")
	flag.Parse()

	if *config == "" {
		fmt.Fprintln(os.Stderr, "missing --config")
		return expectedArgCount
	}
	if *outputPath == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return expectedArgCount
	}

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing TypeScript source files")
		return expectedArgCount
	}

	if err := run(*config, *outputPath, files); err != nil {
		fmt.Fprintf(os.Stderr, "run typescript archtest: %v\n", err)
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
	if err := archtest.WriteSARIFWithInvocation(outputPath, "typescript_archtest", allViolations, invocation); err != nil {
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

	imports, err := parseTypeScriptImports(sourceFile)
	if err != nil {
		return nil, err
	}

	return archtest.Evaluate(sourceFile, layer, imports, config.Layers, config.Rules), nil
}

func parseTypeScriptImports(filePath string) (_ []archtest.Import, err error) {
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
	inBlockComment := false
	inMultiLineImport := false
	multiLineStartLine := 0

	for scanner.Scan() {
		lineNum++
		trimmed, skip := cleanTSCommentLine(strings.TrimSpace(scanner.Text()), &inBlockComment)
		if skip {
			continue
		}
		if inMultiLineImport {
			if m := fromClauseRe.FindStringSubmatch(trimmed); m != nil {
				inMultiLineImport = false
				addTSImport(m[1], fileDir, multiLineStartLine, &imports)
			}
			continue
		}
		if esImportFromPartialRe.MatchString(trimmed) {
			inMultiLineImport = true
			multiLineStartLine = lineNum
			continue
		}
		matchTSImport(trimmed, fileDir, lineNum, &imports)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", filePath, err)
	}
	return imports, nil
}

// cleanTSCommentLine advances the block-comment state and strips comments,
// returning the code left on the line and whether the caller should skip it.
func cleanTSCommentLine(trimmed string, inBlockComment *bool) (string, bool) {
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
	trimmed = stripTSBlockComments(trimmed)
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

// matchTSImport records the import path from the first single-line import form
// (es import, require, dynamic import) that the line matches, if any.
func matchTSImport(trimmed, fileDir string, lineNum int, imports *[]archtest.Import) {
	for _, importRe := range []*regexp.Regexp{esImportRe, esImportOnlyRe, requireRe, dynamicImportRe} {
		if m := importRe.FindStringSubmatch(trimmed); m != nil {
			addTSImport(m[1], fileDir, lineNum, imports)
			return
		}
	}
}

func addTSImport(spec, fileDir string, lineNum int, imports *[]archtest.Import) {
	if imp, ok := resolveRelativeImport(spec, fileDir); ok {
		*imports = append(*imports, archtest.Import{Path: imp, Line: lineNum})
	}
}

func resolveRelativeImport(importPath, fileDir string) (string, bool) {
	if !strings.HasPrefix(importPath, ".") {
		return "", false
	}

	resolved := filepath.Join(fileDir, importPath)
	return filepath.ToSlash(resolved), true
}

func stripTSBlockComments(lineText string) string {
	for {
		start := strings.Index(lineText, "/*")
		if start < 0 {
			return lineText
		}
		end := strings.Index(lineText[start+2:], "*/")
		if end < 0 {
			return lineText
		}
		lineText = lineText[:start] + lineText[start+2+end+2:]
	}
}
