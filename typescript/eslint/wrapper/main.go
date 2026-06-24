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
	expectedArgCount    = 2
	dirPermission       = 0o755
	filePermission      = 0o644
	envCapacityOverhead = 2
)

func main() { os.Exit(execute()) }

func execute() int {
	eslint := flag.String("eslint", "", "Path to the ESLint executable")
	out := flag.String("out", "", "SARIF output path")
	config := flag.String("config", "", "Path to eslint.config.js")
	flag.Parse()

	if *out == "" {
		fmt.Fprintln(os.Stderr, "missing --out")
		return expectedArgCount
	}
	files := flag.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "missing TypeScript source files")
		return expectedArgCount
	}

	if err := run(*eslint, *out, *config, files); err != nil {
		fmt.Fprintf(os.Stderr, "run eslint: %v\n", err)
		return 1
	}
	return 0
}

func run(eslint, out, config string, files []string) error {
	if err := os.MkdirAll(filepath.Dir(out), dirPermission); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}

	if eslint == "" {
		bin, err := exec.LookPath("eslint")
		if err != nil {
			return errors.New("eslint not found in PATH and --eslint was not provided")
		}
		eslint = bin
	}
	eslint = resolveBazelExternal(eslint)

	fixBrokenStoreLinks(eslint)

	absFiles := make([]string, len(files))
	for i, f := range files {
		if abs, err := filepath.Abs(f); err == nil {
			absFiles[i] = abs
		} else {
			absFiles[i] = f
		}
	}

	absOut, err := filepath.Abs(out)
	if err == nil {
		out = absOut
	}

	args := buildArgs(out, config, absFiles)
	cmd := exec.Command(eslint, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = eslintEnv(eslint)
	err = cmd.Run()
	if err != nil && !isLintExitCode(err) {
		if isMisconfiguration(err) {
			return writeEmptySARIF(out)
		}
		return fmt.Errorf("%s %v: %w", eslint, args, err)
	}
	return nil
}

func buildArgs(out, config string, files []string) []string {
	args := []string{
		"--format", "@microsoft/eslint-formatter-sarif",
		"--output-file", out,
	}
	if config != "" {
		args = append(args, "--config", config)
	}
	return append(args, files...)
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

func writeEmptySARIF(path string) error {
	empty := `{"version":"2.1.0","$schema":"https://raw.githubusercontent.com/oasis-tcs/sarif-spec/main/sarif-2.1/schema/sarif-schema-2.1.0.json","runs":[]}`
	return os.WriteFile(path, []byte(empty), filePermission)
}

func eslintEnv(eslintBin string) []string {
	env := make([]string, 0, len(os.Environ())+envCapacityOverhead)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "JS_BINARY__PATCH_NODE_FS=") {
			continue
		}
		if strings.HasPrefix(entry, "NODE_PATH=") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env, "JS_BINARY__PATCH_NODE_FS=0")
	binDir := filepath.Dir(eslintBin)
	toolDir := filepath.Dir(binDir)
	nodeModules := filepath.Join(toolDir, "node_modules")
	if abs, err := filepath.Abs(nodeModules); err == nil {
		nodeModules = abs
	}
	env = append(env, "NODE_PATH="+nodeModules)
	return env
}

func fixBrokenStoreLinks(eslintBin string) {
	binDir := filepath.Dir(eslintBin)
	toolDir := filepath.Dir(binDir)
	fixStoreDir(filepath.Join(toolDir, "node_modules", ".aspect_rules_js"))

	runfilesDir := eslintBin + ".runfiles"
	if _, err := os.Stat(runfilesDir); err != nil {
		return
	}
	matches, _ := filepath.Glob(filepath.Join(runfilesDir, "*", "tools", "typescript", "eslint", "node_modules", ".aspect_rules_js"))
	for _, m := range matches {
		fixStoreDir(m)
	}
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

func resolveBazelExternal(path string) string {
	if _, err := os.Stat(path); err == nil {
		return path
	}
	if strings.HasPrefix(path, "external/") {
		alternate := filepath.Join("..", "..", path)
		if _, err := os.Stat(alternate); err == nil {
			return alternate
		}
		matches, err := filepath.Glob(filepath.Join("..", "..", "external", "*"+strings.TrimPrefix(path, "external/")))
		if err == nil && len(matches) > 0 {
			return matches[0]
		}
	}
	return path
}
