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
	pmdPath := flag.String("pmd", "", "Path to the pinned PMD executable")
	outputPath := flag.String("out", "", "SARIF output path")
	flag.Parse()

	if *outputPath == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return missingArgCode
	}
	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing Java source files")
		return missingArgCode
	}

	if err := run(*pmdPath, *outputPath, files); err != nil {
		fmt.Fprintf(os.Stderr, "run pmd: %v\n", err)
		return 1
	}
	return 0
}

func run(pmdPath, outputPath string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	if pmdPath == "" {
		bin, err := exec.LookPath("pmd")
		if err != nil {
			return errors.New("pmd not found in PATH and --pmd was not provided")
		}
		pmdPath = bin
	}
	pmdPath = resolveBazelExternal(pmdPath)

	fileList, err := writeFileList(files)
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(fileList) }()

	arguments := []string{
		"check",
		"--no-cache",
		"--no-fail-on-violation",
		"--format=sarif",
		"--report-file=" + outputPath,
		"--rulesets=category/java/bestpractices.xml,category/java/codestyle.xml,category/java/design.xml,category/java/documentation.xml,category/java/errorprone.xml,category/java/multithreading.xml,category/java/performance.xml,category/java/security.xml",
		"--file-list=" + fileList,
	}
	cmd := exec.Command(pmdPath, arguments...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = commandEnv()
	if err := cmd.Run(); err != nil {
		return sarif.WriteFailed(outputPath, "PMD", fmt.Sprintf("PMD failed to run: %v", err))
	}
	return sarif.MarkSuccessful(outputPath)
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

	for _, filePath := range files {
		if _, writeErr := fmt.Fprintln(listFile, filePath); writeErr != nil {
			return "", fmt.Errorf("write file list: %w", writeErr)
		}
	}
	return listFile.Name(), nil
}

func commandEnv() []string {
	environment := sanitizedEnv()
	if _, ok := lookupEnv(environment, "JAVA_HOME"); ok {
		return environment
	}

	javaHome := os.Getenv("JAVA_HOME")
	if javaHome != "" {
		return append(environment, "JAVA_HOME="+javaHome)
	}
	return environment
}

func sanitizedEnv() []string {
	environment := make([]string, 0, len(os.Environ()))
	for _, item := range os.Environ() {
		if strings.HasPrefix(item, "JAVA_HOME=") {
			continue
		}
		environment = append(environment, item)
	}
	return environment
}

func lookupEnv(environment []string, key string) (string, bool) {
	prefix := key + "="
	for _, item := range environment {
		if val, ok := strings.CutPrefix(item, prefix); ok {
			return val, true
		}
	}
	return "", false
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
