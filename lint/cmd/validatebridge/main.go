// Package validatebridge is the `gavel-lint validate-bridge` subcommand: it
// checks SARIF files against the bridge contract (docs/bridge.md) so a bridge
// author can verify their aspect's output locally.
package validatebridge

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/gavelcode/gavel-tools/lint/sarif"
)

const (
	exitViolations = 1
	exitUsageError = 2
)

// Main runs validate-bridge with args (the tool name already stripped) and
// returns the process exit code: 0 all files valid, 1 violations found, 2 usage
// or I/O error.
func Main(arguments []string) int {
	return run(arguments, os.Stdout, os.Stderr)
}

func run(files []string, stdout, stderr io.Writer) int {
	if len(files) == 0 {
		_, _ = fmt.Fprintln(stderr, "usage: gavel-lint validate-bridge FILE.sarif...")
		return exitUsageError
	}
	exitCode := 0
	for _, sarifPath := range files {
		data, err := os.ReadFile(resolve(sarifPath))
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "validate-bridge: %v\n", err)
			return exitUsageError
		}
		for _, problem := range sarif.ValidateBridge(data) {
			_, _ = fmt.Fprintf(stdout, "%s: %s\n", sarifPath, problem)
			exitCode = exitViolations
		}
	}
	return exitCode
}

// resolve anchors a relative path at the directory `bazel run` was invoked from,
// since bazel runs binaries inside their runfiles tree.
func resolve(file string) string {
	if workingDir := os.Getenv("BUILD_WORKING_DIRECTORY"); workingDir != "" && !filepath.IsAbs(file) {
		return filepath.Join(workingDir, file)
	}
	return file
}
