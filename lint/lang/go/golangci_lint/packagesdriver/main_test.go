package main

import (
	"go/token"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolvePath_ExecrootPlaceholder(t *testing.T) {
	got := resolvePath(execrootPlaceholder+"/bazel-out/x.x", "/sandbox/root")

	assert.Equal(t, "/sandbox/root/bazel-out/x.x", got)
}

func TestResolvePath_WorkspaceAndOutputBaseCollapseToExecRoot(t *testing.T) {
	assert.Equal(t, "/r/core/a.go", resolvePath(workspacePlaceholder+"/core/a.go", "/r"))
	assert.Equal(t, "/r/external/dep/b.go", resolvePath(outputBasePlaceholder+"/external/dep/b.go", "/r"))
}

func TestResolvePath_EmptyAndUnprefixed(t *testing.T) {
	assert.Equal(t, "", resolvePath("", "/r"))
	assert.Equal(t, "/already/abs", resolvePath("/already/abs", "/r"))
}

func TestReadManifest_SkipsBlankLines(t *testing.T) {
	dir := t.TempDir()
	m := filepath.Join(dir, "manifest")
	require.NoError(t, os.WriteFile(m, []byte("a.json\n\n  b.json  \n"), 0o644))

	got, err := readManifest(m)

	require.NoError(t, err)
	assert.Equal(t, []string{"a.json", "b.json"}, got)
}

func TestReadManifest_MissingFile(t *testing.T) {
	_, err := readManifest(filepath.Join(t.TempDir(), "nope"))

	require.Error(t, err)
}

func TestLoadPackages_StreamResolvesAndMergesSameID(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib.go")
	aTest := filepath.Join(dir, "a_test.go")
	require.NoError(t, os.WriteFile(lib, []byte("package a\n"), 0o644))
	require.NoError(t, os.WriteFile(aTest, []byte("package a_test\n"), 0o644))
	jf := filepath.Join(dir, "p.json")
	require.NoError(t, os.WriteFile(jf, []byte(
		`{"ID":"//a","PkgPath":"a","ExportFile":"`+execrootPlaceholder+`/a.x","CompiledGoFiles":["`+lib+`"]}`+
			`{"ID":"//a","CompiledGoFiles":["`+aTest+`"]}`+
			`{"ID":"//b","PkgPath":"b"}`), 0o644))

	pkgs, err := loadPackages([]string{jf}, "/r")

	require.NoError(t, err)
	require.Len(t, pkgs, 2, "same ID must collapse to one merged package")
	assert.Equal(t, "/r/a.x", pkgs[0].ExportFile)
	assert.Equal(t, []string{lib, aTest}, pkgs[0].CompiledGoFiles,
		"internal lib and test sources must both survive the merge")
}

func TestMergePackage_UnionsFilesAndImports(t *testing.T) {
	dst := &FlatPackage{ID: "//a", GoFiles: []string{"x.go"}, Imports: map[string]string{"fmt": "@fmt"}}
	src := &FlatPackage{ID: "//a", GoFiles: []string{"x.go", "y.go"}, ExportFile: "/a.x", Imports: map[string]string{"io": "@io"}}

	mergePackage(dst, src)

	assert.Equal(t, []string{"x.go", "y.go"}, dst.GoFiles, "files unioned without duplicates")
	assert.Equal(t, "/a.x", dst.ExportFile, "empty ExportFile filled from src")
	assert.Equal(t, map[string]string{"fmt": "@fmt", "io": "@io"}, dst.Imports)
}

func TestResolveImports_AddsStdlibImports(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.go")
	require.NoError(t, os.WriteFile(src, []byte("package a\nimport (\n\t\"fmt\"\n\t\"C\"\n)\n"), 0o644))
	pkg := &FlatPackage{ID: "//a", Name: "a", PkgPath: "a", CompiledGoFiles: []string{src}}
	stdlib := map[string]string{"fmt": "@stdlib//:fmt"}

	resolveImports(pkg, stdlib, token.NewFileSet())

	assert.Equal(t, "@stdlib//:fmt", pkg.Imports["fmt"])
	_, hasC := pkg.Imports["C"]
	assert.False(t, hasC, "cgo pseudo-import must be skipped")
}

func TestResolveImports_FillsEmptyName(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "a.go")
	require.NoError(t, os.WriteFile(src, []byte("package realname\n"), 0o644))
	pkg := &FlatPackage{ID: "//a", CompiledGoFiles: []string{src}}

	resolveImports(pkg, nil, token.NewFileSet())

	assert.Equal(t, "realname", pkg.Name)
}

func TestMoveTestFiles_SplitsExternalTest(t *testing.T) {
	dir := t.TempDir()
	internal := filepath.Join(dir, "a.go")
	xtest := filepath.Join(dir, "a_ext_test.go")
	require.NoError(t, os.WriteFile(internal, []byte("package a\n"), 0o644))
	require.NoError(t, os.WriteFile(xtest, []byte("package a_test\n"), 0o644))
	pkg := &FlatPackage{ID: "//a", Name: "a", PkgPath: "a", GoFiles: []string{internal, xtest}, CompiledGoFiles: []string{internal, xtest}}

	got := moveTestFiles(pkg, token.NewFileSet())

	require.NotNil(t, got)
	assert.Equal(t, "//a_xtest", got.ID)
	assert.Equal(t, "a_test", got.Name)
	assert.Equal(t, []string{xtest}, got.GoFiles)
	assert.Equal(t, []string{internal}, pkg.GoFiles, "internal test stays on the base package")
	assert.Equal(t, "//a", got.Imports["a"], "xtest imports the package under test")
}

func TestMoveTestFiles_NoExternalReturnsNil(t *testing.T) {
	dir := t.TempDir()
	internal := filepath.Join(dir, "a_test.go")
	require.NoError(t, os.WriteFile(internal, []byte("package a\n"), 0o644))
	pkg := &FlatPackage{ID: "//a", Name: "a", GoFiles: []string{internal}, CompiledGoFiles: []string{internal}}

	assert.Nil(t, moveTestFiles(pkg, token.NewFileSet()))
}

func TestMatchRoots_DirPattern(t *testing.T) {
	pkgs := []*FlatPackage{
		{ID: "//core/verdict:verdict", CompiledGoFiles: []string{"/r/core/verdict/a.go"}},
		{ID: "//core/other:other", CompiledGoFiles: []string{"/r/core/other/b.go"}},
	}

	roots := matchRoots([]string{"./core/verdict"}, pkgs, "/r")

	assert.Equal(t, []string{"//core/verdict:verdict"}, roots)
}

func TestMatchRoots_RecursivePattern(t *testing.T) {
	pkgs := []*FlatPackage{
		{ID: "//core/a:a", CompiledGoFiles: []string{"/r/core/a/x.go"}},
		{ID: "//core/a/b:b", CompiledGoFiles: []string{"/r/core/a/b/y.go"}},
		{ID: "//other:other", CompiledGoFiles: []string{"/r/other/z.go"}},
	}

	roots := matchRoots([]string{"./core/a/..."}, pkgs, "/r")

	assert.ElementsMatch(t, []string{"//core/a:a", "//core/a/b:b"}, roots)
}

func TestMatchRoots_FileQuery(t *testing.T) {
	pkgs := []*FlatPackage{
		{ID: "//a:a", GoFiles: []string{"/r/a/x.go"}, CompiledGoFiles: []string{"/r/a/x.go"}},
	}

	roots := matchRoots([]string{"file=/r/a/x.go"}, pkgs, "/r")

	assert.Equal(t, []string{"//a:a"}, roots)
}

func TestMatchRoots_SkipsStdlibForDirMatch(t *testing.T) {
	pkgs := []*FlatPackage{
		{ID: "@stdlib//:fmt", Standard: true, CompiledGoFiles: []string{"/r/fmt/print.go"}},
	}

	roots := matchRoots([]string{"./fmt"}, pkgs, "/r")

	assert.Empty(t, roots)
}

func TestRun_EndToEnd(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "verdict.go")
	require.NoError(t, os.WriteFile(src, []byte("package verdict\nimport \"fmt\"\nvar _ = fmt.Sprint\n"), 0o644))
	pkgJSON := filepath.Join(dir, "verdict.pkg.json")
	require.NoError(t, os.WriteFile(pkgJSON, []byte(
		`{"ID":"//v:v","Name":"verdict","PkgPath":"v","GoFiles":["`+src+`"],"CompiledGoFiles":["`+src+`"]}`), 0o644))
	stdlibJSON := filepath.Join(dir, "stdlib.json")
	require.NoError(t, os.WriteFile(stdlibJSON, []byte(
		`{"ID":"@stdlib//:fmt","Name":"fmt","PkgPath":"fmt","Standard":true}`), 0o644))
	stdlibDir := filepath.Join(dir, "stdlibpkg")
	require.NoError(t, os.MkdirAll(stdlibDir, 0o755))
	fmtExport := filepath.Join(stdlibDir, "fmt.a")
	require.NoError(t, os.WriteFile(fmtExport, []byte("x"), 0o644))
	manifest := filepath.Join(dir, "manifest")
	require.NoError(t, os.WriteFile(manifest, []byte(pkgJSON+"\n"+stdlibJSON+"\n"), 0o644))

	resp, err := run(manifest, stdlibDir, dir, []string{"file=" + src})

	require.NoError(t, err)
	assert.False(t, resp.NotHandled)
	assert.Equal(t, []string{"//v:v"}, resp.Roots)
	require.Len(t, resp.Packages, 2)
	var verdict, fmtPkg *FlatPackage
	for _, p := range resp.Packages {
		switch p.ID {
		case "//v:v":
			verdict = p
		case "@stdlib//:fmt":
			fmtPkg = p
		}
	}
	require.NotNil(t, verdict)
	assert.Equal(t, "@stdlib//:fmt", verdict.Imports["fmt"], "stdlib import resolved into the graph")
	require.NotNil(t, fmtPkg)
	assert.Equal(t, fmtExport, fmtPkg.ExportFile, "stdlib package given its compiled .a as export data")
}

func TestRun_MissingManifestEnv(t *testing.T) {
	_, err := run("", "", "/r", nil)

	require.Error(t, err)
}

func TestAssignStdlibExports_FillsStandardPackagesFromCompiledArchives(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "math.a"), []byte("x"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "net"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "net", "http.a"), []byte("x"), 0o644))

	packages := []*FlatPackage{
		{ID: "m", PkgPath: "math", Standard: true},
		{ID: "h", PkgPath: "net/http", Standard: true},
		{ID: "keep", PkgPath: "embed", Standard: true, ExportFile: "/already/set.a"},
		{ID: "absent", PkgPath: "syscall", Standard: true},
		{ID: "app", PkgPath: "example.com/app", Standard: false},
	}

	assignStdlibExports(packages, dir)

	assert.Equal(t, filepath.Join(dir, "math.a"), packages[0].ExportFile)
	assert.Equal(t, filepath.Join(dir, "net", "http.a"), packages[1].ExportFile)
	assert.Equal(t, "/already/set.a", packages[2].ExportFile, "existing export data is not overwritten")
	assert.Empty(t, packages[3].ExportFile, "no archive on disk leaves the package untouched")
	assert.Empty(t, packages[4].ExportFile, "non-standard packages are never touched")
}

func TestAssignStdlibExports_EmptyDirIsNoOp(t *testing.T) {
	packages := []*FlatPackage{{ID: "m", PkgPath: "math", Standard: true}}

	assignStdlibExports(packages, "")

	assert.Empty(t, packages[0].ExportFile)
}

func TestFilterBuildTags_DropsForeignGOOS(t *testing.T) {
	dir := t.TempDir()
	kept := filepath.Join(dir, "node_net.go")
	dropped := filepath.Join(dir, "node_js.go")
	require.NoError(t, os.WriteFile(kept, []byte("package uuid\n"), 0o644))
	require.NoError(t, os.WriteFile(dropped, []byte("package uuid\n"), 0o644))

	got := filterBuildTags([]string{kept, dropped})

	assert.Equal(t, []string{kept}, got, "a _js.go file must be excluded on a non-js host")
}

func TestDropGenerated_RemovesTestmain(t *testing.T) {
	got := dropGenerated([]string{"/x/a_test.go", "/x/integration_test_/testmain.go"})

	assert.Equal(t, []string{"/x/a_test.go"}, got, "generated testmain.go must never be linted")
}

func TestFilterBuildTags_KeepsCgoProcessedFiles(t *testing.T) {
	got := filterBuildTags([]string{"/x/_cgo_gotypes.go", "/x/foo.cgo1.go"})

	assert.Equal(t, []string{"/x/_cgo_gotypes.go", "/x/foo.cgo1.go"}, got)
}
