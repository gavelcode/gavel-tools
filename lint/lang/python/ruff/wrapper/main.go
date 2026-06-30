package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/gavelcode/gavel-tools/lint/sarif"
)

const (
	exitCodeMisuse = 2
	dirPermission  = 0o755
)

func main() { os.Exit(execute()) }

func execute() int {
	ruffPath := flag.String("ruff", "", "Path to the pinned Ruff executable")
	outputPath := flag.String("out", "", "SARIF output path")
	flag.Parse()

	if *outputPath == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return exitCodeMisuse
	}
	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing Python source files")
		return exitCodeMisuse
	}

	if err := run(*ruffPath, *outputPath, files); err != nil {
		fmt.Fprintf(os.Stderr, "run ruff: %v\n", err)
		return 1
	}
	return 0
}

func run(ruffPath, outputPath string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	if ruffPath == "" {
		bin, err := exec.LookPath("ruff")
		if err != nil {
			return errors.New("ruff not found in PATH and --ruff was not provided")
		}
		ruffPath = bin
	}
	ruffPath = resolveBazelExternal(ruffPath)

	arguments := buildArgs(outputPath, files)
	cmd := exec.Command(ruffPath, arguments...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return sarif.WriteFailed(outputPath, "ruff", fmt.Sprintf("ruff failed to run: %v", err))
	}
	return sarif.MarkSuccessful(outputPath)
}

func buildArgs(outputPath string, files []string) []string {
	arguments := []string{
		"check",
		"--output-format=sarif",
		"--no-fix",
		"--exit-zero",
		"--output-file=" + outputPath,
	}
	return append(arguments, files...)
}

func resolveBazelExternal(filePath string) string {
	if _, err := os.Stat(filePath); err == nil {
		return filePath
	}
	if suffix, ok := strings.CutPrefix(filePath, "external/"); ok {
		alternate := filepath.Join("..", "..", filePath)
		if _, err := os.Stat(alternate); err == nil {
			return alternate
		}
		matches, err := filepath.Glob(filepath.Join("..", "..", "external", "*"+suffix))
		if err == nil && len(matches) > 0 {
			return matches[0]
		}
	}
	return filePath
}
