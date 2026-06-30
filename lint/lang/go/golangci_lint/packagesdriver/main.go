// Command packagesdriver is a static GOPACKAGESDRIVER for Bazel sandboxes.
//
// rules_go ships a gopackagesdriver that shells out to `bazel build` to
// materialize the package graph — unusable inside a sandboxed aspect action
// (nested Bazel) and, in practice, buggy for golangci-lint's relative `./pkg`
// patterns. This driver instead reads the package-graph JSON that
// go_pkg_info_aspect already produced as declared action inputs (a manifest of
// .pkg.json files), resolves the Bazel path placeholders against the exec root,
// and answers go/packages with zero Bazel calls, zero network, and zero host
// toolchain. That is what lets golangci-lint run fully hermetic.
//
// The FlatPackage schema and the import/test-split resolution mirror
// rules_go's go/tools/gopackagesdriver (Apache-2.0); they are reimplemented
// here over stdlib-only types so gavel-tools takes on no new dependency.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// buildContext applies the host platform's build constraints, exactly as
// rules_go's driver does. Bazel hands us every source file in a package; the
// ones whose GOOS/GOARCH suffix or //go:build line excludes the host must be
// dropped or the type checker sees duplicate symbols (e.g. uuid's node_js.go
// alongside node_net.go).
var buildContext = build.Default

const (
	execrootPlaceholder   = "__BAZEL_EXECROOT__"
	workspacePlaceholder  = "__BAZEL_WORKSPACE__"
	outputBasePlaceholder = "__BAZEL_OUTPUT_BASE__"
	manifestEnv           = "GAVEL_PKG_JSON_MANIFEST"
	filePrefix            = "file="
	patternPrefix         = "pattern="
	recursiveSuffix       = "/..."
	testSuffix            = "_test.go"
	// testMainFile is rules_go's generated go_test entrypoint. It imports
	// build-system internals (bzltestutil) that are not in the package graph and
	// carries unused imports, so linting it yields false positives — it must
	// never be analyzed, only the real test sources.
	testMainFile = "testmain.go"
)

// FlatPackage is the JSON shape shared by rules_go's pkg.json files and the
// go/packages driver response. Imports maps an import path to a package ID.
type FlatPackage struct {
	ID              string
	Name            string            `json:",omitempty"`
	PkgPath         string            `json:",omitempty"`
	GoFiles         []string          `json:",omitempty"`
	CompiledGoFiles []string          `json:",omitempty"`
	OtherFiles      []string          `json:",omitempty"`
	ExportFile      string            `json:",omitempty"`
	Imports         map[string]string `json:",omitempty"`
	Standard        bool              `json:",omitempty"`
}

type driverResponse struct {
	NotHandled bool
	Compiler   string
	Arch       string
	Roots      []string `json:",omitempty"`
	Packages   []*FlatPackage
}

func main() {
	execRoot, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	resp, err := run(os.Getenv(manifestEnv), execRoot, os.Args[1:])
	if err != nil {
		fail(err)
	}
	if err := json.NewEncoder(os.Stdout).Encode(resp); err != nil {
		fail(err)
	}
}

// fail prints the error but exits 0: go/packages falls back to `go list` on a
// non-zero driver exit, which would silently defeat hermeticity. A handled
// error surfaces as an empty response instead.
func fail(err error) {
	fmt.Fprintf(os.Stderr, "packagesdriver: %v\n", err)
	_ = json.NewEncoder(os.Stdout).Encode(&driverResponse{NotHandled: true})
	os.Exit(0)
}

func run(manifestPath, execRoot string, patterns []string) (*driverResponse, error) {
	if manifestPath == "" {
		return nil, fmt.Errorf("missing %s", manifestEnv)
	}
	jsonFiles, err := readManifest(manifestPath)
	if err != nil {
		return nil, err
	}

	pkgs, err := loadPackages(jsonFiles, execRoot)
	if err != nil {
		return nil, err
	}

	stdlibByPath := map[string]string{}
	for _, pkg := range pkgs {
		if pkg.Standard {
			stdlibByPath[pkg.PkgPath] = pkg.ID
		}
	}

	fset := token.NewFileSet()
	var extras []*FlatPackage
	for _, pkg := range pkgs {
		resolveImports(pkg, stdlibByPath, fset)
		if xtest := moveTestFiles(pkg, fset); xtest != nil {
			extras = append(extras, xtest)
		}
	}
	pkgs = append(pkgs, extras...)

	return &driverResponse{
		Compiler: "gc",
		Arch:     runtime.GOARCH,
		Roots:    matchRoots(patterns, pkgs, execRoot),
		Packages: pkgs,
	}, nil
}

func readManifest(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer f.Close()

	var files []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			files = append(files, line)
		}
	}
	return files, scanner.Err()
}

// loadPackages decodes every JSON file (each a stream of one or more
// FlatPackage objects) and rewrites Bazel path placeholders to absolute paths.
// rules_go emits two pkg.json under one ID for a go_test — the generated
// testmain and the real test sources — so same-ID packages are merged rather
// than dropped; otherwise the sources (the files we must lint) would be lost.
func loadPackages(jsonFiles []string, execRoot string) ([]*FlatPackage, error) {
	byID := map[string]*FlatPackage{}
	var pkgs []*FlatPackage
	for _, jf := range jsonFiles {
		f, err := os.Open(jf)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", jf, err)
		}
		dec := json.NewDecoder(f)
		for dec.More() {
			pkg := &FlatPackage{}
			if err := dec.Decode(pkg); err != nil {
				f.Close()
				return nil, fmt.Errorf("decode %s: %w", jf, err)
			}
			resolvePaths(pkg, execRoot)
			if existing, ok := byID[pkg.ID]; ok {
				mergePackage(existing, pkg)
				continue
			}
			byID[pkg.ID] = pkg
			pkgs = append(pkgs, pkg)
		}
		f.Close()
	}
	return pkgs, nil
}

// mergePackage folds src into dst, unioning file lists and imports. Used to
// reunite a go_test's testmain with its test sources under their shared ID.
func mergePackage(dst, src *FlatPackage) {
	dst.GoFiles = unionFiles(dst.GoFiles, src.GoFiles)
	dst.CompiledGoFiles = unionFiles(dst.CompiledGoFiles, src.CompiledGoFiles)
	dst.OtherFiles = unionFiles(dst.OtherFiles, src.OtherFiles)
	if dst.PkgPath == "" {
		dst.PkgPath = src.PkgPath
	}
	if dst.ExportFile == "" {
		dst.ExportFile = src.ExportFile
	}
	for k, v := range src.Imports {
		if dst.Imports == nil {
			dst.Imports = map[string]string{}
		}
		if _, ok := dst.Imports[k]; !ok {
			dst.Imports[k] = v
		}
	}
}

func unionFiles(a, b []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(a)+len(b))
	for _, f := range append(append([]string{}, a...), b...) {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

func resolvePaths(pkg *FlatPackage, execRoot string) {
	resolveSlice(pkg.GoFiles, execRoot)
	resolveSlice(pkg.CompiledGoFiles, execRoot)
	resolveSlice(pkg.OtherFiles, execRoot)
	pkg.ExportFile = resolvePath(pkg.ExportFile, execRoot)
	pkg.GoFiles = dropGenerated(pkg.GoFiles)
	pkg.CompiledGoFiles = dropGenerated(filterBuildTags(pkg.CompiledGoFiles))
}

func dropGenerated(files []string) []string {
	kept := make([]string, 0, len(files))
	for _, f := range files {
		if filepath.Base(f) != testMainFile {
			kept = append(kept, f)
		}
	}
	return kept
}

// filterBuildTags keeps only the files the host build context selects, plus
// extension-less and cgo-processed files, which MatchFile rejects but the
// type checker needs.
func filterBuildTags(files []string) []string {
	kept := make([]string, 0, len(files))
	for _, f := range files {
		dir, name := filepath.Split(f)
		match, _ := buildContext.MatchFile(dir, name)
		if match || filepath.Ext(f) == "" || isCgoProcessed(name) {
			kept = append(kept, f)
		}
	}
	return kept
}

func isCgoProcessed(name string) bool {
	return name == "_cgo_gotypes.go" || name == "_cgo_imports.go" || strings.HasSuffix(name, ".cgo1.go")
}

func resolveSlice(paths []string, execRoot string) {
	for i, p := range paths {
		paths[i] = resolvePath(p, execRoot)
	}
}

// resolvePath rewrites the three Bazel placeholders to the exec root. Inside a
// sandbox the workspace, exec root and external output base all live under the
// action's working directory, so all three collapse to execRoot.
func resolvePath(p, execRoot string) string {
	if p == "" {
		return ""
	}
	for _, placeholder := range []string{execrootPlaceholder, workspacePlaceholder, outputBasePlaceholder} {
		if strings.HasPrefix(p, placeholder) {
			return filepath.Join(execRoot, strings.TrimPrefix(p, placeholder))
		}
	}
	return p
}

// resolveImports parses each compiled file's import clauses and links stdlib
// imports, which rules_go omits from pkg.json (Bazel does not model them).
// Dependency type info comes from each package's ExportFile, so a file that
// cannot be parsed is skipped rather than failing the whole graph — only its
// stdlib edges are lost, which the type checker recovers from export data.
func resolveImports(pkg *FlatPackage, stdlibByPath map[string]string, fset *token.FileSet) {
	for _, file := range pkg.CompiledGoFiles {
		f, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			continue
		}
		if pkg.Name == "" {
			pkg.Name = f.Name.Name
		}
		for _, raw := range f.Imports {
			imp, err := strconv.Unquote(raw.Path.Value)
			if err != nil || imp == "C" {
				continue
			}
			if _, ok := pkg.Imports[imp]; ok {
				continue
			}
			if id, ok := stdlibByPath[imp]; ok {
				if pkg.Imports == nil {
					pkg.Imports = map[string]string{}
				}
				pkg.Imports[imp] = id
			}
		}
	}
}

// moveTestFiles splits external test files (package foo_test) into their own
// package, mirroring go/packages. gavel's tests are black-box, so without this
// golangci-lint would mis-attribute their findings.
func moveTestFiles(pkg *FlatPackage, fset *token.FileSet) *FlatPackage {
	internalGo, externalGo := splitExternalTests(pkg.Name, pkg.GoFiles, fset)
	internalCompiled, externalCompiled := splitExternalTests(pkg.Name, pkg.CompiledGoFiles, fset)
	if len(externalGo) == 0 && len(externalCompiled) == 0 {
		return nil
	}
	pkg.GoFiles = internalGo
	pkg.CompiledGoFiles = internalCompiled

	imports := map[string]string{}
	for k, v := range pkg.Imports {
		imports[k] = v
	}
	imports[pkg.PkgPath] = pkg.ID

	return &FlatPackage{
		ID:              pkg.ID + "_xtest",
		Name:            pkg.Name + "_test",
		PkgPath:         pkg.PkgPath + "_test",
		GoFiles:         externalGo,
		CompiledGoFiles: externalCompiled,
		OtherFiles:      pkg.OtherFiles,
		ExportFile:      pkg.ExportFile,
		Imports:         imports,
	}
}

func splitExternalTests(pkgName string, files []string, fset *token.FileSet) (internal, external []string) {
	for _, file := range files {
		if !strings.HasSuffix(file, testSuffix) {
			internal = append(internal, file)
			continue
		}
		f, err := parser.ParseFile(fset, file, nil, parser.PackageClauseOnly)
		if err == nil && f.Name.Name != pkgName {
			external = append(external, file)
		} else {
			internal = append(internal, file)
		}
	}
	return internal, external
}

// matchRoots maps the patterns go/packages passes (file= queries or directory
// patterns) to the package IDs whose sources satisfy them.
func matchRoots(patterns []string, pkgs []*FlatPackage, execRoot string) []string {
	var roots []string
	seen := map[string]bool{}
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			roots = append(roots, id)
		}
	}
	for _, pattern := range patterns {
		if file, ok := strings.CutPrefix(pattern, filePrefix); ok {
			matchFile(absFrom(file, execRoot), pkgs, add)
			continue
		}
		pattern = strings.TrimPrefix(pattern, patternPrefix)
		recursive := strings.HasSuffix(pattern, recursiveSuffix)
		dir := absFrom(strings.TrimSuffix(strings.TrimSuffix(pattern, recursiveSuffix), "/"), execRoot)
		matchDir(dir, recursive, pkgs, add)
	}
	return roots
}

func matchFile(file string, pkgs []*FlatPackage, add func(string)) {
	for _, pkg := range pkgs {
		for _, f := range append(append([]string{}, pkg.GoFiles...), pkg.CompiledGoFiles...) {
			if f == file {
				add(pkg.ID)
				return
			}
		}
	}
}

func matchDir(dir string, recursive bool, pkgs []*FlatPackage, add func(string)) {
	for _, pkg := range pkgs {
		if pkg.Standard {
			continue
		}
		for _, f := range pkg.CompiledGoFiles {
			fdir := filepath.Dir(f)
			if fdir == dir || (recursive && strings.HasPrefix(fdir, dir+string(filepath.Separator))) {
				add(pkg.ID)
				break
			}
		}
	}
}

func absFrom(p, execRoot string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(execRoot, strings.TrimPrefix(p, "./"))
}
