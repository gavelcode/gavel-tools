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

	if err := run(*pythonFlag, *sitePackages, *out, files); err != nil {
		fmt.Fprintf(os.Stderr, "run bandit: %v\n", err)
		return 1
	}
	return 0
}

func run(pythonPath, sitePackages, out string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(out), dirPermission); err != nil {
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

	args := buildArgs(out, files)
	cmd := exec.Command(python3, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = buildEnv(sitePackages)
	if err := cmd.Run(); err != nil {
		return sarif.WriteFailed(out, "bandit", fmt.Sprintf("bandit failed to run: %v", err))
	}
	return sarif.MarkSuccessful(out)
}

func buildArgs(out string, files []string) []string {
	args := []string{
		"-m", "bandit",
		"--format", "sarif",
		"--exit-zero",
		"--output", out,
	}
	return append(args, files...)
}

func buildEnv(sitePackages string) []string {
	env := os.Environ()
	if sitePackages == "" {
		return env
	}

	result := make([]string, 0, len(env)+1)
	for _, item := range env {
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
