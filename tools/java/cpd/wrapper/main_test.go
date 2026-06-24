package main

import (
	"encoding/json"
	"encoding/xml"
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
	script := fmt.Sprintf("#!/bin/sh\necho '<?xml version=\"1.0\" encoding=\"UTF-8\"?><pmd-cpd></pmd-cpd>'\nexit %d\n", exitCode)
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

	err := run(pmd, "/dev/null/impossible/out.sarif", 100, []string{"Test.java"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output dir")
}

func TestRun_PmdNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run("", out, 100, []string{"Test.java"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "pmd not found")
}

func TestRun_PmdFoundInPath(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "pmd")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\necho '<?xml version=\"1.0\" encoding=\"UTF-8\"?><pmd-cpd></pmd-cpd>'\nexit 0\n"), 0o755))
	t.Setenv("PATH", tmp)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run("", out, 100, []string{"Test.java"})

	require.NoError(t, err)
}

func TestRun_CommandError(t *testing.T) {
	pmd := writeFakeScript(t, 1)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run(pmd, out, 100, []string{"Test.java"})

	require.Error(t, err)
}

func TestRun_CreateXMLOutputError(t *testing.T) {
	pmd := writeFakeScript(t, 0)
	dir := t.TempDir()
	outDir := filepath.Join(dir, "out")
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	require.NoError(t, os.Chmod(outDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(outDir, 0o755) })

	err := run(pmd, filepath.Join(outDir, "result.sarif"), 100, []string{"Test.java"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create xml output")
}

func TestReadCPDXML_ValidFile(t *testing.T) {
	content := `<?xml version="1.0" encoding="UTF-8"?>
<pmd-cpd>
  <duplication lines="6" tokens="75">
    <file line="10" path="src/Foo.java"/>
    <file line="20" path="src/Bar.java"/>
    <codefragment><![CDATA[public void doSomething() { }]]></codefragment>
  </duplication>
</pmd-cpd>`
	path := filepath.Join(t.TempDir(), "cpd.xml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	doc, err := readCPDXML(path)

	require.NoError(t, err)
	require.Len(t, doc.Duplications, 1)
	assert.Equal(t, 6, doc.Duplications[0].Lines)
}

func TestReadCPDXML_MissingFile(t *testing.T) {
	_, err := readCPDXML("/nonexistent/cpd.xml")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "read cpd xml")
}

func TestReadCPDXML_InvalidXML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.xml")
	require.NoError(t, os.WriteFile(path, []byte("not xml"), 0o644))

	_, err := readCPDXML(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode cpd xml")
}

func TestParseCPDXML(t *testing.T) {
	raw := `<?xml version="1.0" encoding="UTF-8"?>
<pmd-cpd>
  <duplication lines="6" tokens="75">
    <file line="10" path="src/Foo.java"/>
    <file line="20" path="src/Bar.java"/>
    <codefragment><![CDATA[public void doSomething() { }]]></codefragment>
  </duplication>
</pmd-cpd>`

	var doc pmdCPD
	require.NoError(t, xml.Unmarshal([]byte(raw), &doc))

	require.Len(t, doc.Duplications, 1)
	assert.Equal(t, 6, doc.Duplications[0].Lines)
	assert.Equal(t, 75, doc.Duplications[0].Tokens)
	require.Len(t, doc.Duplications[0].Files, 2)
	assert.Equal(t, 10, doc.Duplications[0].Files[0].Line)
	assert.Equal(t, "src/Foo.java", doc.Duplications[0].Files[0].Path)
}

func TestToSARIF(t *testing.T) {
	doc := pmdCPD{
		Duplications: []duplication{
			{
				Lines: 6, Tokens: 75,
				Files: []cpdFile{{Line: 10, Path: "src/Foo.java"}, {Line: 20, Path: "src/Bar.java"}},
			},
			{
				Lines: 3, Tokens: 40,
				Files: []cpdFile{{Line: 1, Path: "src/Baz.java"}, {Line: 5, Path: "src/Qux.java"}},
			},
		},
	}

	got := toSARIF(doc)

	assert.Equal(t, "2.1.0", got.Version)
	require.Len(t, got.Runs, 1)
	assert.Equal(t, "CPD", got.Runs[0].Tool.Driver.Name)
	require.Len(t, got.Runs[0].Results, 2)
	first := got.Runs[0].Results[0]
	assert.Equal(t, "cpd/duplicate-code", first.RuleID)
	assert.Equal(t, "warning", first.Level)
	assert.Contains(t, first.Message.Text, "6 lines")
	assert.Contains(t, first.Message.Text, "75 tokens")
	require.Len(t, first.Locations, 2)
}

func TestToSARIFEmptyDuplications(t *testing.T) {
	got := toSARIF(pmdCPD{})

	require.Len(t, got.Runs, 1)
	assert.Empty(t, got.Runs[0].Results)
}

func TestToResultMessage(t *testing.T) {
	dup := duplication{
		Lines: 10, Tokens: 120,
		Files: []cpdFile{
			{Line: 1, Path: "src/main/java/com/example/Alpha.java"},
			{Line: 50, Path: "src/main/java/com/example/Beta.java"},
		},
	}

	got := toResult(dup)

	assert.Equal(t, "Duplicated block of 10 lines (120 tokens) across Alpha.java, Beta.java.", got.Message.Text)
}

func TestWriteSARIF_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.sarif")
	doc := sarifLog{Version: "2.1.0", Runs: []sarifRun{{
		Tool:    sarifTool{Driver: sarifDriver{Name: "CPD"}},
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

	_, ok := lookupEnv(env, "JAVA_HOME")

	assert.False(t, ok)
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
