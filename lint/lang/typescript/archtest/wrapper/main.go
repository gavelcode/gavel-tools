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
)

const (
	expectedArgCount = 2
	dirPermission    = 0o755
)

var (
	esImportRe  = regexp.MustCompile(`^\s*import\s+(?:type\s+)?(?:\{[^}]*\}|[^{}\s]+)\s+from\s+['"]([^'"]+)['"]`)
	esImportOnlyRe = regexp.MustCompile(`^\s*import\s+['"]([^'"]+)['"]`)
	esImportFromPartialRe = regexp.MustCompile(`^\s*import\s+(?:type\s+)?(?:\{[^}]*)?$`)
	fromClauseRe = regexp.MustCompile(`.*}\s*from\s+['"]([^'"]+)['"]`)
	requireRe   = regexp.MustCompile(`require\(\s*['"]([^'"]+)['"]\s*\)`)
	dynamicImportRe = regexp.MustCompile(`import\(\s*['"]([^'"]+)['"]\s*\)`)
)

func main() { os.Exit(execute()) }

func execute() int {
	config := flag.String("config", "", "Path to architecture.yml")
	out := flag.String("out", "", "SARIF output path")
	flag.Parse()

	if *config == "" {
		fmt.Fprintln(os.Stderr, "missing --config")
		return expectedArgCount
	}
	if *out == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return expectedArgCount
	}

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing TypeScript source files")
		return expectedArgCount
	}

	if err := run(*config, *out, files); err != nil {
		fmt.Fprintf(os.Stderr, "run typescript archtest: %v\n", err)
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

	if err := archtest.WriteSARIF(out, "typescript_archtest", allViolations); err != nil {
		return fmt.Errorf("write sarif: %w", err)
	}
	return nil
}

func evaluateFile(file string, cfg archtest.Config) ([]archtest.Violation, error) {
	pkgDir := filepath.Dir(file)
	layer := archtest.MatchLayer(pkgDir, cfg.Layers)
	if layer == "" {
		return nil, nil
	}

	imports, err := parseTypeScriptImports(file)
	if err != nil {
		return nil, err
	}

	return archtest.Evaluate(file, layer, imports, cfg.Layers, cfg.Rules), nil
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

		trimmed = stripTSBlockComments(trimmed)

		if strings.HasPrefix(trimmed, "//") {
			continue
		}

		if strings.Contains(trimmed, "/*") && !strings.Contains(trimmed, "*/") {
			inBlockComment = true
			trimmed = strings.TrimSpace(trimmed[:strings.Index(trimmed, "/*")])
			if trimmed == "" {
				continue
			}
		}

		if inMultiLineImport {
			if m := fromClauseRe.FindStringSubmatch(trimmed); m != nil {
				inMultiLineImport = false
				if imp, ok := resolveRelativeImport(m[1], fileDir); ok {
					imports = append(imports, archtest.Import{Path: imp, Line: multiLineStartLine})
				}
			}
			continue
		}

		if m := esImportRe.FindStringSubmatch(trimmed); m != nil {
			if imp, ok := resolveRelativeImport(m[1], fileDir); ok {
				imports = append(imports, archtest.Import{Path: imp, Line: lineNum})
			}
			continue
		}

		if m := esImportOnlyRe.FindStringSubmatch(trimmed); m != nil {
			if imp, ok := resolveRelativeImport(m[1], fileDir); ok {
				imports = append(imports, archtest.Import{Path: imp, Line: lineNum})
			}
			continue
		}

		if esImportFromPartialRe.MatchString(trimmed) {
			inMultiLineImport = true
			multiLineStartLine = lineNum
			continue
		}

		if m := requireRe.FindStringSubmatch(trimmed); m != nil {
			if imp, ok := resolveRelativeImport(m[1], fileDir); ok {
				imports = append(imports, archtest.Import{Path: imp, Line: lineNum})
			}
			continue
		}

		if m := dynamicImportRe.FindStringSubmatch(trimmed); m != nil {
			if imp, ok := resolveRelativeImport(m[1], fileDir); ok {
				imports = append(imports, archtest.Import{Path: imp, Line: lineNum})
			}
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", filePath, err)
	}
	return imports, nil
}

func resolveRelativeImport(importPath, fileDir string) (string, bool) {
	if !strings.HasPrefix(importPath, ".") {
		return "", false
	}

	resolved := filepath.Join(fileDir, importPath)
	return filepath.ToSlash(resolved), true
}

func stripTSBlockComments(line string) string {
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
