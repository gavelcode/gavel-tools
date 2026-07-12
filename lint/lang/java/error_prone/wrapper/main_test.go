package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gavelcode/gavel-tools/lint/sarif"
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
	bin := filepath.Join(dir, "fake-javac")
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
	t.Setenv("JAVA_HOME", "")
	setArgs(t, []string{"test", "--out", "/tmp/out.sarif", "file.java"})

	code := execute()

	assert.Equal(t, 1, code)
}

func TestExecute_Success(t *testing.T) {
	javac := writeFakeScript(t, 0)
	epJar := filepath.Join(t.TempDir(), "ep.jar")
	require.NoError(t, os.WriteFile(epJar, []byte("fake"), 0o644))
	resetFlags(t)
	out := filepath.Join(t.TempDir(), "out.sarif")
	setArgs(t, []string{"test", "--javac", javac, "--error-prone-jar", epJar, "--out", out, "Test.java"})

	code := execute()

	assert.Equal(t, 0, code)
}

func TestRun_MkdirAllError(t *testing.T) {
	javac := writeFakeScript(t, 0)
	epJar := filepath.Join(t.TempDir(), "ep.jar")
	require.NoError(t, os.WriteFile(epJar, []byte("fake"), 0o644))

	err := run(epJar, "", javac, "/dev/null/impossible/out.sarif", "", []string{"Test.java"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output dir")
}

func TestRun_JavacNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("JAVA_HOME", "")
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run("ep.jar", "", "", out, "", []string{"Test.java"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "javac not found")
}

func TestRun_WithDataflowJar(t *testing.T) {
	javac := writeFakeScript(t, 0)
	epJar := filepath.Join(t.TempDir(), "ep.jar")
	require.NoError(t, os.WriteFile(epJar, []byte("fake"), 0o644))
	dfJar := filepath.Join(t.TempDir(), "df.jar")
	require.NoError(t, os.WriteFile(dfJar, []byte("fake"), 0o644))
	out := filepath.Join(t.TempDir(), "out.sarif")

	err := run(epJar, dfJar, javac, out, "", []string{"Test.java"})

	require.NoError(t, err)
}

func TestRun_WithClasspath(t *testing.T) {
	javac := writeFakeScript(t, 0)
	epJar := filepath.Join(t.TempDir(), "ep.jar")
	require.NoError(t, os.WriteFile(epJar, []byte("fake"), 0o644))
	out := filepath.Join(t.TempDir(), "out.sarif")

	err := run(epJar, "", javac, out, "/path/to/dep.jar", []string{"Test.java"})

	require.NoError(t, err)
}

func TestParseDiagnostics(t *testing.T) {
	stderr := `src/Foo.java:10: warning: [DeadException] Exception created but not thrown
    new RuntimeException("oops");
        ^
src/Foo.java:25: error: [MustBeClosedChecker] This resource must be closed
    InputStream is = new FileInputStream("f");
                     ^
Note: Some messages have been simplified.
2 warnings
`

	got := parseDiagnostics(stderr)

	require.Len(t, got, 2)
	assert.Equal(t, "src/Foo.java", got[0].File)
	assert.Equal(t, 10, got[0].Line)
	assert.Equal(t, "warning", got[0].Level)
	assert.Equal(t, "DeadException", got[0].RuleID)
	assert.Equal(t, "Exception created but not thrown", got[0].Message)
	assert.Equal(t, 25, got[1].Line)
	assert.Equal(t, "error", got[1].Level)
	assert.Equal(t, "MustBeClosedChecker", got[1].RuleID)
}

func TestParseDiagnosticsEmpty(t *testing.T) {
	got := parseDiagnostics("")

	assert.Empty(t, got)
}

func TestParseDiagnosticsSkipsNonMatching(t *testing.T) {
	stderr := "    new RuntimeException(\"oops\");\n        ^\n2 warnings\n"

	got := parseDiagnostics(stderr)

	assert.Empty(t, got)
}

func TestParseDiagnostics_NoteLevel(t *testing.T) {
	stderr := "src/Foo.java:5: note: [SomeCheck] Something\n"

	got := parseDiagnostics(stderr)

	require.Len(t, got, 1)
	assert.Equal(t, "note", got[0].Level)
}

func TestToSARIF(t *testing.T) {
	findings := []finding{
		{File: "src/Foo.java", Line: 10, Level: "warning", RuleID: "DeadException", Message: "Exception created but not thrown"},
		{File: "src/Bar.java", Line: 5, Level: "error", RuleID: "MustBeClosedChecker", Message: "This resource must be closed"},
	}

	got := toSARIF(findings, sarif.Successful())

	assert.Equal(t, "2.1.0", got.Version)
	require.Len(t, got.Runs, 1)
	assert.Equal(t, "ErrorProne", got.Runs[0].Tool.Driver.Name)
	require.Len(t, got.Runs[0].Results, 2)
	first := got.Runs[0].Results[0]
	assert.Equal(t, "DeadException", first.RuleID)
	assert.Equal(t, "warning", first.Level)
	require.Len(t, first.Locations, 1)
	assert.Equal(t, "src/Foo.java", first.Locations[0].PhysicalLocation.ArtifactLocation.URI)
}

func TestToSARIFEmpty(t *testing.T) {
	got := toSARIF(nil, sarif.Successful())

	require.Len(t, got.Runs, 1)
	assert.Empty(t, got.Runs[0].Results)
	assert.Empty(t, got.Runs[0].Tool.Driver.Rules)
}

func TestJavacLevelToSARIF(t *testing.T) {
	assert.Equal(t, "error", javacLevelToSARIF("error"))
	assert.Equal(t, "warning", javacLevelToSARIF("warning"))
	assert.Equal(t, "note", javacLevelToSARIF("note"))
	assert.Equal(t, "note", javacLevelToSARIF("other"))
}

func TestBuildJavacArgs(t *testing.T) {
	args := buildJavacArgs("/path/to/ep.jar:/path/to/df.jar", "/path/to/dep.jar", "/tmp/out", "/tmp/files.txt")

	assert.Contains(t, args, "-XDcompilePolicy=simple")
	assert.Contains(t, args, "-processorpath")
	assert.Contains(t, args, "/path/to/ep.jar:/path/to/df.jar")
	assert.Contains(t, args, "-Xplugin:ErrorProne")
	assert.Contains(t, args, "-classpath")
	assert.Contains(t, args, "/path/to/dep.jar")
	assert.Contains(t, args, "@/tmp/files.txt")
}

func TestBuildJavacArgsNoClasspath(t *testing.T) {
	args := buildJavacArgs("/path/to/ep.jar", "", "/tmp/out", "/tmp/files.txt")

	assert.NotContains(t, args, "-classpath")
}

func TestFindJavac_UsesJAVA_HOME(t *testing.T) {
	t.Setenv("JAVA_HOME", "/nonexistent/java")
	t.Setenv("PATH", t.TempDir())

	_, err := findJavac()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "javac not found")
}

func TestFindJavac_FallsBackToPath(t *testing.T) {
	t.Setenv("JAVA_HOME", "")

	javac, err := findJavac()
	if err != nil {
		t.Skip("javac not available in this environment")
	}
	assert.NotEmpty(t, javac)
}

func TestFindJavac_JAVA_HOME_Candidate(t *testing.T) {
	dir := t.TempDir()
	binDir := filepath.Join(dir, "bin")
	require.NoError(t, os.MkdirAll(binDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "javac"), []byte("#!/bin/sh\nexit 0\n"), 0o755))
	t.Setenv("JAVA_HOME", dir)

	javac, err := findJavac()

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "bin", "javac"), javac)
}

func TestWriteFileList(t *testing.T) {
	files := []string{"/src/Foo.java", "/src/Bar.java"}

	path, err := writeFileList(files)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Remove(path) })

	body, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Contains(t, string(body), "/src/Foo.java\n")
	assert.Contains(t, string(body), "/src/Bar.java\n")
}

func TestWriteFileList_CreateError(t *testing.T) {
	t.Setenv("TMPDIR", "/dev/null/impossible")

	_, err := writeFileList([]string{"a.java"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create file list")
}

func TestWriteSARIF_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.sarif")
	doc := sarifLog{Version: "2.1.0", Runs: []sarifRun{{
		Tool:    sarifTool{Driver: sarifDriver{Name: "ErrorProne"}},
		Results: []sarifResult{},
	}}}

	err := writeSARIF(path, doc)

	require.NoError(t, err)
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, "2.1.0", parsed["version"])
}

func TestWriteSARIF_CreateError(t *testing.T) {
	err := writeSARIF("/dev/null/impossible/out.sarif", sarifLog{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create sarif")
}

func TestResolveBazelExternal_ExistingPath(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "javac")
	require.NoError(t, os.WriteFile(bin, []byte("x"), 0o755))

	got := resolveBazelExternal(bin)

	assert.Equal(t, bin, got)
}

func TestResolveBazelExternal_NonExistentPath(t *testing.T) {
	got := resolveBazelExternal("/nonexistent/javac")

	assert.Equal(t, "/nonexistent/javac", got)
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

func TestRuleList(t *testing.T) {
	rules := map[string]sarifRule{
		"A": {ID: "A", ShortDescription: sarifMessage{Text: "rule A"}},
	}

	got := ruleList(rules)

	require.Len(t, got, 1)
	assert.Equal(t, "A", got[0].ID)
}

func TestDetectCompilerErrors(t *testing.T) {
	stderr := `src/Foo.java:118: error: package org.junit does not exist
import org.junit.Test;
                ^
src/Foo.java:25: error: [MustBeClosedChecker] This resource must be closed
src/Foo.java:40: error: cannot find symbol
1 error`

	got := detectCompilerErrors(stderr)

	require.Len(t, got, 2)
	assert.Contains(t, got[0], "package org.junit does not exist")
	assert.Contains(t, got[1], "cannot find symbol")
}

func TestDetectCompilerErrors_OnlyErrorProneFindings(t *testing.T) {
	stderr := "src/Foo.java:25: error: [MustBeClosedChecker] must be closed\nsrc/Foo.java:10: warning: [DeadException] x\n"

	assert.Empty(t, detectCompilerErrors(stderr))
}

func TestExecutionInvocation_CleanRun(t *testing.T) {
	inv := executionInvocation(nil, nil)

	assert.True(t, inv.ExecutionSuccessful)
	assert.Empty(t, inv.ToolExecutionNotifications)
}

func TestExecutionInvocation_CompileErrorsAreDegradedNotFailed(t *testing.T) {
	inv := executionInvocation(nil, []string{"Foo.java:1: error: package x does not exist"})

	assert.True(t, inv.ExecutionSuccessful,
		"Error Prone ran; a target that will not fully compile (annotation processors) is incomplete, not a tool failure that should gate the verdict")
	require.Len(t, inv.ToolExecutionNotifications, 1)
	assert.Equal(t, "warning", inv.ToolExecutionNotifications[0].Level)
	assert.Contains(t, inv.ToolExecutionNotifications[0].Message.Text, "incomplete")
}

func TestExecutionInvocation_LaunchFailureIsHardFailure(t *testing.T) {
	inv := executionInvocation(errors.New("exec format error"), nil)

	assert.False(t, inv.ExecutionSuccessful)
	require.NotEmpty(t, inv.ToolExecutionNotifications)
	assert.Equal(t, "error", inv.ToolExecutionNotifications[0].Level)
}

func TestExecutionInvocation_LaunchFailureKeepsCompileContext(t *testing.T) {
	inv := executionInvocation(errors.New("exec format error"), []string{"Foo.java:1: error: package x does not exist"})

	assert.False(t, inv.ExecutionSuccessful,
		"a javac that could not launch is a hard failure even when compile errors were also detected")
	require.Len(t, inv.ToolExecutionNotifications, 2)
	assert.Contains(t, inv.ToolExecutionNotifications[1].Message.Text, "incomplete")
}

func TestToSARIF_Invocation(t *testing.T) {
	ok := toSARIF(nil, sarif.Successful())
	require.Len(t, ok.Runs[0].Invocations, 1)
	assert.True(t, ok.Runs[0].Invocations[0].ExecutionSuccessful)

	failed := toSARIF(nil, sarif.Failed("3 javac errors prevented analysis"))
	require.Len(t, failed.Runs[0].Invocations, 1)
	assert.False(t, failed.Runs[0].Invocations[0].ExecutionSuccessful)
	require.Len(t, failed.Runs[0].Invocations[0].ToolExecutionNotifications, 1)
	assert.Equal(t, "error", failed.Runs[0].Invocations[0].ToolExecutionNotifications[0].Level)
}
