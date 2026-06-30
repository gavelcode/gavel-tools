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
	pythonFlag := flag.String("python", "", "Path to the python3 binary")
	sitePackages := flag.String("site-packages", "", "Path to Bandit site-packages directory")
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

	if err := run(*pythonFlag, *sitePackages, *outputPath, files); err != nil {
		fmt.Fprintf(os.Stderr, "run bandit: %v\n", err)
		return 1
	}
	return 0
}

func run(pythonPath, sitePackages, outputPath string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	var python3 string
	var err error
	if pythonPath != "" {
		python3 = pythonPath
	} else {
		python3, err = findPython()
		if err != nil {
			return err
		}
	}

	if sitePackages != "" {
		sitePackages = resolveBazelExternal(sitePackages)
	}

	arguments := buildArgs(outputPath, files)
	cmd := exec.Command(python3, arguments...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = buildEnv(sitePackages)
	if err := cmd.Run(); err != nil {
		return sarif.WriteFailed(outputPath, "bandit", fmt.Sprintf("bandit failed to run: %v", err))
	}
	return sarif.MarkSuccessful(outputPath)
}

func buildArgs(outputPath string, files []string) []string {
	arguments := []string{
		"-m", "bandit",
		"--format", "sarif",
		"--exit-zero",
		"--output", outputPath,
	}
	return append(arguments, files...)
}

func buildEnv(sitePackages string) []string {
	environment := os.Environ()
	if sitePackages == "" {
		return environment
	}

	result := make([]string, 0, len(environment)+1)
	for _, item := range environment {
		if strings.HasPrefix(item, "PYTHONPATH=") {
			continue
		}
		result = append(result, item)
	}
	return append(result, "PYTHONPATH="+sitePackages)
}

func findPython() (string, error) {
	if p, err := exec.LookPath("python3"); err == nil {
		return p, nil
	}
	candidates := []string{
		"/usr/local/bin/python3",
		"/opt/homebrew/bin/python3",
		"/usr/bin/python3",
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", errors.New("python3 not found: ensure python3 is in PATH")
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
