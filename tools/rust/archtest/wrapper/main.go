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
	exitCodeUsageError = 2
	dirPermission      = 0o755
)

func main() { os.Exit(execute()) }

func execute() int {
	config := flag.String("config", "", "Path to architecture.yml")
	out := flag.String("out", "", "SARIF output path")
	flag.Parse()

	if *config == "" {
		fmt.Fprintln(os.Stderr, "missing --config")
		return exitCodeUsageError
	}
	if *out == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return exitCodeUsageError
	}

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing Rust source files")
		return exitCodeUsageError
	}

	if err := run(*config, *out, files); err != nil {
		fmt.Fprintf(os.Stderr, "run rust archtest: %v\n", err)
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

	if err := archtest.WriteSARIF(out, "rust_archtest", allViolations); err != nil {
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

	imports, err := parseRustImports(file)
	if err != nil {
		return nil, err
	}

	return archtest.Evaluate(file, layer, imports, cfg.Layers, cfg.Rules), nil
}

func parseRustImports(filePath string) (imports []archtest.Import, retErr error) {
	sourceFile, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", filePath, err)
	}
	defer func() {
		if closeErr := sourceFile.Close(); closeErr != nil && retErr == nil {
			retErr = fmt.Errorf("close %s: %w", filePath, closeErr)
		}
	}()

	fileDir := filepath.Dir(filePath)
	scanner := bufio.NewScanner(sourceFile)
	lineNum := 0
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

		trimmed = stripRustBlockComments(trimmed)

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

		if strings.HasPrefix(trimmed, "mod ") {
			continue
		}

		if !strings.HasPrefix(trimmed, "use ") {
			continue
		}

		parsed := parseUseStatement(trimmed, fileDir, lineNum)
		imports = append(imports, parsed...)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", filePath, err)
	}
	return imports, nil
}

func parseUseStatement(line, fileDir string, lineNum int) []archtest.Import {
	stmt := strings.TrimPrefix(line, "use ")
	stmt = strings.TrimSuffix(stmt, ";")
	stmt = strings.TrimSpace(stmt)

	if isStdlibUse(stmt) {
		return nil
	}

	if braceIdx := strings.Index(stmt, "::{"); braceIdx >= 0 {
		return parseNestedUse(stmt, braceIdx, fileDir, lineNum)
	}

	path := resolveRustPath(stmt, fileDir)
	if path == "" {
		return nil
	}

	return []archtest.Import{{Path: path, Line: lineNum}}
}

func parseNestedUse(stmt string, braceIdx int, fileDir string, lineNum int) []archtest.Import {
	prefix := stmt[:braceIdx]
	braceContent := stmt[braceIdx+3:]
	braceContent = strings.TrimSuffix(braceContent, "}")

	segments := strings.Split(braceContent, ",")
	var imports []archtest.Import
	for _, seg := range segments {
		seg = strings.TrimSpace(seg)
		if seg == "" || seg == "self" {
			continue
		}

		seg = stripLeadingColons(seg)
		if subBrace := strings.Index(seg, "::{"); subBrace >= 0 {
			seg = seg[:subBrace]
		}

		fullPath := prefix + "::" + seg
		path := resolveRustPath(fullPath, fileDir)
		if path == "" {
			continue
		}
		imports = append(imports, archtest.Import{Path: path, Line: lineNum})
	}

	if len(imports) == 0 {
		path := resolveRustPath(prefix, fileDir)
		if path != "" {
			return []archtest.Import{{Path: path, Line: lineNum}}
		}
	}

	return imports
}

func resolveRustPath(stmt, fileDir string) string {
	if modPath, ok := strings.CutPrefix(stmt, "crate::"); ok {
		modPath = trimToModule(modPath)
		return "src/" + strings.ReplaceAll(modPath, "::", "/")
	}

	if modPath, ok := strings.CutPrefix(stmt, "super::"); ok {
		modPath = trimToModule(modPath)
		parentDir := filepath.Dir(fileDir)
		resolved := filepath.Join(parentDir, strings.ReplaceAll(modPath, "::", "/"))
		return filepath.ToSlash(resolved)
	}

	if modPath, ok := strings.CutPrefix(stmt, "self::"); ok {
		modPath = trimToModule(modPath)
		resolved := filepath.Join(fileDir, strings.ReplaceAll(modPath, "::", "/"))
		return filepath.ToSlash(resolved)
	}

	return ""
}

func trimToModule(path string) string {
	parts := strings.Split(path, "::")
	if len(parts) <= 1 {
		return path
	}

	last := parts[len(parts)-1]
	if len(last) > 0 && last[0] >= 'A' && last[0] <= 'Z' {
		return strings.Join(parts[:len(parts)-1], "::")
	}

	return path
}

func isStdlibUse(stmt string) bool {
	return strings.HasPrefix(stmt, "std::") ||
		strings.HasPrefix(stmt, "core::") ||
		strings.HasPrefix(stmt, "alloc::")
}

func stripRustBlockComments(line string) string {
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

func stripLeadingColons(segment string) string {
	for strings.HasPrefix(segment, "::") {
		segment = segment[2:]
	}
	return segment
}
