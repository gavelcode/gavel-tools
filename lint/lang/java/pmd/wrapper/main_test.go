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
	bin := filepath.Join(dir, "fake-pmd")
	script := fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))
	return bin
}

func TestExecute_MissingOut(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "file.java"})

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
	setArgs(t, []string{"test", "--out", "/tmp/out.sarif", "file.java"})

	code := execute()

	assert.Equal(t, 1, code)
}

func TestExecute_Success(t *testing.T) {
	pmd := writeFakeScript(t, 0)
	resetFlags(t)
	out := filepath.Join(t.TempDir(), "out.sarif")
	setArgs(t, []string{"test", "--pmd", pmd, "--out", out, "Test.java"})

	code := execute()

	assert.Equal(t, 0, code)
}

func TestRun_MkdirAllError(t *testing.T) {
	pmd := writeFakeScript(t, 0)

	err := run(pmd, "/dev/null/impossible/out.sarif", []string{"Test.java"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output dir")
}

func TestRun_PmdNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run("", out, []string{"Test.java"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pmd not found")
}

func TestRun_PmdFoundInPath(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "pmd")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("PATH", tmp)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run("", out, []string{"Test.java"})

	require.NoError(t, err)
}

func TestRun_CommandError(t *testing.T) {
	pmd := writeFakeScript(t, 1)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run(pmd, out, []string{"Test.java"})

	require.Error(t, err)
}

func TestWriteFileList_CreatesFileWithPaths(t *testing.T) {
	files := []string{"/src/Foo.java", "/src/Bar.java"}

	path, err := writeFileList(files)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })

	body, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Contains(t, string(body), "/src/Foo.java\n")
	assert.Contains(t, string(body), "/src/Bar.java\n")
}

func TestWriteFileList_SingleFile(t *testing.T) {
	files := []string{"/src/Main.java"}

	path, err := writeFileList(files)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })

	body, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "/src/Main.java\n", string(body))
}

func TestWriteFileList_CreateError(t *testing.T) {
	t.Setenv("TMPDIR", "/dev/null/impossible")

	_, err := writeFileList([]string{"a.java"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create file list")
}

func TestCommandEnv_WithJAVA_HOME(t *testing.T) {
	t.Setenv("JAVA_HOME", "/usr/lib/jvm/java-17")

	got := commandEnv()

	found := false
	for _, item := range got {
		if item == "JAVA_HOME=/usr/lib/jvm/java-17" {
			found = true
		}
	}
	assert.True(t, found, "JAVA_HOME should be present")
}

func TestCommandEnv_WithoutJAVA_HOME(t *testing.T) {
	t.Setenv("JAVA_HOME", "")

	got := commandEnv()

	for _, item := range got {
		assert.False(t, len(item) > 10 && item[:10] == "JAVA_HOME=", "JAVA_HOME should not be present")
	}
}

func TestSanitizedEnv_FiltersJAVA_HOME(t *testing.T) {
	t.Setenv("JAVA_HOME", "/some/path")

	got := sanitizedEnv()

	for _, item := range got {
		assert.False(t, len(item) > 10 && item[:10] == "JAVA_HOME=", "JAVA_HOME should be filtered out")
	}
}

func TestLookupEnv_Found(t *testing.T) {
	env := []string{"HOME=/home/user", "JAVA_HOME=/usr/lib/jvm/java-17"}

	val, ok := lookupEnv(env, "JAVA_HOME")

	assert.True(t, ok)
	assert.Equal(t, "/usr/lib/jvm/java-17", val)
}

func TestLookupEnv_NotFound(t *testing.T) {
	env := []string{"HOME=/home/user", "PATH=/usr/bin"}

	val, ok := lookupEnv(env, "JAVA_HOME")

	assert.False(t, ok)
	assert.Equal(t, "", val)
}

func TestLookupEnv_EmptySlice(t *testing.T) {
	_, ok := lookupEnv([]string{}, "JAVA_HOME")

	assert.False(t, ok)
}

func TestLookupEnv_EmptyValue(t *testing.T) {
	env := []string{"JAVA_HOME="}

	val, ok := lookupEnv(env, "JAVA_HOME")

	assert.True(t, ok)
	assert.Equal(t, "", val)
}

func TestResolveBazelExternal_ExistingPath(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "pmd")
	require.NoError(t, os.WriteFile(bin, []byte("x"), 0o755))

	got := resolveBazelExternal(bin)

	assert.Equal(t, bin, got)
}

func TestResolveBazelExternal_NonExistentPath(t *testing.T) {
	got := resolveBazelExternal("/nonexistent/pmd")

	assert.Equal(t, "/nonexistent/pmd", got)
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
