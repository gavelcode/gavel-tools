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
	javaSourceRoot = "src/main/java/"
	missingArgCode = 2
	dirPermission  = 0o755
)

func main() { os.Exit(execute()) }

func execute() int {
	config := flag.String("config", "", "Path to architecture.yml")
	outputPath := flag.String("out", "", "SARIF output path")
	flag.Parse()

	if *config == "" {
		fmt.Fprintln(os.Stderr, "missing --config")
		return missingArgCode
	}
	if *outputPath == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return missingArgCode
	}

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing Java source files")
		return missingArgCode
	}

	if err := run(*config, *outputPath, files); err != nil {
		fmt.Fprintf(os.Stderr, "run archtest: %v\n", err)
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
	if err := archtest.WriteSARIFWithInvocation(outputPath, "gavel-archtest-java", allViolations, invocation); err != nil {
		return fmt.Errorf("write sarif: %w", err)
	}
	return nil
}

func evaluateFile(sourceFile string, config archtest.Config) ([]archtest.Violation, error) {
	dirPath := filepath.Dir(sourceFile)
	layer := archtest.MatchLayer(dirPath, config.Layers)
	if layer == "" {
		return nil, nil
	}

	imports, err := parseJavaImports(sourceFile)
	if err != nil {
		return nil, err
	}

	resolved := resolveImportPaths(imports, sourceFile)

	return archtest.Evaluate(sourceFile, layer, resolved, config.Layers, config.Rules), nil
}

func resolveImportPaths(imports []archtest.Import, sourceFile string) []archtest.Import {
	prefix := extractSourcePrefix(sourceFile)

	resolved := make([]archtest.Import, 0, len(imports))
	for _, imp := range imports {
		resolved = append(resolved, archtest.Import{
			Path: prefix + imp.Path,
			Line: imp.Line,
		})
	}
	return resolved
}

func extractSourcePrefix(sourceFile string) string {
	normalized := filepath.ToSlash(sourceFile)
	idx := strings.Index(normalized, javaSourceRoot)
	if idx < 0 {
		return ""
	}
	return normalized[:idx+len(javaSourceRoot)]
}

func javaImportToPackagePath(importStmt string) string {
	importStmt = strings.TrimSuffix(importStmt, ";")
	importStmt = strings.TrimSpace(importStmt)

	if pkg, ok := strings.CutSuffix(importStmt, ".*"); ok {
		return strings.ReplaceAll(pkg, ".", "/")
	}

	lastDot := strings.LastIndex(importStmt, ".")
	if lastDot < 0 {
		return importStmt
	}

	packagePart := importStmt[:lastDot]
	return strings.ReplaceAll(packagePart, ".", "/")
}

func parseJavaImports(filePath string) (_ []archtest.Import, err error) {
	sourceFile, openErr := os.Open(filePath)
	if openErr != nil {
		return nil, fmt.Errorf("open %s: %w", filePath, openErr)
	}
	defer func() {
		if closeErr := sourceFile.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	var imports []archtest.Import
	scanner := bufio.NewScanner(sourceFile)
	lineNum := 0
	inBlockComment := false
	pastPreamble := false

	for scanner.Scan() {
		lineNum++
		trimmed, skip := cleanJavaCommentLine(strings.TrimSpace(scanner.Text()), &inBlockComment)
		if skip {
			continue
		}
		if collectJavaImport(trimmed, lineNum, &pastPreamble, &imports) {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", filePath, err)
	}

	return imports, nil
}

// collectJavaImport records an import line and reports whether the preamble has
// ended (a type declaration or a body line after the imports), so the caller
// stops scanning.
func collectJavaImport(trimmed string, lineNum int, pastPreamble *bool, imports *[]archtest.Import) bool {
	if strings.HasPrefix(trimmed, "package ") {
		return false
	}
	if strings.HasPrefix(trimmed, "import ") {
		*pastPreamble = true
		addJavaImport(trimmed, lineNum, imports)
		return false
	}
	return *pastPreamble || isJavaDeclaration(trimmed)
}

// cleanJavaCommentLine advances the block-comment state and strips comments,
// returning the code left on the line and whether the caller should skip it.
func cleanJavaCommentLine(trimmed string, inBlockComment *bool) (string, bool) {
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
	trimmed = stripInlineBlockComments(trimmed)
	if strings.HasPrefix(trimmed, "//") || trimmed == "" {
		return "", true
	}
	if commentStart := strings.Index(trimmed, "/*"); commentStart >= 0 && !strings.Contains(trimmed[commentStart:], "*/") {
		*inBlockComment = true
		trimmed = strings.TrimSpace(trimmed[:commentStart])
		if trimmed == "" {
			return "", true
		}
	}
	return trimmed, false
}

func addJavaImport(trimmed string, lineNum int, imports *[]archtest.Import) {
	raw := strings.TrimPrefix(trimmed, "import ")
	raw = strings.TrimPrefix(raw, "static ")
	raw = strings.TrimSuffix(raw, ";")
	raw = strings.TrimSpace(raw)
	if pkgPath := javaImportToPackagePath(raw); pkgPath != "" {
		*imports = append(*imports, archtest.Import{Path: pkgPath, Line: lineNum})
	}
}

func isJavaDeclaration(lineText string) bool {
	declarationPrefixes := []string{
		"public ", "private ", "protected ",
		"class ", "interface ", "enum ", "record ",
		"abstract ", "final ", "@",
	}
	for _, prefix := range declarationPrefixes {
		if strings.HasPrefix(lineText, prefix) {
			return true
		}
	}
	return false
}

func stripInlineBlockComments(lineText string) string {
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
