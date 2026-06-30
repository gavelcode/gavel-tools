package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func resetFlags(t *testing.T) {
	t.Helper()
	orig := flag.CommandLine
	t.Cleanup(func() { flag.CommandLine = orig })
	flag.CommandLine = flag.NewFlagSet("test", flag.ContinueOnError)
}

func setArgs(t *testing.T, args []string) {
	t.Helper()
	orig := os.Args
	t.Cleanup(func() { os.Args = orig })
	os.Args = args
}

const fakeEslintEmit = `#!/bin/sh
prev=""
for a in "$@"; do [ "$prev" = "--output-file" ] && o="$a"; prev="$a"; done
[ -n "$o" ] && printf '[]' > "$o"
`

func writeFakeEslint(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-eslint")
	script := fakeEslintEmit + fmt.Sprintf("exit %d\n", exitCode)
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))
	return bin
}

func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

func TestExecute_MissingOut(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "file.ts"})

	code := execute()

	assert.Equal(t, 2, code)
}

func TestExecute_MissingFiles(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "--out", "/tmp/out.sarif"})

	code := execute()

	assert.Equal(t, 2, code)
}

func TestExecute_RunError(t *testing.T) {
	resetFlags(t)
	t.Setenv("PATH", t.TempDir())
	setArgs(t, []string{"test", "--out", "/tmp/out.sarif", "file.ts"})

	code := execute()

	assert.Equal(t, 1, code)
}

func TestExecute_Success(t *testing.T) {
	eslint := writeFakeEslint(t, 0)
	resetFlags(t)
	src := writeTempFile(t, "app.ts", "const x = 1;")
	out := filepath.Join(t.TempDir(), "out.sarif")
	setArgs(t, []string{"test", "--eslint", eslint, "--out", out, src})

	code := execute()

	assert.Equal(t, 0, code)
}

func TestBuildArgs_WithConfig(t *testing.T) {
	got := buildArgs("/tmp/report.json", "eslint.config.js", []string{"src/app.tsx", "src/main.ts"})

	assert.Equal(t, []string{
		"--format", "json",
		"--output-file", "/tmp/report.json",
		"--config", "eslint.config.js",
		"src/app.tsx",
		"src/main.ts",
	}, got)
}

func TestBuildArgs_WithoutConfig(t *testing.T) {
	got := buildArgs("/tmp/report.json", "", []string{"src/app.tsx"})

	assert.Equal(t, []string{
		"--format", "json",
		"--output-file", "/tmp/report.json",
		"src/app.tsx",
	}, got)
}

func TestBuildArgs_SingleFile(t *testing.T) {
	got := buildArgs("/tmp/out.sarif", "", []string{"index.ts"})

	assert.Len(t, got, 5)
	assert.Equal(t, "index.ts", got[4])
}

func TestIsLintExitCode_LintFailure(t *testing.T) {
	cmd := exec.Command("false")
	err := cmd.Run()

	assert.True(t, isLintExitCode(err))
}

func TestIsLintExitCode_ExitCode2(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 2")
	err := cmd.Run()

	assert.False(t, isLintExitCode(err))
}

func TestIsLintExitCode_NonExitError(t *testing.T) {
	assert.False(t, isLintExitCode(errors.New("something else")))
}

func TestIsMisconfiguration_ExitCode2(t *testing.T) {
	cmd := exec.Command("sh", "-c", "exit 2")
	err := cmd.Run()

	assert.True(t, isMisconfiguration(err))
}

func TestIsMisconfiguration_ExitCode1(t *testing.T) {
	cmd := exec.Command("false")
	err := cmd.Run()

	assert.False(t, isMisconfiguration(err))
}

func TestIsMisconfiguration_NonExitError(t *testing.T) {
	assert.False(t, isMisconfiguration(errors.New("something")))
}

func TestEslintEnv_FiltersAndAdds(t *testing.T) {
	t.Setenv("JS_BINARY__PATCH_NODE_FS", "1")
	t.Setenv("NODE_PATH", "/old/path")

	env := eslintEnv("/some/tool/bin/eslint")

	hasOldPatch := false
	hasOldNodePath := false
	hasNewPatch := false
	hasNewNodePath := false
	for _, entry := range env {
		if entry == "JS_BINARY__PATCH_NODE_FS=1" {
			hasOldPatch = true
		}
		if entry == "NODE_PATH=/old/path" {
			hasOldNodePath = true
		}
		if entry == "JS_BINARY__PATCH_NODE_FS=0" {
			hasNewPatch = true
		}
		if strings.HasPrefix(entry, "NODE_PATH=") && strings.Contains(entry, "node_modules") {
			hasNewNodePath = true
		}
	}

	assert.False(t, hasOldPatch, "old JS_BINARY__PATCH_NODE_FS should be filtered")
	assert.False(t, hasOldNodePath, "old NODE_PATH should be filtered")
	assert.True(t, hasNewPatch, "JS_BINARY__PATCH_NODE_FS=0 should be added")
	assert.True(t, hasNewNodePath, "NODE_PATH with node_modules should be added")
}

func TestFixStoreDir_ReadDirError(t *testing.T) {
	aspectDir := filepath.Join(t.TempDir(), ".aspect_rules_js")
	require.NoError(t, os.MkdirAll(aspectDir, 0o755))
	require.NoError(t, os.Chmod(aspectDir, 0o000))
	t.Cleanup(func() { _ = os.Chmod(aspectDir, 0o755) })

	fixStoreDir(aspectDir)
}

func TestFixStoreDir_NonExistent(t *testing.T) {
	fixStoreDir(filepath.Join(t.TempDir(), "nonexistent"))
}

func TestFixStoreDir_StoreDirExists(t *testing.T) {
	aspectDir := filepath.Join(t.TempDir(), ".aspect_rules_js")
	require.NoError(t, os.MkdirAll(filepath.Join(aspectDir, "s"), 0o755))

	fixStoreDir(aspectDir)
}

func TestFixStoreDir_CreatesSymlinks(t *testing.T) {
	aspectDir := filepath.Join(t.TempDir(), ".aspect_rules_js")
	require.NoError(t, os.MkdirAll(filepath.Join(aspectDir, "package-a"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(aspectDir, "package-b"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(aspectDir, "somefile"), []byte("x"), 0o644))

	fixStoreDir(aspectDir)

	storeDir := filepath.Join(aspectDir, "s")
	_, err := os.Stat(storeDir)
	require.NoError(t, err)

	linkA, err := os.Readlink(filepath.Join(storeDir, "package-a"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("..", "package-a"), linkA)

	linkB, err := os.Readlink(filepath.Join(storeDir, "package-b"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("..", "package-b"), linkB)

	_, err = os.Lstat(filepath.Join(storeDir, "somefile"))
	assert.True(t, os.IsNotExist(err))
}

func TestFixBrokenStoreLinks_NoRunfiles(t *testing.T) {
	tmp := t.TempDir()
	eslint := filepath.Join(tmp, "tool", "bin", "eslint")
	require.NoError(t, os.MkdirAll(filepath.Dir(eslint), 0o755))
	require.NoError(t, os.WriteFile(eslint, []byte("x"), 0o755))

	fixBrokenStoreLinks(eslint)
}

func TestFixBrokenStoreLinks_WithRunfiles(t *testing.T) {
	tmp := t.TempDir()
	eslint := filepath.Join(tmp, "tool", "bin", "eslint")
	require.NoError(t, os.MkdirAll(filepath.Dir(eslint), 0o755))
	require.NoError(t, os.WriteFile(eslint, []byte("x"), 0o755))

	runfilesDir := eslint + ".runfiles"
	aspectDir := filepath.Join(runfilesDir, "some_pkg", "tools", "typescript", "eslint", "node_modules", ".aspect_rules_js")
	require.NoError(t, os.MkdirAll(filepath.Join(aspectDir, "pkg-x"), 0o755))

	fixBrokenStoreLinks(eslint)

	_, err := os.Stat(filepath.Join(aspectDir, "s", "pkg-x"))
	assert.NoError(t, err)
}

func TestResolveBazelExternal_ExistingPath(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "eslint")
	require.NoError(t, os.WriteFile(bin, []byte("x"), 0o755))

	got := resolveBazelExternal(bin)

	assert.Equal(t, bin, got)
}

func TestResolveBazelExternal_NonExistentPath(t *testing.T) {
	got := resolveBazelExternal("/nonexistent/eslint")

	assert.Equal(t, "/nonexistent/eslint", got)
}

func TestResolveBazelExternal_ExternalPrefixAlternate(t *testing.T) {
	tmp := t.TempDir()
	workDir := filepath.Join(tmp, "a", "b")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "external", "foo"), 0o755))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	got := resolveBazelExternal("external/foo")

	assert.Equal(t, filepath.Join("..", "..", "external", "foo"), got)
}

func TestResolveBazelExternal_ExternalPrefixGlobMatch(t *testing.T) {
	tmp := t.TempDir()
	workDir := filepath.Join(tmp, "a", "b")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "external", "prefix~foo"), 0o755))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	got := resolveBazelExternal("external/foo")

	assert.Equal(t, filepath.Join("..", "..", "external", "prefix~foo"), got)
}

func TestResolveBazelExternal_ExternalPrefixNoMatch(t *testing.T) {
	tmp := t.TempDir()
	workDir := filepath.Join(tmp, "a", "b")
	require.NoError(t, os.MkdirAll(workDir, 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(tmp, "external"), 0o755))

	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(workDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	got := resolveBazelExternal("external/nomatch")

	assert.Equal(t, "external/nomatch", got)
}

func TestRun_Success(t *testing.T) {
	eslint := writeFakeEslint(t, 0)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")
	src := writeTempFile(t, "app.ts", "const x = 1;")

	err := run(eslint, out, "", []string{src})

	require.NoError(t, err)
}

func TestRun_WithConfig(t *testing.T) {
	eslint := writeFakeEslint(t, 0)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")
	src := writeTempFile(t, "app.ts", "const x = 1;")

	err := run(eslint, out, "eslint.config.js", []string{src})

	require.NoError(t, err)
}

func TestRun_LintFindings(t *testing.T) {
	eslint := writeFakeEslint(t, 1)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")
	src := writeTempFile(t, "app.ts", "const x = 1;")

	err := run(eslint, out, "", []string{src})

	require.NoError(t, err)
}

func TestRun_Misconfiguration(t *testing.T) {
	eslint := writeFakeEslint(t, 2)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")
	src := writeTempFile(t, "app.ts", "const x = 1;")

	err := run(eslint, out, "", []string{src})

	require.NoError(t, err)
	data, readErr := os.ReadFile(out)
	require.NoError(t, readErr)
	assert.Contains(t, string(data), `"executionSuccessful": false`)
}

func TestRun_OtherExitError(t *testing.T) {
	eslint := writeFakeEslint(t, 3)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")
	src := writeTempFile(t, "app.ts", "const x = 1;")

	err := run(eslint, out, "", []string{src})

	require.NoError(t, err)
	data, readErr := os.ReadFile(out)
	require.NoError(t, readErr)
	assert.Contains(t, string(data), `"executionSuccessful": false`)
}

func TestRun_MkdirError(t *testing.T) {
	eslint := writeFakeEslint(t, 0)

	err := run(eslint, "/dev/null/impossible/out.sarif", "", []string{"file.ts"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output dir")
}

func TestRun_EslintNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run("", out, "", []string{"file.ts"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "eslint not found")
}

func TestRun_AbsFallbackWhenGetwdFails(t *testing.T) {
	tmp := t.TempDir()
	eslint := filepath.Join(tmp, "fake-eslint")
	require.NoError(t, os.WriteFile(eslint, []byte(fakeEslintEmit+"exit 0\n"), 0o755))

	removedDir := filepath.Join(tmp, "removed")
	require.NoError(t, os.MkdirAll(removedDir, 0o755))
	origDir, getErr := os.Getwd()
	require.NoError(t, getErr)
	require.NoError(t, os.Chdir(removedDir))
	require.NoError(t, os.RemoveAll(removedDir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	out := filepath.Join(tmp, "result.sarif")

	err := run(eslint, out, "", []string{"relative/file.ts"})

	require.NoError(t, err)
}

func TestRun_EslintFoundInPath(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "eslint")
	require.NoError(t, os.WriteFile(bin, []byte(fakeEslintEmit+"exit 0\n"), 0o755))
	t.Setenv("PATH", tmp)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")
	src := writeTempFile(t, "app.ts", "const x = 1;")

	err := run("", out, "", []string{src})

	require.NoError(t, err)
}
