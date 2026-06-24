package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gavelcode/gavel-tools/archtest"
)

const (
	javaSourceRoot = "src/main/java/"
	missingArgCode = 2
	dirPermission  = 0o755
)

func main() { os.Exit(execute()) }

func execute() int {
	config := flag.String("config", "", "Path to architecture.yml")
	out := flag.String("out", "", "SARIF output path")
	flag.Parse()

	if *config == "" {
		fmt.Fprintln(os.Stderr, "missing --config")
		return missingArgCode
	}
	if *out == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return missingArgCode
	}

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing Java source files")
		return missingArgCode
	}

	if err := run(*config, *out, files); err != nil {
		fmt.Fprintf(os.Stderr, "run archtest: %v\n", err)
		return 1
	}
	return 0
}

func run(configPath, out string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(out), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	cfg, err := archtest.LoadConfig(configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	var allViolations []archtest.Violation
	for _, file := range files {
		violations, err := evaluateFile(file, cfg)
		if err != nil {
			return fmt.Errorf("evaluate %s: %w", file, err)
		}
		allViolations = append(allViolations, violations...)
	}

	if err := archtest.WriteSARIF(out, "gavel-archtest-java", allViolations); err != nil {
		return fmt.Errorf("write sarif: %w", err)
	}
	return nil
}

func evaluateFile(file string, cfg archtest.Config) ([]archtest.Violation, error) {
	dirPath := filepath.Dir(file)
	layer := archtest.MatchLayer(dirPath, cfg.Layers)
	if layer == "" {
		return nil, nil
	}

	imports, err := parseJavaImports(file)
	if err != nil {
		return nil, err
	}

	resolved := resolveImportPaths(imports, file)

	return archtest.Evaluate(file, layer, resolved, cfg.Layers, cfg.Rules), nil
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
		line := scanner.Text()
		trimmed := strings.TrimSpace(line)

		if inBlockComment {
			if idx := strings.Index(trimmed, "*/"); idx >= 0 {
				inBlockComment = false
				trimmed = strings.TrimSpace(trimmed[idx+2:])
				if trimmed == "" {
					continue
				}
			} else {
				continue
			}
		}

		trimmed = stripInlineBlockComments(trimmed)

		if strings.HasPrefix(trimmed, "//") || trimmed == "" {
			continue
		}

		if strings.Contains(trimmed, "/*") {
			commentStart := strings.Index(trimmed, "/*")
			if !strings.Contains(trimmed[commentStart:], "*/") {
				inBlockComment = true
				trimmed = strings.TrimSpace(trimmed[:commentStart])
				if trimmed == "" {
					continue
				}
			}
		}

		if strings.HasPrefix(trimmed, "package ") {
			continue
		}

		if strings.HasPrefix(trimmed, "import ") {
			pastPreamble = true
			raw := strings.TrimPrefix(trimmed, "import ")
			raw = strings.TrimPrefix(raw, "static ")
			raw = strings.TrimSuffix(raw, ";")
			raw = strings.TrimSpace(raw)

			pkgPath := javaImportToPackagePath(raw)
			if pkgPath != "" {
				imports = append(imports, archtest.Import{Path: pkgPath, Line: lineNum})
			}
			continue
		}

		if pastPreamble || isJavaDeclaration(trimmed) {
			break
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", filePath, err)
	}

	return imports, nil
}

func isJavaDeclaration(line string) bool {
	declarationPrefixes := []string{
		"public ", "private ", "protected ",
		"class ", "interface ", "enum ", "record ",
		"abstract ", "final ", "@",
	}
	for _, prefix := range declarationPrefixes {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func stripInlineBlockComments(line string) string {
	for {
		start := strings.Index(line, "/*")
		if start < 0 {
			return line
		}
		end := strings.Index(line[start+2:], "*/")
		if end < 0 {
			return line
		}
		line = line[:start] + line[start+2+end+2:]
	}
}
