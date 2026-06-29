package main

import (
	"encoding/json"
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

func savePythonBinary(t *testing.T) {
	t.Helper()
	orig := pythonBinary
	t.Cleanup(func() { pythonBinary = orig })
}

func writeFakeScript(t *testing.T, exitCode int, stderrLine string) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fake-python")
	var script string
	if stderrLine != "" {
		script = fmt.Sprintf("#!/bin/sh\necho '%s' >&2\nexit %d\n", stderrLine, exitCode)
	} else {
		script = fmt.Sprintf("#!/bin/sh\nexit %d\n", exitCode)
	}
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))
	return bin
}

func TestExecute_MissingOut(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test"})

	code := execute()

	assert.Equal(t, 2, code)
}

func TestExecute_Success(t *testing.T) {
	resetFlags(t)
	savePythonBinary(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "clean.py")
	require.NoError(t, os.WriteFile(file, []byte("x = 1\n"), 0o644))
	out := filepath.Join(dir, "out.sarif")
	fakePython := writeFakeScript(t, 0, "")
	setArgs(t, []string{"test", "--python", fakePython, "--out", out, file})

	code := execute()

	assert.Equal(t, 0, code)
}

func TestExecute_WriteSARIFError(t *testing.T) {
	resetFlags(t)
	savePythonBinary(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "clean.py")
	require.NoError(t, os.WriteFile(file, []byte("x = 1\n"), 0o644))
	fakePython := writeFakeScript(t, 0, "")
	setArgs(t, []string{"test", "--python", fakePython, "--out", "/dev/null/impossible/out.sarif", file})

	code := execute()

	assert.Equal(t, 1, code)
}

func TestAnalyze_CleanFile(t *testing.T) {
	savePythonBinary(t)
	pythonBinary = writeFakeScript(t, 0, "")
	dir := t.TempDir()
	file := filepath.Join(dir, "clean.py")
	require.NoError(t, os.WriteFile(file, []byte("x = 1\n"), 0o644))

	findings, _ := analyze([]string{file})

	assert.Empty(t, findings)
}

func TestAnalyze_FileWithEval(t *testing.T) {
	savePythonBinary(t)
	pythonBinary = writeFakeScript(t, 0, "")
	dir := t.TempDir()
	file := filepath.Join(dir, "eval.py")
	require.NoError(t, os.WriteFile(file, []byte("x = eval('1+1')\n"), 0o644))

	findings, _ := analyze([]string{file})

	require.Len(t, findings, 1)
	assert.Equal(t, "python/builtin-eval", findings[0].RuleID)
}

func TestCompileFindings_Clean(t *testing.T) {
	savePythonBinary(t)
	pythonBinary = writeFakeScript(t, 0, "")
	dir := t.TempDir()
	file := filepath.Join(dir, "clean.py")
	require.NoError(t, os.WriteFile(file, []byte("x = 1\n"), 0o644))

	findings, _ := compileFindings(file)

	assert.Nil(t, findings)
}

func TestCompileFindings_InterpreterFailure(t *testing.T) {
	savePythonBinary(t)
	pythonBinary = "/nonexistent/python3"
	dir := t.TempDir()
	file := filepath.Join(dir, "x.py")
	require.NoError(t, os.WriteFile(file, []byte("x = 1\n"), 0o644))

	findings, failure := compileFindings(file)

	assert.Empty(t, findings)
	assert.Contains(t, failure, "could not run the interpreter")
}

func TestCompileFindings_Error(t *testing.T) {
	savePythonBinary(t)
	pythonBinary = writeFakeScript(t, 1, `File "test.py", line 5`)
	dir := t.TempDir()
	file := filepath.Join(dir, "bad.py")
	require.NoError(t, os.WriteFile(file, []byte("invalid"), 0o644))

	findings, _ := compileFindings(file)

	require.Len(t, findings, 1)
	assert.Equal(t, "python/pycompile", findings[0].RuleID)
	assert.Equal(t, "error", findings[0].Level)
	assert.Equal(t, 5, findings[0].Line)
}

func TestEvalFindings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "example.py")
	require.NoError(t, os.WriteFile(path, []byte("def run(expression):\n    return eval(expression)\n"), 0o644))

	got := evalFindings(path)

	require.Len(t, got, 1)
	assert.Equal(t, "python/builtin-eval", got[0].RuleID)
	assert.Equal(t, 2, got[0].Line)
}

func TestEvalFindings_ReadError(t *testing.T) {
	got := evalFindings("/nonexistent/file.py")

	assert.Nil(t, got)
}

func TestParsePythonErrorLine(t *testing.T) {
	got := parsePythonErrorLine(`File "example.py", line 12`)

	assert.Equal(t, 12, got)
}

func TestParsePythonErrorLine_NoMatch(t *testing.T) {
	got := parsePythonErrorLine("no line number here")

	assert.Equal(t, 1, got)
}

func TestParsePythonErrorLine_AtoiOverflow(t *testing.T) {
	got := parsePythonErrorLine("line 99999999999999999999")

	assert.Equal(t, 1, got)
}

func TestResolvePython_ExplicitPath(t *testing.T) {
	got := resolvePython("/usr/bin/python3")

	assert.Equal(t, "/usr/bin/python3", got)
}

func TestResolvePython_FallsBackToLookPath(t *testing.T) {
	got := resolvePython("")

	assert.NotEmpty(t, got)
}

func TestResolvePython_FallbackWhenNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())

	got := resolvePython("")

	assert.Equal(t, "python3", got)
}

func TestWriteSARIF_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out", "result.sarif")
	testFindings := []finding{{
		RuleID:  "python/pycompile",
		Level:   "error",
		Message: "syntax error",
		File:    "test.py",
		Line:    5,
	}}

	err := writeSARIF(path, testFindings, sarif.Successful())

	require.NoError(t, err)
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	assert.Equal(t, "2.1.0", doc["version"])
	runs := doc["runs"].([]any)
	require.Len(t, runs, 1)
	results := runs[0].(map[string]any)["results"].([]any)
	assert.Len(t, results, 1)
}

func TestWriteSARIF_EmptyFindings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out", "result.sarif")

	err := writeSARIF(path, nil, sarif.Successful())

	require.NoError(t, err)
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	results := doc["runs"].([]any)[0].(map[string]any)["results"].([]any)
	assert.Empty(t, results)
}

func TestWriteSARIF_MkdirError(t *testing.T) {
	err := writeSARIF("/dev/null/impossible/out.sarif", nil, sarif.Successful())

	require.Error(t, err)
}
