package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/gavelcode/gavel-tools/lint/lang/rust/clippy/converter"
)

const (
	exitCodeUsageError = 2
	filePermission     = 0o644
)

func main() {
	inputPath := flag.String("in", "", "Input file (rustc JSON diagnostics)")
	outputFlag := flag.String("out", "", "Output file (SARIF)")
	flag.Parse()

	if *inputPath == "" || *outputFlag == "" {
		fmt.Fprintln(os.Stderr, "usage: converter --in <diagnostics.json> --out <output.sarif>")
		os.Exit(exitCodeUsageError)
	}

	inputData, err := os.ReadFile(*inputPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "read input: %v\n", err)
		os.Exit(1)
	}

	sarif, err := converter.Convert(inputData)
	if err != nil {
		fmt.Fprintf(os.Stderr, "convert: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*outputFlag, sarif, filePermission); err != nil {
		fmt.Fprintf(os.Stderr, "write output: %v\n", err)
		os.Exit(1)
	}
}
