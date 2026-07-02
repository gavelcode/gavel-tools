package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gavelcode/gavel-tools/lint/lang/typescript/eslint/converter"
	"github.com/gavelcode/gavel-tools/lint/sarif"
)

const (
	expectedArgCount    = 2
	dirPermission       = 0o755
	filePermission      = 0o644
	envCapacityOverhead = 2
	storeParentDirName  = ".aspect_rules_js"
)

func main() { os.Exit(execute()) }

func execute() int {
	eslint := flag.String("eslint", "", "Path to the ESLint executable")
	outputPath := flag.String("out", "", "SARIF output path")
	config := flag.String("config", "", "Path to eslint.config.js")
	flag.Parse()

	if *outputPath == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return expectedArgCount
	}
	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing TypeScript source files")
		return expectedArgCount
	}

	if err := run(*eslint, *outputPath, *config, files); err != nil {
		fmt.Fprintf(os.Stderr, "run eslint: %v\n", err)
		return 1
	}
	return 0
}

func run(eslint, outputPath, config string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	eslintBin, err := resolveESLintBin(eslint)
	if err != nil {
		return err
	}

	absFiles := absolutePaths(files)
	outputPath = toAbsolutePath(outputPath)
	if config != "" {
		config = toAbsolutePath(config)
	}

	reportPath, cleanup, err := createReportFile()
	if err != nil {
		return err
	}
	defer cleanup()

	return runESLint(eslintBin, outputPath, config, reportPath, absFiles)
}

// resolveESLintBin locates the ESLint executable, falling back to PATH, then
// rewrites the path for the Bazel sandbox and repairs its pnpm store symlinks so
// ESLint can resolve its own dependencies.
func resolveESLintBin(eslint string) (string, error) {
	if eslint == "" {
		bin, err := exec.LookPath("eslint")
		if err != nil {
			return "", errors.New("eslint not found in PATH and --eslint was not provided")
		}
		eslint = bin
	}
	eslint = resolveBazelExternal(eslint)
	fixBrokenStoreLinks(eslint)
	return eslint, nil
}

func absolutePaths(files []string) []string {
	absFiles := make([]string, len(files))
	for index, filePath := range files {
		absFiles[index] = toAbsolutePath(filePath)
	}
	return absFiles
}

func toAbsolutePath(filePath string) string {
	if abs, err := filepath.Abs(filePath); err == nil {
		return abs
	}
	return filePath
}

// createReportFile reserves a temp file for ESLint's JSON output and returns a
// cleanup that removes it.
func createReportFile() (string, func(), error) {
	jsonReport, err := os.CreateTemp("", "gavel-eslint-*.json")
	if err != nil {
		return "", nil, fmt.Errorf("create report file: %w", err)
	}
	reportPath := jsonReport.Name()
	_ = jsonReport.Close()
	return reportPath, func() { _ = os.Remove(reportPath) }, nil
}

func runESLint(eslintBin, outputPath, config, reportPath string, files []string) error {
	arguments := buildArgs(reportPath, config, files)
	cmd := exec.Command(eslintBin, arguments...)
	var stderrBuf strings.Builder
	cmd.Stdout = os.Stdout
	cmd.Stderr = io.MultiWriter(os.Stderr, &stderrBuf)
	cmd.Env = eslintEnv(eslintBin)
	runErr := cmd.Run()
	if runErr != nil && !isLintExitCode(runErr) {
		reason := fmt.Sprintf("eslint failed to run: %v\n%s", runErr, stderrBuf.String())
		if isMisconfiguration(runErr) {
			reason = fmt.Sprintf("eslint configuration error (exit 2): %v\n%s", runErr, stderrBuf.String())
		}
		return sarif.WriteFailed(outputPath, "eslint", reason)
	}
	return convertReport(reportPath, outputPath)
}

// convertReport turns ESLint's JSON report into SARIF. The built-in `json`
// formatter is used (not the npm SARIF formatter, which does not resolve inside
// the sandbox), so conversion happens here.
func convertReport(reportPath, outputPath string) error {
	data, err := os.ReadFile(reportPath)
	if err != nil {
		return sarif.WriteFailed(outputPath, "eslint", fmt.Sprintf("read eslint report: %v", err))
	}
	sarifBytes, err := converter.Convert(data)
	if err != nil {
		return sarif.WriteFailed(outputPath, "eslint", fmt.Sprintf("convert eslint report: %v", err))
	}
	if err := os.WriteFile(outputPath, sarifBytes, filePermission); err != nil {
		return fmt.Errorf("write sarif: %w", err)
	}
	return nil
}

func buildArgs(reportPath, config string, files []string) []string {
	arguments := []string{
		"--format", "json",
		"--output-file", reportPath,
	}
	if config != "" {
		arguments = append(arguments, "--config", config)
	}
	return append(arguments, files...)
}

func isLintExitCode(err error) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode() == 1
	}
	return false
}

func isMisconfiguration(err error) bool {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode() == expectedArgCount
	}
	return false
}

// eslintEnv builds the environment for the sandboxed ESLint run. It keeps the
// rules_js node fs-patch on (JS_BINARY__PATCH_NODE_FS=1) so Node resolves the
// pnpm store through the runfiles tree instead of following symlinks out to the
// raw output tree, where the store layout is incomplete; with the patch off,
// ESLint cannot load its own dependencies.
func eslintEnv(eslintBin string) []string {
	environment := make([]string, 0, len(os.Environ())+envCapacityOverhead)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "JS_BINARY__PATCH_NODE_FS=") {
			continue
		}
		if strings.HasPrefix(entry, "NODE_PATH=") {
			continue
		}
		environment = append(environment, entry)
	}
	environment = append(environment, "JS_BINARY__PATCH_NODE_FS=1")
	binDir := filepath.Dir(eslintBin)
	toolDir := filepath.Dir(binDir)
	nodeModules := filepath.Join(toolDir, "node_modules")
	if abs, err := filepath.Abs(nodeModules); err == nil {
		nodeModules = abs
	}
	environment = append(environment, "NODE_PATH="+nodeModules)
	return environment
}

// fixBrokenStoreLinks reconstructs the pnpm `s/` store directory that rules_js
// leaves out of the materialized tree, without which every `../../s/<pkg>`
// package symlink dangles and ESLint cannot load its dependencies. It repairs
// the store next to the ESLint launcher plus every store found in its runfiles.
func fixBrokenStoreLinks(eslintBin string) {
	binDir := filepath.Dir(eslintBin)
	toolDir := filepath.Dir(binDir)
	fixStoreDir(filepath.Join(toolDir, "node_modules", storeParentDirName))

	for _, aspectDir := range findStoreDirs(eslintBin + ".runfiles") {
		fixStoreDir(aspectDir)
	}
}

// findStoreDirs walks a runfiles tree and returns every pnpm store directory it
// contains, wherever the tool's package sits. Locating the store by name rather
// than by a hard-coded path means a gavel-tools directory reorg cannot silently
// break store-link repair (an earlier `tools/` -> `lint/lang/` move did exactly
// that, leaving ESLint unable to resolve its own dependencies in the sandbox).
func findStoreDirs(root string) []string {
	if _, err := os.Stat(root); err != nil {
		return nil
	}
	var storeDirs []string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() && entry.Name() == storeParentDirName {
			storeDirs = append(storeDirs, path)
			return filepath.SkipDir
		}
		return nil
	})
	return storeDirs
}

func fixStoreDir(aspectDir string) {
	if _, err := os.Stat(aspectDir); err != nil {
		return
	}
	storeDir := filepath.Join(aspectDir, "s")
	if _, err := os.Stat(storeDir); err == nil {
		return
	}
	entries, err := os.ReadDir(aspectDir)
	if err != nil {
		return
	}
	_ = os.MkdirAll(storeDir, dirPermission)
	for _, e := range entries {
		if e.Name() == "s" || !e.IsDir() {
			continue
		}
		_ = os.Symlink(filepath.Join("..", e.Name()), filepath.Join(storeDir, e.Name()))
	}
}

func resolveBazelExternal(filePath string) string {
	if _, err := os.Stat(filePath); err == nil {
		return filePath
	}
	if strings.HasPrefix(filePath, "external/") {
		alternate := filepath.Join("..", "..", filePath)
		if _, err := os.Stat(alternate); err == nil {
			return alternate
		}
		matches, err := filepath.Glob(filepath.Join("..", "..", "external", "*"+strings.TrimPrefix(filePath, "external/")))
		if err == nil && len(matches) > 0 {
			return matches[0]
		}
	}
	return filePath
}
