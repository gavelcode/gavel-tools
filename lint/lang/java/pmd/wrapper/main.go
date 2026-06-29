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
	missingArgCode = 2
	dirPermission  = 0o755
)

func main() { os.Exit(execute()) }

func execute() int {
	pmd := flag.String("pmd", "", "Path to the pinned PMD executable")
	out := flag.String("out", "", "SARIF output path")
	flag.Parse()

	if *out == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return missingArgCode
	}
	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing Java source files")
		return missingArgCode
	}

	if err := run(*pmd, *out, files); err != nil {
		fmt.Fprintf(os.Stderr, "run pmd: %v\n", err)
		return 1
	}
	return 0
}

func run(pmd, out string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(out), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	if pmd == "" {
		bin, err := exec.LookPath("pmd")
		if err != nil {
			return errors.New("pmd not found in PATH and --pmd was not provided")
		}
		pmd = bin
	}
	pmd = resolveBazelExternal(pmd)

	fileList, err := writeFileList(files)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(fileList) }()

	args := []string{
		"check",
		"--no-cache",
		"--no-fail-on-violation",
		"--format=sarif",
		"--report-file=" + out,
		"--rulesets=category/java/bestpractices.xml,category/java/codestyle.xml,category/java/design.xml,category/java/documentation.xml,category/java/errorprone.xml,category/java/multithreading.xml,category/java/performance.xml,category/java/security.xml",
		"--file-list=" + fileList,
	}
	cmd := exec.Command(pmd, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = commandEnv()
	if err := cmd.Run(); err != nil {
		return sarif.WriteFailed(out, "PMD", fmt.Sprintf("PMD failed to run: %v", err))
	}
	return sarif.MarkSuccessful(out)
}

func writeFileList(files []string) (_ string, err error) {
	listFile, createErr := os.CreateTemp("", "gavel-pmd-files-*")
	if createErr != nil {
		return "", fmt.Errorf("create file list: %w", createErr)
	}
	defer func() {
		if closeErr := listFile.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	for _, path := range files {
		if _, writeErr := fmt.Fprintln(listFile, path); writeErr != nil {
			return "", fmt.Errorf("write file list: %w", writeErr)
		}
	}
	return listFile.Name(), nil
}

func commandEnv() []string {
	env := sanitizedEnv()
	if _, ok := lookupEnv(env, "JAVA_HOME"); ok {
		return env
	}

	javaHome := os.Getenv("JAVA_HOME")
	if javaHome != "" {
		return append(env, "JAVA_HOME="+javaHome)
	}
	return env
}

func sanitizedEnv() []string {
	env := make([]string, 0, len(os.Environ()))
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "JAVA_HOME=") {
			continue
		}
		env = append(env, item)
	}
	return env
}

func lookupEnv(env []string, key string) (string, bool) {
	prefix := key + "="
	for _, item := range env {
		if val, ok := strings.CutPrefix(item, prefix); ok {
			return val, true
		}
	}
	return "", false
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
