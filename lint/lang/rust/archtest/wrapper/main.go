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
	exitCodeUsageError = 2
	dirPermission      = 0o755
)

func main() { os.Exit(execute()) }

func execute() int {
	config := flag.String("config", "", "Path to architecture.yml")
	outputPath := flag.String("out", "", "SARIF output path")
	flag.Parse()

	if *config == "" {
		fmt.Fprintln(os.Stderr, "missing --config")
		return exitCodeUsageError
	}
	if *outputPath == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return exitCodeUsageError
	}

	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing Rust source files")
		return exitCodeUsageError
	}

	if err := run(*config, *outputPath, files); err != nil {
		fmt.Fprintf(os.Stderr, "run rust archtest: %v\n", err)
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
	if err := archtest.WriteSARIFWithInvocation(outputPath, "rust_archtest", allViolations, invocation); err != nil {
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

	imports, err := parseRustImports(sourceFile)
	if err != nil {
		return nil, err
	}

	return archtest.Evaluate(sourceFile, layer, imports, config.Layers, config.Rules), nil
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
		trimmed, skip := cleanRustCommentLine(strings.TrimSpace(scanner.Text()), &inBlockComment)
		if skip {
			continue
		}
		if strings.HasPrefix(trimmed, "mod ") {
			continue
		}
		if !strings.HasPrefix(trimmed, "use ") {
			continue
		}
		imports = append(imports, parseUseStatement(trimmed, fileDir, lineNum)...)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", filePath, err)
	}
	return imports, nil
}

// cleanRustCommentLine advances the block-comment state and strips comments,
// returning the code left on the line and whether the caller should skip it.
func cleanRustCommentLine(trimmed string, inBlockComment *bool) (string, bool) {
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
	trimmed = stripRustBlockComments(trimmed)
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

func parseUseStatement(lineText, fileDir string, lineNum int) []archtest.Import {
	statement := strings.TrimPrefix(lineText, "use ")
	statement = strings.TrimSuffix(statement, ";")
	statement = strings.TrimSpace(statement)

	if isStdlibUse(statement) {
		return nil
	}

	if braceIdx := strings.Index(statement, "::{"); braceIdx >= 0 {
		return parseNestedUse(statement, braceIdx, fileDir, lineNum)
	}

	filePath := resolveRustPath(statement, fileDir)
	if filePath == "" {
		return nil
	}

	return []archtest.Import{{Path: filePath, Line: lineNum}}
}

func parseNestedUse(statement string, braceIdx int, fileDir string, lineNum int) []archtest.Import {
	prefix := statement[:braceIdx]
	braceContent := statement[braceIdx+3:]
	braceContent = strings.TrimSuffix(braceContent, "}")

	segments := strings.Split(braceContent, ",")
	var imports []archtest.Import
	for _, segment := range segments {
		segment = strings.TrimSpace(segment)
		if segment == "" || segment == "self" {
			continue
		}

		segment = stripLeadingColons(segment)
		if subBrace := strings.Index(segment, "::{"); subBrace >= 0 {
			segment = segment[:subBrace]
		}

		fullPath := prefix + "::" + segment
		filePath := resolveRustPath(fullPath, fileDir)
		if filePath == "" {
			continue
		}
		imports = append(imports, archtest.Import{Path: filePath, Line: lineNum})
	}

	if len(imports) == 0 {
		filePath := resolveRustPath(prefix, fileDir)
		if filePath != "" {
			return []archtest.Import{{Path: filePath, Line: lineNum}}
		}
	}

	return imports
}

func resolveRustPath(statement, fileDir string) string {
	if modPath, ok := strings.CutPrefix(statement, "crate::"); ok {
		modPath = trimToModule(modPath)
		return "src/" + strings.ReplaceAll(modPath, "::", "/")
	}

	if modPath, ok := strings.CutPrefix(statement, "super::"); ok {
		modPath = trimToModule(modPath)
		parentDir := filepath.Dir(fileDir)
		resolved := filepath.Join(parentDir, strings.ReplaceAll(modPath, "::", "/"))
		return filepath.ToSlash(resolved)
	}

	if modPath, ok := strings.CutPrefix(statement, "self::"); ok {
		modPath = trimToModule(modPath)
		resolved := filepath.Join(fileDir, strings.ReplaceAll(modPath, "::", "/"))
		return filepath.ToSlash(resolved)
	}

	return ""
}

func trimToModule(filePath string) string {
	parts := strings.Split(filePath, "::")
	if len(parts) <= 1 {
		return filePath
	}

	last := parts[len(parts)-1]
	if len(last) > 0 && last[0] >= 'A' && last[0] <= 'Z' {
		return strings.Join(parts[:len(parts)-1], "::")
	}

	return filePath
}

func isStdlibUse(statement string) bool {
	return strings.HasPrefix(statement, "std::") ||
		strings.HasPrefix(statement, "core::") ||
		strings.HasPrefix(statement, "alloc::")
}

func stripRustBlockComments(lineText string) string {
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

func stripLeadingColons(segment string) string {
	for strings.HasPrefix(segment, "::") {
		segment = segment[2:]
	}
	return segment
}
