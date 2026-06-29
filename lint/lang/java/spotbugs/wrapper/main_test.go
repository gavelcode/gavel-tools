package main

import (
	"encoding/json"
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
	bin := filepath.Join(dir, "fake-spotbugs")
	script := fmt.Sprintf(`#!/bin/sh
OUTPUT=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -output) OUTPUT="$2"; shift 2;;
    *) shift;;
  esac
done
if [ -n "$OUTPUT" ]; then
  echo '<?xml version="1.0" encoding="UTF-8"?><BugCollection version="test"></BugCollection>' > "$OUTPUT"
fi
exit %d
`, exitCode)
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))
	return bin
}

func TestExecute_MissingOut(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "app.jar"})

	code := execute()

	assert.Equal(t, 2, code)
}

func TestExecute_MissingJars(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "--out", "/tmp/out.sarif"})

	code := execute()

	assert.Equal(t, 2, code)
}

func TestExecute_RunError(t *testing.T) {
	resetFlags(t)
	t.Setenv("PATH", t.TempDir())
	setArgs(t, []string{"test", "--out", "/tmp/out.sarif", "app.jar"})

	code := execute()

	assert.Equal(t, 1, code)
}

func TestExecute_Success(t *testing.T) {
	sb := writeFakeScript(t, 0)
	resetFlags(t)
	out := filepath.Join(t.TempDir(), "out.sarif")
	setArgs(t, []string{"test", "--spotbugs", sb, "--out", out, "app.jar"})

	code := execute()

	assert.Equal(t, 0, code)
}

func TestRun_MkdirAllError(t *testing.T) {
	sb := writeFakeScript(t, 0)

	err := run(sb, "/dev/null/impossible/out.sarif", []string{"app.jar"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output dir")
}

func TestRun_SpotbugsNotFound(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run("", out, []string{"app.jar"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "spotbugs not found")
}

func TestRun_CommandError(t *testing.T) {
	sb := writeFakeScript(t, 1)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run(sb, out, []string{"app.jar"})

	require.NoError(t, err)
	data, readErr := os.ReadFile(out)
	require.NoError(t, readErr)
	assert.Contains(t, string(data), `"executionSuccessful": false`)
}

func TestRun_SpotbugsFoundInPath(t *testing.T) {
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "spotbugs")
	script := `#!/bin/sh
OUTPUT=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    -output) OUTPUT="$2"; shift 2;;
    *) shift;;
  esac
done
if [ -n "$OUTPUT" ]; then
  echo '<?xml version="1.0" encoding="UTF-8"?><BugCollection version="test"></BugCollection>' > "$OUTPUT"
fi
exit 0
`
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o755))
	t.Setenv("PATH", tmp)
	out := filepath.Join(t.TempDir(), "out", "result.sarif")

	err := run("", out, []string{"app.jar"})

	require.NoError(t, err)
}

func TestReadXML_ValidFile(t *testing.T) {
	content := `<?xml version="1.0" encoding="UTF-8"?>
<BugCollection version="4.9.8">
  <BugInstance type="NP_NULL" priority="1" category="CORRECTNESS">
    <ShortMessage>Null pointer</ShortMessage>
    <LongMessage>Possible null pointer dereference</LongMessage>
    <SourceLine sourcepath="Foo.java" start="10" end="10"/>
  </BugInstance>
</BugCollection>`
	tmp := t.TempDir()
	path := filepath.Join(tmp, "spotbugs.xml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))

	doc, err := readXML(path)

	require.NoError(t, err)
	assert.Equal(t, "4.9.8", doc.Version)
	require.Len(t, doc.Bugs, 1)
	assert.Equal(t, "NP_NULL", doc.Bugs[0].Type)
	assert.Equal(t, "Foo.java", doc.Bugs[0].SourceLine[0].SourcePath)
}

func TestReadXML_MissingFile(t *testing.T) {
	_, err := readXML("/nonexistent/path.xml")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read spotbugs xml")
}

func TestReadXML_InvalidXML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.xml")
	require.NoError(t, os.WriteFile(path, []byte("not xml at all {{{"), 0o644))

	_, err := readXML(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode spotbugs xml")
}

func TestToSARIF_StructuresOutput(t *testing.T) {
	doc := bugCollection{
		Version: "4.9.8",
		Bugs: []bugInstance{
			{
				Type: "NP_NULL_ON_SOME_PATH", Priority: "1", Category: "CORRECTNESS",
				Short: "Null deref", LongMessage: "Possible null pointer dereference of x",
				SourceLine: []sourceLine{{SourcePath: "Foo.java", Start: 10, End: 10}},
			},
		},
	}

	sarif := toSARIF(doc)

	assert.Equal(t, "2.1.0", sarif.Version)
	require.Len(t, sarif.Runs, 1)
	run := sarif.Runs[0]
	assert.Equal(t, "SpotBugs", run.Tool.Driver.Name)
	assert.Equal(t, "4.9.8", run.Tool.Driver.Version)
	require.Len(t, run.Tool.Driver.Rules, 1)
	assert.Equal(t, "NP_NULL_ON_SOME_PATH", run.Tool.Driver.Rules[0].ID)
	assert.Equal(t, "CORRECTNESS", run.Tool.Driver.Rules[0].Name)
	require.Len(t, run.Results, 1)
	assert.Equal(t, "NP_NULL_ON_SOME_PATH", run.Results[0].RuleID)
}

func TestToSARIF_EmptyBugs(t *testing.T) {
	sarif := toSARIF(bugCollection{Version: "4.9.8"})

	require.Len(t, sarif.Runs, 1)
	assert.Empty(t, sarif.Runs[0].Results)
	assert.Empty(t, sarif.Runs[0].Tool.Driver.Rules)
}

func TestToSARIF_MultipleBugs(t *testing.T) {
	doc := bugCollection{
		Version: "4.8.0",
		Bugs: []bugInstance{
			{Type: "NP_NULL", Priority: "1", Category: "CORRECTNESS", Short: "Null", LongMessage: "Null deref",
				SourceLine: []sourceLine{{SourcePath: "Foo.java", Start: 1, End: 1}}},
			{Type: "DLS_DEAD_LOCAL_STORE", Priority: "2", Category: "STYLE", Short: "Dead store", LongMessage: "Dead store to x",
				SourceLine: []sourceLine{{SourcePath: "Bar.java", Start: 10, End: 12}}},
		},
	}

	sarif := toSARIF(doc)

	require.Len(t, sarif.Runs[0].Results, 2)
	assert.Equal(t, "error", sarif.Runs[0].Results[0].Level)
	assert.Equal(t, "warning", sarif.Runs[0].Results[1].Level)
}

func TestToResult_UsesLongMessage(t *testing.T) {
	bug := bugInstance{
		Type: "NP_NULL_ON_SOME_PATH", Priority: "1",
		LongMessage: "Possible null pointer dereference", Short: "Null dereference",
		SourceLine: []sourceLine{{SourcePath: "Foo.java", Start: 5, End: 5}},
	}

	result := toResult(bug)

	assert.Equal(t, "NP_NULL_ON_SOME_PATH", result.RuleID)
	assert.Equal(t, "error", result.Level)
	assert.Equal(t, "Possible null pointer dereference", result.Message.Text)
}

func TestToResult_FallsBackToShortMessage(t *testing.T) {
	bug := bugInstance{
		Type: "NP_NULL_ON_SOME_PATH", Priority: "2",
		Short: "Null dereference",
	}

	result := toResult(bug)

	assert.Equal(t, "Null dereference", result.Message.Text)
	assert.Equal(t, "warning", result.Level)
}

func TestLocationsFor_Empty(t *testing.T) {
	assert.Nil(t, locationsFor(nil))
}

func TestLocationsFor_EmptySourcePath(t *testing.T) {
	lines := []sourceLine{{SourcePath: "", Start: 1, End: 5}}

	assert.Nil(t, locationsFor(lines))
}

func TestLocationsFor_UsesFirstLine(t *testing.T) {
	lines := []sourceLine{
		{SourcePath: "com/example/Foo.java", Start: 10, End: 15},
		{SourcePath: "com/example/Bar.java", Start: 20, End: 25},
	}

	locs := locationsFor(lines)

	require.Len(t, locs, 1)
	assert.Equal(t, "com/example/Foo.java", locs[0].PhysicalLocation.ArtifactLocation.URI)
	assert.Equal(t, 10, locs[0].PhysicalLocation.Region.StartLine)
	assert.Equal(t, 15, locs[0].PhysicalLocation.Region.EndLine)
}

func TestLevelForPriority_Error(t *testing.T) {
	assert.Equal(t, "error", levelForPriority("1"))
}

func TestLevelForPriority_Warning(t *testing.T) {
	assert.Equal(t, "warning", levelForPriority("2"))
}

func TestLevelForPriority_Note(t *testing.T) {
	assert.Equal(t, "note", levelForPriority("3"))
}

func TestLevelForPriority_UnknownDefaultsToNote(t *testing.T) {
	assert.Equal(t, "note", levelForPriority("99"))
}

func TestRuleList(t *testing.T) {
	rules := map[string]sarifRule{
		"A": {ID: "A", ShortDescription: sarifMessage{Text: "rule A"}},
	}

	got := ruleList(rules)

	require.Len(t, got, 1)
	assert.Equal(t, "A", got[0].ID)
}

func TestRuleList_Empty(t *testing.T) {
	got := ruleList(map[string]sarifRule{})

	assert.Empty(t, got)
}

func TestWriteSARIF_CreatesValidFile(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "out.sarif")
	doc := sarifLog{Version: "2.1.0", Runs: []sarifRun{{
		Tool:    sarifTool{Driver: sarifDriver{Name: "SpotBugs"}},
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
	bin := filepath.Join(tmp, "spotbugs")
	require.NoError(t, os.WriteFile(bin, []byte("x"), 0o755))

	got := resolveBazelExternal(bin)

	assert.Equal(t, bin, got)
}

func TestResolveBazelExternal_NonExistentPath(t *testing.T) {
	got := resolveBazelExternal("/nonexistent/spotbugs")

	assert.Equal(t, "/nonexistent/spotbugs", got)
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
