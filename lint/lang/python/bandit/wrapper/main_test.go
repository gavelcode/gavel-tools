package main

import (
	"flag"
	"fmt"
	"os"
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

const fakeBanditEmit = `#!/bin/sh
prev=""
for a in "$@"; do [ "$prev" = "--output" ] && o="$a"; prev="$a"; done
[ -n "$o" ] && printf '{"version":"2.1.0","runs":[{"tool":{"driver":{"name":"bandit"}},"results":[]}]}' > "$o"
`

func writeFakeScript(t *testing.T, exitCode int) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-python")
	script := fakeBanditEmit + fmt.Sprintf("exit %d\n", exitCode)
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
	out := filepath.Join(t.TempDir(), "out.sarif")
	setArgs(t, []string{"test", "--python", "/nonexistent/python3", "--out", out, "file.py"})

	code := execute()

	assert.Equal(t, 0, code)
	data, _ := os.ReadFile(out)
	assert.Contains(t, string(data), `"executionSuccessful": false`)
}

func TestExecute_SuccessWithExplicitPython(t *testing.T) {
	fakePython := writeFakeScript(t, 0)
	resetFlags(t)
	out := filepath.Join(t.TempDir(), "out.sarif")
	setArgs(t, []string{"test", "--python", fakePython, "--out", out, "test.py"})

	code := execute()

	assert.Equal(t, 0, code)
}

func TestBuildArgs(t *testing.T) {
	got := buildArgs("/tmp/out.sarif", []string{"foo.py", "bar.py"})

	assert.Equal(t, []string{
		"-m", "bandit",
		"--format", "sarif",
		"--exit-zero",
		"--output", "/tmp/out.sarif",
		"foo.py",
		"bar.py",
	}, got)
}

func TestBuildEnvWithSitePackages(t *testing.T) {
	t.Setenv("PYTHONPATH", "/old/path")

	got := buildEnv("/path/to/site-packages")

	foundNew := false
	foundOld := false
	for _, item := range got {
		if item == "PYTHONPATH=/path/to/site-packages" {
			foundNew = true
		}
		if item == "PYTHONPATH=/old/path" {
			foundOld = true
		}
	}
	assert.True(t, foundNew, "new PYTHONPATH should be set")
	assert.False(t, foundOld, "old PYTHONPATH should be filtered")
}

func TestBuildEnvWithoutSitePackages(t *testing.T) {
	got := buildEnv("")

	for _, item := range got {
		assert.False(t, len(item) > 11 && item[:11] == "PYTHONPATH=", "PYTHONPATH should not be added")
	}
}

func TestFindPython_ReturnsNonEmpty(t *testing.T) {
	python, err := findPython()
	if err != nil {
		t.Skip("python3 not available in this environment")
	}

	assert.NotEmpty(t, python)
}

func TestFindPython_FallbackCandidate(t *testing.T) {
	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)

	python, err := findPython()
	if err != nil {
		t.Skip("no hardcoded python3 candidate exists on this machine")
	}

	assert.NotEmpty(t, python)
}

func TestRun_SuccessWithExplicitPython(t *testing.T) {
	fakePython := writeFakeScript(t, 0)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run(fakePython, "", out, []string{"test.py"})

	require.NoError(t, err)
}

func TestRun_SuccessWithSitePackages(t *testing.T) {
	fakePython := writeFakeScript(t, 0)
	siteDir := t.TempDir()
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run(fakePython, siteDir, out, []string{"test.py"})

	require.NoError(t, err)
}

func TestRun_MkdirAllError(t *testing.T) {
	fakePython := writeFakeScript(t, 0)

	err := run(fakePython, "", "/dev/null/impossible/out.sarif", []string{"test.py"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output dir")
}

func TestRun_FindPythonFallback(t *testing.T) {
	emptyDir := t.TempDir()
	t.Setenv("PATH", emptyDir)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run("", "", out, []string{"test.py"})

	if err == nil {
		t.Skip("hardcoded python3 candidate exists, cannot test not-found path")
	}
	assert.True(t, strings.Contains(err.Error(), "python3 not found") || strings.Contains(err.Error(), "bandit"))
}

func TestRun_CommandError(t *testing.T) {
	fakePython := writeFakeScript(t, 1)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run(fakePython, "", out, []string{"test.py"})

	require.NoError(t, err)
	data, readErr := os.ReadFile(out)
	require.NoError(t, readErr)
	assert.Contains(t, string(data), `"executionSuccessful": false`)
}

func TestResolveBazelExternal_ExistingPath(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "python3")
	require.NoError(t, os.WriteFile(bin, []byte("x"), 0o755))

	got := resolveBazelExternal(bin)

	assert.Equal(t, bin, got)
}

func TestResolveBazelExternal_NonExistentPath(t *testing.T) {
	got := resolveBazelExternal("/nonexistent/python3")

	assert.Equal(t, "/nonexistent/python3", got)
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
