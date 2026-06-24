package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
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

func writeFakeScript(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-ruff")
	script := fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))
	return bin
}

func TestExecute_MissingOut(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "file.py"})

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
	setArgs(t, []string{"test", "--out", "/tmp/out.sarif", "file.py"})

	code := execute()

	assert.Equal(t, 1, code)
}

func TestExecute_Success(t *testing.T) {
	ruff := writeFakeScript(t, 0)
	resetFlags(t)
	out := filepath.Join(t.TempDir(), "out.sarif")
	setArgs(t, []string{"test", "--ruff", ruff, "--out", out, "test.py"})

	code := execute()

	assert.Equal(t, 0, code)
}

func TestBuildArgs(t *testing.T) {
	got := buildArgs("/tmp/out.sarif", []string{"foo.py", "bar.py"})

	assert.Equal(t, []string{
		"check",
		"--output-format=sarif",
		"--no-fix",
		"--exit-zero",
		"--output-file=/tmp/out.sarif",
		"foo.py",
		"bar.py",
	}, got)
}

func TestBuildArgsSingleFile(t *testing.T) {
	got := buildArgs("/tmp/out.sarif", []string{"main.py"})

	assert.Len(t, got, 6)
	assert.Equal(t, "main.py", got[5])
}

func TestRun_Success(t *testing.T) {
	ruff := writeFakeScript(t, 0)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run(ruff, out, []string{"test.py"})

	require.NoError(t, err)
}

func TestRun_MkdirAllError(t *testing.T) {
	ruff := writeFakeScript(t, 0)

	err := run(ruff, "/dev/null/impossible/out.sarif", []string{"test.py"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output dir")
}

func TestRun_RuffNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run("", out, []string{"test.py"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ruff not found")
}

func TestRun_RuffFoundInPath(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "ruff")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("PATH", tmp)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run("", out, []string{"test.py"})

	require.NoError(t, err)
}

func TestRun_CommandError(t *testing.T) {
	ruff := writeFakeScript(t, 1)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run(ruff, out, []string{"test.py"})

	require.Error(t, err)
}

func TestResolveBazelExternal_ExistingPath(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "ruff")
	require.NoError(t, os.WriteFile(bin, []byte("x"), 0o755))

	got := resolveBazelExternal(bin)

	assert.Equal(t, bin, got)
}

func TestResolveBazelExternal_NonExistentPath(t *testing.T) {
	got := resolveBazelExternal("/nonexistent/ruff")

	assert.Equal(t, "/nonexistent/ruff", got)
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
