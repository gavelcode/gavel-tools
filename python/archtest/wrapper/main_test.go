package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/gavelcode/gavel-tools/archtest"
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

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func writePythonSource(t *testing.T, subdir, source string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), subdir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "test.py")
	require.NoError(t, os.WriteFile(path, []byte(source), 0o644))
	return path
}

func writeConfig(t *testing.T, dir string) string {
	t.Helper()
	configPath := filepath.Join(dir, "architecture.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("layers:\n  domain:\n    - src/domain/...\n  infra:\n    - src/infrastructure/...\nrules:\n  - name: domain-no-infra\n    source: domain\n    deny:\n      - infra\n"), 0o644))
	return configPath
}

func TestExecute_MissingConfig(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "--out", "/tmp/out.sarif", "file.py"})

	code := execute()

	assert.Equal(t, 2, code)
}

func TestExecute_MissingOut(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "--config", "/tmp/arch.yml", "file.py"})

	code := execute()

	assert.Equal(t, 2, code)
}

func TestExecute_MissingFiles(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "--config", "/tmp/arch.yml", "--out", "/tmp/out.sarif"})

	code := execute()

	assert.Equal(t, 2, code)
}

func TestExecute_RunError(t *testing.T) {
	resetFlags(t)
	dir := t.TempDir()
	setArgs(t, []string{"test", "--config", "/nonexistent/arch.yml", "--out", filepath.Join(dir, "out.sarif"), "file.py"})

	code := execute()

	assert.Equal(t, 1, code)
}

func TestExecute_Success(t *testing.T) {
	resetFlags(t)
	dir := t.TempDir()
	configPath := writeConfig(t, dir)
	srcDir := filepath.Join(dir, "src", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	srcFile := filepath.Join(srcDir, "test.py")
	require.NoError(t, os.WriteFile(srcFile, []byte("import os\n"), 0o644))
	outPath := filepath.Join(dir, "out", "result.sarif")
	setArgs(t, []string{"test", "--config", configPath, "--out", outPath, srcFile})

	code := execute()

	assert.Equal(t, 0, code)
}

func TestRun_FullFlow(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir)
	srcDir := filepath.Join(dir, "src", "domain", "order")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	srcFile := filepath.Join(srcDir, "order.py")
	require.NoError(t, os.WriteFile(srcFile, []byte("import os\n"), 0o644))
	outPath := filepath.Join(dir, "out", "results.sarif")

	err := run(configPath, outPath, []string{srcFile})

	require.NoError(t, err)
	_, statErr := os.Stat(outPath)
	assert.NoError(t, statErr)
}

func TestRun_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir)

	err := run(configPath, "/dev/null/impossible/out.sarif", []string{"file.py"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output dir")
}

func TestRun_LoadConfigError(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out", "result.sarif")

	err := run("/nonexistent/arch.yml", outPath, []string{"file.py"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "load config")
}

func TestRun_EvaluateFileError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	configPath := writeConfig(t, dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src", "domain"), 0o755))
	outPath := filepath.Join(dir, "out", "result.sarif")

	err := run(configPath, outPath, []string{"src/domain/nonexistent.py"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "evaluate")
}

func TestRun_WriteSARIFError(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir)
	srcDir := filepath.Join(dir, "src", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	srcFile := filepath.Join(srcDir, "test.py")
	require.NoError(t, os.WriteFile(srcFile, []byte("import os\n"), 0o644))
	outDir := filepath.Join(dir, "out")
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	require.NoError(t, os.Chmod(outDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(outDir, 0o755) })

	err := run(configPath, filepath.Join(outDir, "result.sarif"), []string{srcFile})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write sarif")
}

func TestEvaluateFile_SkipsUnmatchedLayer(t *testing.T) {
	file := writePythonSource(t, "unmatched", "import src.domain.model\n")
	cfg := archtest.Config{
		Layers: map[string][]string{
			"domain": {"src/domain/..."},
		},
		Rules: []archtest.Rule{
			{Name: "domain-isolation", Source: "domain", Deny: []string{"infra"}},
		},
	}

	violations, err := evaluateFile(file, cfg)

	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestEvaluateFile_MatchedLayerNoViolation(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	srcDir := filepath.Join(dir, "src", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "handler.py"), []byte("import src.domain.model\n"), 0o644))
	cfg := archtest.Config{
		Layers: map[string][]string{
			"domain": {"src/domain/..."},
		},
		Rules: []archtest.Rule{
			{Name: "domain-no-infra", Source: "domain", Deny: []string{"infra"}},
		},
	}

	violations, err := evaluateFile("src/domain/handler.py", cfg)

	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestEvaluateFile_MatchedLayerWithViolation(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	srcDir := filepath.Join(dir, "src", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "handler.py"), []byte("from src.infrastructure.db import repo\n"), 0o644))
	cfg := archtest.Config{
		Layers: map[string][]string{
			"domain": {"src/domain/..."},
			"infra":  {"src/infrastructure/..."},
		},
		Rules: []archtest.Rule{
			{Name: "domain-no-infra", Source: "domain", Deny: []string{"infra"}},
		},
	}

	violations, err := evaluateFile("src/domain/handler.py", cfg)

	require.NoError(t, err)
	require.NotEmpty(t, violations)
}

func TestEvaluateFile_ParseError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src", "domain"), 0o755))
	cfg := archtest.Config{
		Layers: map[string][]string{
			"domain": {"src/domain/..."},
		},
		Rules: []archtest.Rule{},
	}

	_, err := evaluateFile("src/domain/nonexistent.py", cfg)

	require.Error(t, err)
}

func TestParsePythonImports_SimpleImport(t *testing.T) {
	file := writePythonSource(t, "src/domain", "import src.domain.model\n")

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
	assert.Equal(t, 1, imports[0].Line)
}

func TestParsePythonImports_FromImport(t *testing.T) {
	file := writePythonSource(t, "src/application", "from src.infrastructure.db import repo\n")

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/infrastructure/db", imports[0].Path)
	assert.Equal(t, 1, imports[0].Line)
}

func TestParsePythonImports_RelativeSingleDot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "service.py")
	require.NoError(t, os.WriteFile(file, []byte("from .model import Entity\n"), 0o644))

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, filepath.ToSlash(filepath.Join(dir, "model")), imports[0].Path)
}

func TestParsePythonImports_RelativeDoubleDot(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain", "sub")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "service.py")
	require.NoError(t, os.WriteFile(file, []byte("from ..utils import helper\n"), 0o644))

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	parentDir := filepath.Dir(dir)
	assert.Equal(t, filepath.ToSlash(filepath.Join(parentDir, "utils")), imports[0].Path)
}

func TestParsePythonImports_MultiLineParenImport(t *testing.T) {
	file := writePythonSource(t, "src/application", "from src.domain.model import (\n    Entity,\n    Value,\n)\n")

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
	assert.Equal(t, 1, imports[0].Line)
}

func TestParsePythonImports_CommentSkipped(t *testing.T) {
	file := writePythonSource(t, "src/domain", "# this is a comment\nimport src.domain.model\n")

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
	assert.Equal(t, 2, imports[0].Line)
}

func TestParsePythonImports_InlineCommentStripped(t *testing.T) {
	file := writePythonSource(t, "src/domain", "import src.domain.model  # for models\n")

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
}

func TestParsePythonImports_TripleQuotesSkipped(t *testing.T) {
	file := writePythonSource(t, "src/domain", "\"\"\"\nimport src.infrastructure.db\n\"\"\"\nimport src.domain.model\n")

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
}

func TestParsePythonImports_SingleQuoteTripleQuotes(t *testing.T) {
	file := writePythonSource(t, "src/domain", "'''\nimport src.infrastructure.db\n'''\nimport src.domain.model\n")

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
}

func TestParsePythonImports_StdlibImport(t *testing.T) {
	file := writePythonSource(t, "src/domain", "import os\nimport sys\nimport src.domain.model\n")

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 3)
	assert.Equal(t, "os", imports[0].Path)
	assert.Equal(t, "sys", imports[1].Path)
	assert.Equal(t, "src/domain/model", imports[2].Path)
}

func TestParsePythonImports_BlankLines(t *testing.T) {
	file := writePythonSource(t, "src/domain", "\nimport src.domain.model\n\nfrom src.domain.service import handler\n\n")

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 2)
	assert.Equal(t, "src/domain/model", imports[0].Path)
	assert.Equal(t, "src/domain/service", imports[1].Path)
}

func TestParsePythonImports_MultipleImportsOnOneLine(t *testing.T) {
	file := writePythonSource(t, "src/domain", "import os, sys, src.domain.model\n")

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 3)
	assert.Equal(t, "os", imports[0].Path)
	assert.Equal(t, "sys", imports[1].Path)
	assert.Equal(t, "src/domain/model", imports[2].Path)
}

func TestParsePythonImports_ImportWithAlias(t *testing.T) {
	file := writePythonSource(t, "src/domain", "import src.domain.model as m\n")

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
}

func TestParsePythonImports_NonexistentFile(t *testing.T) {
	_, err := parsePythonImports("/nonexistent/file.py")

	require.Error(t, err)
}

func TestParsePythonImports_EmptyFile(t *testing.T) {
	file := writePythonSource(t, "src/domain", "")

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	assert.Empty(t, imports)
}

func TestParsePythonImports_ScannerError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "huge.py")
	longLine := "import " + strings.Repeat("a", 70000) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(longLine), 0o644))

	_, err := parsePythonImports(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan")
}

func TestParsePythonImports_FromWithoutImport(t *testing.T) {
	file := writePythonSource(t, "src/domain", "from os\nimport src.domain.model\n")

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
}

func TestParsePythonImports_BackslashContinuation(t *testing.T) {
	file := writePythonSource(t, "src/domain", "from src.domain.model import \\\n    Entity\n)\nimport os\n")

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 2)
	assert.Equal(t, "src/domain/model", imports[0].Path)
	assert.Equal(t, "os", imports[1].Path)
}

func TestParsePythonImports_EmptyImportPart(t *testing.T) {
	file := writePythonSource(t, "src/domain", "import os,,sys\n")

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 2)
	assert.Equal(t, "os", imports[0].Path)
	assert.Equal(t, "sys", imports[1].Path)
}

func TestParsePythonImports_BareDotImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "mod.py")
	require.NoError(t, os.WriteFile(file, []byte("from . import Entity\n"), 0o644))

	imports, err := parsePythonImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, filepath.ToSlash(dir), imports[0].Path)
}

func TestDotsToSlashes(t *testing.T) {
	assert.Equal(t, "src/domain/model", dotsToSlashes("src.domain.model"))
	assert.Equal(t, "os", dotsToSlashes("os"))
	assert.Equal(t, "os/path", dotsToSlashes("os.path"))
}

func TestStripInlineComment(t *testing.T) {
	assert.Equal(t, "import os  ", stripInlineComment("import os  # comment"))
	assert.Equal(t, "import os", stripInlineComment("import os"))
	assert.Equal(t, `x = "has # inside"`, stripInlineComment(`x = "has # inside"`))
}

func TestStripInlineComment_EscapeInString(t *testing.T) {
	assert.Equal(t, `x = "escaped\"quote"  `, stripInlineComment(`x = "escaped\"quote"  # comment`))
}
