// Command gavel-lint is the single prebuilt helper binary behind every gavel
// lint aspect: `gavel-lint <tool> [args...]`. One multi-call binary instead of a
// binary per wrapper means a consumer downloads (or builds) exactly one artifact
// per platform. See docs/tier-model.md.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	goarchtest "github.com/gavelcode/gavel-tools/lint/lang/go/archtest/wrapper"
	packagesdriver "github.com/gavelcode/gavel-tools/lint/lang/go/golangci_lint/packagesdriver"
	golangci "github.com/gavelcode/gavel-tools/lint/lang/go/golangci_lint/wrapper"
	javaarchtest "github.com/gavelcode/gavel-tools/lint/lang/java/archtest/wrapper"
	cpd "github.com/gavelcode/gavel-tools/lint/lang/java/cpd/wrapper"
	errorprone "github.com/gavelcode/gavel-tools/lint/lang/java/error_prone/wrapper"
	pmd "github.com/gavelcode/gavel-tools/lint/lang/java/pmd/wrapper"
	spotbugs "github.com/gavelcode/gavel-tools/lint/lang/java/spotbugs/wrapper"
	pyarchtest "github.com/gavelcode/gavel-tools/lint/lang/python/archtest/wrapper"
	bandit "github.com/gavelcode/gavel-tools/lint/lang/python/bandit/wrapper"
	pycompile "github.com/gavelcode/gavel-tools/lint/lang/python/pycompile/wrapper"
	ruff "github.com/gavelcode/gavel-tools/lint/lang/python/ruff/wrapper"
	rustarchtest "github.com/gavelcode/gavel-tools/lint/lang/rust/archtest/wrapper"
	clippy "github.com/gavelcode/gavel-tools/lint/lang/rust/clippy/converter"
	tsarchtest "github.com/gavelcode/gavel-tools/lint/lang/typescript/archtest/wrapper"
	eslint "github.com/gavelcode/gavel-tools/lint/lang/typescript/eslint/wrapper"
)

const exitUsageError = 2

// packagesDriverLauncher is the commandLine[0] under which the golangci wrapper
// re-executes this binary as a GOPACKAGESDRIVER (a driver cannot take a
// subcommand argument).
const packagesDriverLauncher = golangci.DriverLauncherName

// tools maps each subcommand to its entry point. Entry points take the
// arguments after the tool name and return the process exit code.
var tools = map[string]func([]string) int{
	"bandit":              bandit.Main,
	"clippy-convert":      clippy.Main,
	"cpd":                 cpd.Main,
	"error-prone":         errorprone.Main,
	"eslint":              eslint.Main,
	"go-archtest":         goarchtest.Main,
	"golangci":            golangci.Main,
	"java-archtest":       javaarchtest.Main,
	"packagesdriver":      packagesdriver.Main,
	"pmd":                 pmd.Main,
	"pycompile":           pycompile.Main,
	"python-archtest":     pyarchtest.Main,
	"ruff":                ruff.Main,
	"rust-archtest":       rustarchtest.Main,
	"spotbugs":            spotbugs.Main,
	"typescript-archtest": tsarchtest.Main,
}

func main() { os.Exit(dispatch(os.Args, os.Stderr)) }

func dispatch(commandLine []string, stderr io.Writer) int {
	if len(commandLine) > 0 && filepath.Base(commandLine[0]) == packagesDriverLauncher {
		return tools["packagesdriver"](commandLine[1:])
	}
	if len(commandLine) < 2 {
		_, _ = fmt.Fprint(stderr, usage())
		return exitUsageError
	}
	run, ok := tools[commandLine[1]]
	if !ok {
		_, _ = fmt.Fprintf(stderr, "gavel-lint: unknown tool %q\n%s", commandLine[1], usage())
		return exitUsageError
	}
	return run(commandLine[2:])
}

func usage() string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return "usage: gavel-lint <tool> [args...]\ntools: " + strings.Join(names, ", ") + "\n"
}
