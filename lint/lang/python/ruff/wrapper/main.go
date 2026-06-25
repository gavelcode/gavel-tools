package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	exitCodeMisuse = 2
	dirPermission  = 0o755
)

func main() { os.Exit(execute()) }

func execute() int {
	ruff := flag.String("ruff", "", "Path to the pinned Ruff executable")
	out := flag.String("out", "", "SARIF output path")
	flag.Parse()

	if *out == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return exitCodeMisuse
	}
	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing Python source files")
		return exitCodeMisuse
	}

	if err := run(*ruff, *out, files); err != nil {
		fmt.Fprintf(os.Stderr, "run ruff: %v\n", err)
		return 1
	}
	return 0
}

func run(ruff, out string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(out), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	if ruff == "" {
		bin, err := exec.LookPath("ruff")
		if err != nil {
			return errors.New("ruff not found in PATH and --ruff was not provided")
		}
		ruff = bin
	}
	ruff = resolveBazelExternal(ruff)

	args := buildArgs(out, files)
	cmd := exec.Command(ruff, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v: %w", ruff, args, err)
	}
	return nil
}

func buildArgs(out string, files []string) []string {
	args := []string{
		"check",
		"--output-format=sarif",
		"--no-fix",
		"--exit-zero",
		"--output-file=" + out,
	}
	return append(args, files...)
}

func resolveBazelExternal(path string) string {
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if suffix, ok := strings.CutPrefix(path, "external/"); ok {
		alternate := filepath.Join("..", "..", path)
		if _, err := os.Stat(alternate); err == nil {
			return alternate
		}
		matches, err := filepath.Glob(filepath.Join("..", "..", "external", "*"+suffix))
		if err == nil && len(matches) > 0 {
			return matches[0]
		}
	}
	return path
}
