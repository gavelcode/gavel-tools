package converter

import (
	"flag"
	"fmt"
	"os"
)

const (
	exitCodeUsageError = 2
	filePermission     = 0o644
)

// Main runs the clippy-convert tool: it reads rustc JSON diagnostics from --in
// and writes SARIF to --out. args excludes the tool name; the return value is
// the process exit code.
func Main(args []string) int {
	fs := flag.NewFlagSet("clippy-convert", flag.ContinueOnError)
	inputPath := fs.String("in", "", "Input file (rustc JSON diagnostics)")
	outputPath := fs.String("out", "", "Output file (SARIF)")
	if err := fs.Parse(args); err != nil {
		return exitCodeUsageError
	}

	if *inputPath == "" || *outputPath == "" {
		fmt.Fprintln(os.Stderr, "usage: gavel-lint clippy-convert --in <diagnostics.json> --out <output.sarif>")
		return exitCodeUsageError
	}

	inputData, err := os.ReadFile(*inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read input: %v\n", err)
		return 1
	}

	sarif, err := Convert(inputData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "convert: %v\n", err)
		return 1
	}

	if err := os.WriteFile(*outputPath, sarif, filePermission); err != nil {
		fmt.Fprintf(os.Stderr, "write output: %v\n", err)
		return 1
	}
	return 0
}
