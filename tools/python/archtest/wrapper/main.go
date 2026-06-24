package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gavelcode/gavel-tools/tools/archtest"
)

const (
	exitCodeMisuse  = 2
	dirPermission   = 0o755
	expectedParts   = 2
)

func main() { os.Exit(execute()) }

func execute() int {
	config := flag.String("config", "", "Path to architecture.yml")
	out := flag.String("out", "", "SARIF output path")
	flag.Parse()

	if *config == "" {
		fmt.Fprintln(os.Stderr, "missing --config")
		return exitCodeMisuse
	}
	if *out == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return exitCodeMisuse
	}

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing Python source files")
		return exitCodeMisuse
	}

	if err := run(*config, *out, files); err != nil {
		fmt.Fprintf(os.Stderr, "run python archtest: %v\n", err)
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

	if err := archtest.WriteSARIF(out, "python_archtest", allViolations); err != nil {
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

	imports, err := parsePythonImports(file)
	if err != nil {
		return nil, err
	}

	return archtest.Evaluate(file, layer, imports, cfg.Layers, cfg.Rules), nil
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
	parenImportPath := ""
	parenImportLine := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()

		if inTripleQuote {
			if containsTripleQuote(line, tripleQuoteChar) {
				inTripleQuote = false
				tripleQuoteChar = ""
			}
			continue
		}

		if tq := detectTripleQuote(line); tq != "" {
			count := strings.Count(line, tq)
			if count%2 != 0 {
				inTripleQuote = true
				tripleQuoteChar = tq
			}
			continue
		}

		trimmed := stripInlineComment(line)
		trimmed = strings.TrimSpace(trimmed)

		if inParenImport {
			if strings.Contains(trimmed, ")") {
				inParenImport = false
			}
			continue
		}

		if trimmed == "" {
			continue
		}

		if strings.HasPrefix(trimmed, "from ") {
			imp, continuation := parseFromImport(trimmed, fileDir, lineNum)
			if imp.Path != "" {
				imports = append(imports, imp)
			}
			if continuation {
				inParenImport = true
				parenImportPath = imp.Path
				parenImportLine = lineNum
			}
			_ = parenImportPath
			_ = parenImportLine
			if !continuation && strings.HasSuffix(trimmed, "\\") {
				inParenImport = true
			}
			continue
		}

		if strings.HasPrefix(trimmed, "import ") {
			parsed := parseImportStatement(trimmed, lineNum)
			imports = append(imports, parsed...)
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", filePath, err)
	}
	return imports, nil
}

func parseFromImport(line, fileDir string, lineNum int) (archtest.Import, bool) {
	rest := strings.TrimPrefix(line, "from ")
	parts := strings.SplitN(rest, " import ", expectedParts)
	if len(parts) < expectedParts {
		return archtest.Import{}, false
	}

	module := strings.TrimSpace(parts[0])
	hasParen := strings.Contains(parts[1], "(") && !strings.Contains(parts[1], ")")

	path := resolveModulePath(module, fileDir)
	return archtest.Import{Path: path, Line: lineNum}, hasParen
}

func parseImportStatement(line string, lineNum int) []archtest.Import {
	rest := strings.TrimPrefix(line, "import ")
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

	dots := 0
	for _, ch := range module {
		if ch == '.' {
			dots++
		} else {
			break
		}
	}

	remainder := module[dots:]
	base := fileDir
	for i := 1; i < dots; i++ {
		base = filepath.Dir(base)
	}

	if remainder == "" {
		return filepath.ToSlash(base)
	}

	suffix := dotsToSlashes(remainder)
	return filepath.ToSlash(filepath.Join(base, suffix))
}

func dotsToSlashes(module string) string {
	return strings.ReplaceAll(module, ".", "/")
}

func stripInlineComment(line string) string {
	inString := false
	stringChar := byte(0)
	for offset := 0; offset < len(line); offset++ {
		char := line[offset]
		if inString {
			if char == '\\' {
				offset++
				continue
			}
			if char == stringChar {
				inString = false
			}
			continue
		}
		if char == '\'' || char == '"' {
			inString = true
			stringChar = char
			continue
		}
		if char == '#' {
			return line[:offset]
		}
	}
	return line
}

func detectTripleQuote(line string) string {
	trimmed := strings.TrimSpace(line)
	if strings.Contains(trimmed, `"""`) {
		return `"""`
	}
	if strings.Contains(trimmed, `'''`) {
		return `'''`
	}
	return ""
}

func containsTripleQuote(line, quote string) bool {
	return strings.Contains(line, quote)
}
