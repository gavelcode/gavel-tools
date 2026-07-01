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
	stdlibDirEnv          = "GAVEL_STDLIB_PKG_DIR"
	archiveSuffix         = ".a"
	filePrefix            = "file="
	patternPrefix         = "pattern="
	recursiveSuffix       = "/..."
	testSuffix            = "_test.go"
	// testMainFile is rules_go's generated go_test entrypoint. It imports
	// build-system internals (bzltestutil) that are not in the package graph and
	// carries unused imports, so linting it yields false positives — it must
	// never be analyzed, only the real test sources.
	testMainFile = "testmain.go"
	// bufio scanner limits for the manifest (one path per line).
	manifestBufferInitial = 64 * 1024
	manifestBufferMax     = 1024 * 1024
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
	resp, err := run(os.Getenv(manifestEnv), os.Getenv(stdlibDirEnv), execRoot, os.Args[1:])
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

func run(manifestPath, stdlibDir, execRoot string, patterns []string) (*driverResponse, error) {
	if manifestPath == "" {
		return nil, fmt.Errorf("missing %s", manifestEnv)
	}
	jsonFiles, err := readManifest(manifestPath)
	if err != nil {
		return nil, err
	}

	packages, err := loadPackages(jsonFiles, execRoot)
	if err != nil {
		return nil, err
	}

	assignStdlibExports(packages, absoluteStdlibDir(stdlibDir, execRoot))

	stdlibByPath := map[string]string{}
	for _, flatPackage := range packages {
		if flatPackage.Standard {
			stdlibByPath[flatPackage.PkgPath] = flatPackage.ID
		}
	}

	fileSet := token.NewFileSet()
	var extras []*FlatPackage
	for _, flatPackage := range packages {
		resolveImports(flatPackage, stdlibByPath, fileSet)
		if xtest := moveTestFiles(flatPackage, fileSet); xtest != nil {
			extras = append(extras, xtest)
		}
	}
	packages = append(packages, extras...)

	return &driverResponse{
		Compiler: "gc",
		Arch:     runtime.GOARCH,
		Roots:    matchRoots(patterns, packages, execRoot),
		Packages: packages,
	}, nil
}

// assignStdlibExports points each standard-library package at the compiled
// archive rules_go emits (stdlibDir/<PkgPath>.a) as its ExportFile. Without an
// explicit export, golangci-lint fails to load export data for anything that
// imports the stdlib ("no export data for <stdlib pkg>"). rules_go leaves the
// field empty by default; under export_stdlib it fills it with a gocache path,
// but that compiler-cache object is not the archive gcexportdata expects, so a
// package like internal/byteorder (reached via syscall) still fails to load.
// The .a is the reliable export data, so it is preferred over whatever the
// pkg.json carried. Non-standard packages, and stdlib packages with no archive
// on disk, are left untouched.
func assignStdlibExports(packages []*FlatPackage, stdlibDir string) {
	if stdlibDir == "" {
		return
	}
	for _, flatPackage := range packages {
		if !flatPackage.Standard {
			continue
		}
		archive := filepath.Join(stdlibDir, filepath.FromSlash(flatPackage.PkgPath)+archiveSuffix)
		if _, err := os.Stat(archive); err == nil {
			flatPackage.ExportFile = archive
		}
	}
}

func absoluteStdlibDir(stdlibDir, execRoot string) string {
	if stdlibDir == "" || filepath.IsAbs(stdlibDir) {
		return stdlibDir
	}
	return filepath.Join(execRoot, stdlibDir)
}

func readManifest(path string) ([]string, error) {
	manifestFile, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open manifest: %w", err)
	}
	defer func() { _ = manifestFile.Close() }()

	var files []string
	scanner := bufio.NewScanner(manifestFile)
	scanner.Buffer(make([]byte, 0, manifestBufferInitial), manifestBufferMax)
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
	packagesByID := map[string]*FlatPackage{}
	var packages []*FlatPackage
	for _, jsonPath := range jsonFiles {
		jsonFile, err := os.Open(jsonPath)
		if err != nil {
			return nil, fmt.Errorf("open %s: %w", jsonPath, err)
		}
		decoder := json.NewDecoder(jsonFile)
		for decoder.More() {
			flatPackage := &FlatPackage{}
			if err := decoder.Decode(flatPackage); err != nil {
				_ = jsonFile.Close()
				return nil, fmt.Errorf("decode %s: %w", jsonPath, err)
			}
			resolvePaths(flatPackage, execRoot)
			if existing, ok := packagesByID[flatPackage.ID]; ok {
				mergePackage(existing, flatPackage)
				continue
			}
			packagesByID[flatPackage.ID] = flatPackage
			packages = append(packages, flatPackage)
		}
		_ = jsonFile.Close()
	}
	return packages, nil
}

// mergePackage folds source into destination, unioning file lists and imports.
// Used to reunite a go_test's testmain with its test sources under their shared
// ID.
func mergePackage(destination, source *FlatPackage) {
	destination.GoFiles = unionFiles(destination.GoFiles, source.GoFiles)
	destination.CompiledGoFiles = unionFiles(destination.CompiledGoFiles, source.CompiledGoFiles)
	destination.OtherFiles = unionFiles(destination.OtherFiles, source.OtherFiles)
	if destination.PkgPath == "" {
		destination.PkgPath = source.PkgPath
	}
	if destination.ExportFile == "" {
		destination.ExportFile = source.ExportFile
	}
	for importPath, packageID := range source.Imports {
		if destination.Imports == nil {
			destination.Imports = map[string]string{}
		}
		if _, ok := destination.Imports[importPath]; !ok {
			destination.Imports[importPath] = packageID
		}
	}
}

func unionFiles(first, second []string) []string {
	seen := map[string]bool{}
	union := make([]string, 0, len(first)+len(second))
	for _, file := range append(append([]string{}, first...), second...) {
		if !seen[file] {
			seen[file] = true
			union = append(union, file)
		}
	}
	return union
}

func resolvePaths(flatPackage *FlatPackage, execRoot string) {
	resolveSlice(flatPackage.GoFiles, execRoot)
	resolveSlice(flatPackage.CompiledGoFiles, execRoot)
	resolveSlice(flatPackage.OtherFiles, execRoot)
	flatPackage.ExportFile = resolvePath(flatPackage.ExportFile, execRoot)
	flatPackage.GoFiles = dropGenerated(flatPackage.GoFiles)
	flatPackage.CompiledGoFiles = dropGenerated(filterBuildTags(flatPackage.CompiledGoFiles))
}

func dropGenerated(files []string) []string {
	keptFiles := make([]string, 0, len(files))
	for _, f := range files {
		if filepath.Base(f) != testMainFile {
			keptFiles = append(keptFiles, f)
		}
	}
	return keptFiles
}

// filterBuildTags keeps only the files the host build context selects, plus
// extension-less and cgo-processed files, which MatchFile rejects but the
// type checker needs.
func filterBuildTags(files []string) []string {
	keptFiles := make([]string, 0, len(files))
	for _, f := range files {
		dir, name := filepath.Split(f)
		match, _ := buildContext.MatchFile(dir, name)
		if match || filepath.Ext(f) == "" || isCgoProcessed(name) {
			keptFiles = append(keptFiles, f)
		}
	}
	return keptFiles
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
func resolvePath(rawPath, execRoot string) string {
	if rawPath == "" {
		return ""
	}
	for _, placeholder := range []string{execrootPlaceholder, workspacePlaceholder, outputBasePlaceholder} {
		if strings.HasPrefix(rawPath, placeholder) {
			return filepath.Join(execRoot, strings.TrimPrefix(rawPath, placeholder))
		}
	}
	return rawPath
}

// resolveImports parses each compiled file's import clauses and links stdlib
// imports, which rules_go omits from pkg.json (Bazel does not model them).
// Dependency type info comes from each package's ExportFile, so a file that
// cannot be parsed is skipped rather than failing the whole graph — only its
// stdlib edges are lost, which the type checker recovers from export data.
func resolveImports(flatPackage *FlatPackage, stdlibByPath map[string]string, fileSet *token.FileSet) {
	for _, sourceFile := range flatPackage.CompiledGoFiles {
		astFile, err := parser.ParseFile(fileSet, sourceFile, nil, parser.ImportsOnly)
		if err != nil {
			continue
		}
		if flatPackage.Name == "" {
			flatPackage.Name = astFile.Name.Name
		}
		for _, rawImport := range astFile.Imports {
			importPath, err := strconv.Unquote(rawImport.Path.Value)
			if err != nil || importPath == "C" {
				continue
			}
			if _, ok := flatPackage.Imports[importPath]; ok {
				continue
			}
			if stdlibID, ok := stdlibByPath[importPath]; ok {
				if flatPackage.Imports == nil {
					flatPackage.Imports = map[string]string{}
				}
				flatPackage.Imports[importPath] = stdlibID
			}
		}
	}
}

// moveTestFiles splits external test files (package foo_test) into their own
// package, mirroring go/packages. gavel's tests are black-box, so without this
// golangci-lint would mis-attribute their findings.
func moveTestFiles(flatPackage *FlatPackage, fileSet *token.FileSet) *FlatPackage {
	internalGo, externalGo := splitExternalTests(flatPackage.Name, flatPackage.GoFiles, fileSet)
	internalCompiled, externalCompiled := splitExternalTests(flatPackage.Name, flatPackage.CompiledGoFiles, fileSet)
	if len(externalGo) == 0 && len(externalCompiled) == 0 {
		return nil
	}
	flatPackage.GoFiles = internalGo
	flatPackage.CompiledGoFiles = internalCompiled

	imports := map[string]string{}
	for importPath, packageID := range flatPackage.Imports {
		imports[importPath] = packageID
	}
	imports[flatPackage.PkgPath] = flatPackage.ID

	return &FlatPackage{
		ID:              flatPackage.ID + "_xtest",
		Name:            flatPackage.Name + "_test",
		PkgPath:         flatPackage.PkgPath + "_test",
		GoFiles:         externalGo,
		CompiledGoFiles: externalCompiled,
		OtherFiles:      flatPackage.OtherFiles,
		ExportFile:      flatPackage.ExportFile,
		Imports:         imports,
	}
}

func splitExternalTests(pkgName string, files []string, fileSet *token.FileSet) (internal, external []string) {
	for _, sourceFile := range files {
		if !strings.HasSuffix(sourceFile, testSuffix) {
			internal = append(internal, sourceFile)
			continue
		}
		astFile, err := parser.ParseFile(fileSet, sourceFile, nil, parser.PackageClauseOnly)
		if err == nil && astFile.Name.Name != pkgName {
			external = append(external, sourceFile)
		} else {
			internal = append(internal, sourceFile)
		}
	}
	return internal, external
}

// matchRoots maps the patterns go/packages passes (file= queries or directory
// patterns) to the package IDs whose sources satisfy them.
func matchRoots(patterns []string, packages []*FlatPackage, execRoot string) []string {
	var roots []string
	seen := map[string]bool{}
	addRoot := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			roots = append(roots, id)
		}
	}
	for _, pattern := range patterns {
		if file, ok := strings.CutPrefix(pattern, filePrefix); ok {
			matchFile(absFrom(file, execRoot), packages, addRoot)
			continue
		}
		pattern = strings.TrimPrefix(pattern, patternPrefix)
		recursive := strings.HasSuffix(pattern, recursiveSuffix)
		directory := absFrom(strings.TrimSuffix(strings.TrimSuffix(pattern, recursiveSuffix), "/"), execRoot)
		matchDir(directory, recursive, packages, addRoot)
	}
	return roots
}

func matchFile(file string, packages []*FlatPackage, addRoot func(string)) {
	for _, flatPackage := range packages {
		for _, candidate := range append(append([]string{}, flatPackage.GoFiles...), flatPackage.CompiledGoFiles...) {
			if candidate == file {
				addRoot(flatPackage.ID)
				return
			}
		}
	}
}

func matchDir(directory string, recursive bool, packages []*FlatPackage, addRoot func(string)) {
	for _, flatPackage := range packages {
		if flatPackage.Standard {
			continue
		}
		for _, sourceFile := range flatPackage.CompiledGoFiles {
			fileDir := filepath.Dir(sourceFile)
			if fileDir == directory || (recursive && strings.HasPrefix(fileDir, directory+string(filepath.Separator))) {
				addRoot(flatPackage.ID)
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
