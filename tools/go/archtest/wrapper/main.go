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
	exitUsageError = 2
	dirPermission  = 0o755
)

func main() { os.Exit(execute()) }

func execute() int {
	config := flag.String("config", "", "Path to architecture.yml")
	out := flag.String("out", "", "SARIF output path")
	flag.Parse()

	if *config == "" {
		fmt.Fprintln(os.Stderr, "missing --config")
		return exitUsageError
	}
	if *out == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return exitUsageError
	}

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing Go source files")
		return exitUsageError
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

	modulePrefix := detectModulePrefix()

	var allViolations []archtest.Violation
	for _, file := range files {
		violations, err := evaluateFile(file, cfg, modulePrefix)
		if err != nil {
			return fmt.Errorf("evaluate %s: %w", file, err)
		}
		allViolations = append(allViolations, violations...)
	}

	if err := archtest.WriteSARIF(out, "gavel-archtest", allViolations); err != nil {
		return fmt.Errorf("write sarif: %w", err)
	}
	return nil
}

func evaluateFile(file string, cfg archtest.Config, modulePrefix string) ([]archtest.Violation, error) {
	pkgDir := filepath.Dir(file)
	layer := archtest.MatchLayer(pkgDir, cfg.Layers)
	if layer == "" {
		return nil, nil
	}

	imports, err := parseGoImports(file)
	if err != nil {
		return nil, err
	}

	return archtest.EvaluateWithModule(file, layer, imports, cfg.Layers, cfg.Rules, modulePrefix), nil
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
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filePath, err)
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	var imports []archtest.Import
	scanner := bufio.NewScanner(file)
	lineNum := 0
	inBlock := false
	inBlockComment := false

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

		trimmed = stripBlockComments(trimmed)

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

		if inBlock {
			if trimmed == ")" {
				inBlock = false
				continue
			}

			if imp, ok := extractImportPath(trimmed); ok {
				if imp != "C" {
					imports = append(imports, archtest.Import{Path: imp, Line: lineNum})
				}
			}
			continue
		}

		if strings.HasPrefix(trimmed, "import (") {
			inBlock = true
			continue
		}
		if trimmed == "import(" {
			inBlock = true
			continue
		}

		if rest, ok := strings.CutPrefix(trimmed, "import "); ok {
			if imp, ok := extractImportPath(rest); ok {
				if imp != "C" {
					imports = append(imports, archtest.Import{Path: imp, Line: lineNum})
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", filePath, err)
	}
	return imports, nil
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
