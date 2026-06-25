package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/gavelcode/gavel-tools/lint/archtest"
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

func TestExecute_MissingConfig(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "--out", "/tmp/out.sarif", "file.go"})

	code := execute()

	assert.Equal(t, 2, code)
}

func TestExecute_MissingOut(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "--config", "/tmp/arch.yml", "file.go"})

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
	setArgs(t, []string{"test", "--config", "/nonexistent/arch.yml", "--out", filepath.Join(dir, "out.sarif"), "file.go"})

	code := execute()

	assert.Equal(t, 1, code)
}

func TestExecute_Success(t *testing.T) {
	resetFlags(t)
	dir := t.TempDir()
	configPath := filepath.Join(dir, "arch.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("layers:\n  domain:\n    - internal/domain/...\nrules: []\n"), 0o644))
	srcDir := filepath.Join(dir, "internal", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	goFile := filepath.Join(srcDir, "order.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package domain\n\nimport \"fmt\"\n\nvar _ = fmt.Println\n"), 0o644))
	outPath := filepath.Join(dir, "out", "result.sarif")
	setArgs(t, []string{"test", "--config", configPath, "--out", outPath, goFile})

	code := execute()

	assert.Equal(t, 0, code)
}

func TestRun_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "arch.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("layers: {}\nrules: []\n"), 0o644))

	err := run(configPath, "/dev/null/impossible/out.sarif", []string{"file.go"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output dir")
}

func TestRun_LoadConfigError(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out", "result.sarif")

	err := run("/nonexistent/arch.yml", outPath, []string{"file.go"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "load config")
}

func TestRun_EvaluateFileError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	configPath := filepath.Join(dir, "arch.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("layers:\n  domain:\n    - internal/domain/...\nrules: []\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "internal", "domain"), 0o755))
	outPath := filepath.Join(dir, "out", "result.sarif")

	err := run(configPath, outPath, []string{"internal/domain/missing.go"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "evaluate")
}

func TestRun_WriteSARIFError(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "arch.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("layers:\n  domain:\n    - internal/domain/...\nrules: []\n"), 0o644))
	srcFile := filepath.Join(dir, "test.go")
	require.NoError(t, os.WriteFile(srcFile, []byte("package foo\n"), 0o644))
	outDir := filepath.Join(dir, "out")
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	require.NoError(t, os.Chmod(outDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(outDir, 0o755) })

	err := run(configPath, filepath.Join(outDir, "result.sarif"), []string{srcFile})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write sarif")
}

func TestRun_ProducesValidSARIF(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	configPath := filepath.Join(dir, "arch.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("layers:\n  domain:\n    - internal/domain/...\n  infra:\n    - internal/infra/...\nrules:\n  - name: domain-no-infra\n    source: domain\n    deny: [infra]\n"), 0o644))
	srcDir := filepath.Join(dir, "internal", "domain", "order")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	goFile := filepath.Join(srcDir, "order.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package order\n\nimport \"fmt\"\n\nvar _ = fmt.Println\n"), 0o644))
	outFile := filepath.Join(dir, "out", "results.sarif")

	err := run(configPath, outFile, []string{"internal/domain/order/order.go"})

	require.NoError(t, err)
	data, readErr := os.ReadFile(outFile)
	require.NoError(t, readErr)
	var sarif map[string]any
	require.NoError(t, json.Unmarshal(data, &sarif))
	assert.Equal(t, "2.1.0", sarif["version"])
}

func TestEvaluateFile_SkipsUnmatchedLayer(t *testing.T) {
	file := writeGoSource(t, "package foo\n\nimport \"fmt\"\n\nvar _ = fmt.Println\n")
	cfg := archtest.Config{
		Layers: map[string][]string{"domain": {"internal/domain/..."}},
		Rules:  []archtest.Rule{{Name: "domain-isolation", Source: "domain", Deny: []string{"infra"}}},
	}

	violations, err := evaluateFile(file, cfg, "")

	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestEvaluateFile_MatchedLayerNoViolation(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	srcDir := filepath.Join(dir, "internal", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	goFile := filepath.Join(srcDir, "order.go")
	require.NoError(t, os.WriteFile(goFile, []byte("package domain\n\nimport \"fmt\"\n\nvar _ = fmt.Println\n"), 0o644))
	cfg := archtest.Config{
		Layers: map[string][]string{
			"domain": {"internal/domain/..."},
			"infra":  {"internal/infra/..."},
		},
		Rules: []archtest.Rule{{Name: "domain-no-infra", Source: "domain", Deny: []string{"infra"}}},
	}

	violations, err := evaluateFile("internal/domain/order.go", cfg, "")

	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestEvaluateFile_ParseError(t *testing.T) {
	cfg := archtest.Config{
		Layers: map[string][]string{"domain": {"internal/domain/..."}},
		Rules:  []archtest.Rule{},
	}

	_, err := evaluateFile("internal/domain/missing.go", cfg, "")

	require.Error(t, err)
}

func TestDetectModulePrefix_WithGoMod(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module github.com/example/myproject\n\ngo 1.25\n"), 0o644))

	got := detectModulePrefix()

	assert.Equal(t, "github.com/example/myproject", got)
}

func TestDetectModulePrefix_NoGoMod(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	got := detectModulePrefix()

	assert.Equal(t, "", got)
}

func TestDetectModulePrefix_GoModWithoutModule(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "go.mod"), []byte("go 1.25\n"), 0o644))

	got := detectModulePrefix()

	assert.Equal(t, "", got)
}

func TestParseGoImports_SingleImport(t *testing.T) {
	file := writeGoSource(t, "package foo\n\nimport \"fmt\"\n")

	imports, err := parseGoImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "fmt", imports[0].Path)
	assert.Equal(t, 3, imports[0].Line)
}

func TestParseGoImports_GroupedImports(t *testing.T) {
	file := writeGoSource(t, "package foo\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n")

	imports, err := parseGoImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 2)
	assert.Equal(t, "fmt", imports[0].Path)
	assert.Equal(t, 4, imports[0].Line)
	assert.Equal(t, "os", imports[1].Path)
	assert.Equal(t, 5, imports[1].Line)
}

func TestParseGoImports_NamedImport(t *testing.T) {
	file := writeGoSource(t, "package foo\n\nimport foo \"github.com/example/foo\"\n")

	imports, err := parseGoImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "github.com/example/foo", imports[0].Path)
}

func TestParseGoImports_DotImport(t *testing.T) {
	file := writeGoSource(t, "package foo\n\nimport . \"testing\"\n")

	imports, err := parseGoImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "testing", imports[0].Path)
}

func TestParseGoImports_BlankImport(t *testing.T) {
	file := writeGoSource(t, "package foo\n\nimport _ \"net/http/pprof\"\n")

	imports, err := parseGoImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "net/http/pprof", imports[0].Path)
}

func TestParseGoImports_CgoImportSkipped(t *testing.T) {
	file := writeGoSource(t, "package foo\n\nimport \"C\"\nimport \"fmt\"\n")

	imports, err := parseGoImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "fmt", imports[0].Path)
}

func TestParseGoImports_CgoInGroupSkipped(t *testing.T) {
	file := writeGoSource(t, "package foo\n\nimport (\n\t\"C\"\n\t\"fmt\"\n)\n")

	imports, err := parseGoImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "fmt", imports[0].Path)
}

func TestParseGoImports_CommentsIgnored(t *testing.T) {
	file := writeGoSource(t, "package foo\n\nimport (\n\t// this is a comment\n\t\"fmt\"\n\t\"os\" // inline comment\n)\n")

	imports, err := parseGoImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 2)
	assert.Equal(t, "fmt", imports[0].Path)
	assert.Equal(t, "os", imports[1].Path)
}

func TestParseGoImports_BlockCommentsIgnored(t *testing.T) {
	file := writeGoSource(t, "package foo\n\nimport (\n\t/* block comment */\n\t\"fmt\"\n\t/* multi-line\n\t   block comment */\n\t\"os\"\n)\n")

	imports, err := parseGoImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 2)
	assert.Equal(t, "fmt", imports[0].Path)
	assert.Equal(t, "os", imports[1].Path)
}

func TestParseGoImports_MultipleImportBlocks(t *testing.T) {
	file := writeGoSource(t, "package foo\n\nimport \"fmt\"\n\nimport (\n\t\"os\"\n\t\"strings\"\n)\n\nimport \"path/filepath\"\n")

	imports, err := parseGoImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 4)
	assert.Equal(t, "fmt", imports[0].Path)
	assert.Equal(t, "os", imports[1].Path)
	assert.Equal(t, "strings", imports[2].Path)
	assert.Equal(t, "path/filepath", imports[3].Path)
}

func TestParseGoImports_GroupedWithAliases(t *testing.T) {
	file := writeGoSource(t, "package foo\n\nimport (\n\t\"fmt\"\n\tmylog \"log\"\n\t. \"testing\"\n\t_ \"net/http/pprof\"\n)\n")

	imports, err := parseGoImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 4)
	assert.Equal(t, "fmt", imports[0].Path)
	assert.Equal(t, "log", imports[1].Path)
	assert.Equal(t, "testing", imports[2].Path)
	assert.Equal(t, "net/http/pprof", imports[3].Path)
}

func TestParseGoImports_EmptyFile(t *testing.T) {
	file := writeGoSource(t, "package foo\n")

	imports, err := parseGoImports(file)

	require.NoError(t, err)
	assert.Empty(t, imports)
}

func TestParseGoImports_NonexistentFile(t *testing.T) {
	_, err := parseGoImports("/nonexistent/file.go")

	require.Error(t, err)
}

func TestParseGoImports_ImportWithoutSpace(t *testing.T) {
	file := writeGoSource(t, "package foo\n\nimport(\n\t\"fmt\"\n)\n")

	imports, err := parseGoImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "fmt", imports[0].Path)
}

func TestParseGoImports_ScannerError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.go")
	longLine := "import \"" + strings.Repeat("a", 70000) + "\"\n"
	require.NoError(t, os.WriteFile(path, []byte(longLine), 0o644))

	_, err := parseGoImports(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan")
}

func TestParseGoImports_BlockCommentStartMidLine(t *testing.T) {
	file := writeGoSource(t, "package foo\n\nimport (\n\t\"fmt\" /* start comment\n\tstill comment\n\t*/\n\t\"os\"\n)\n")

	imports, err := parseGoImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 2)
	assert.Equal(t, "fmt", imports[0].Path)
	assert.Equal(t, "os", imports[1].Path)
}

func TestParseGoImports_ImportSpaceOpenParen(t *testing.T) {
	file := writeGoSource(t, "package foo\n\nimport (\n\t\"fmt\"\n)\n")

	imports, err := parseGoImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "fmt", imports[0].Path)
}

func TestExtractImportPath_SimpleQuoted(t *testing.T) {
	path, ok := extractImportPath(`"fmt"`)

	assert.True(t, ok)
	assert.Equal(t, "fmt", path)
}

func TestExtractImportPath_WithAlias(t *testing.T) {
	path, ok := extractImportPath(`foo "github.com/example/foo"`)

	assert.True(t, ok)
	assert.Equal(t, "github.com/example/foo", path)
}

func TestExtractImportPath_BlankImport(t *testing.T) {
	path, ok := extractImportPath(`_ "net/http/pprof"`)

	assert.True(t, ok)
	assert.Equal(t, "net/http/pprof", path)
}

func TestExtractImportPath_DotImport(t *testing.T) {
	path, ok := extractImportPath(`. "testing"`)

	assert.True(t, ok)
	assert.Equal(t, "testing", path)
}

func TestExtractImportPath_EmptyString(t *testing.T) {
	_, ok := extractImportPath("")

	assert.False(t, ok)
}

func TestExtractImportPath_NoQuotes(t *testing.T) {
	_, ok := extractImportPath("fmt")

	assert.False(t, ok)
}

func TestExtractImportPath_SingleQuoteOnly(t *testing.T) {
	_, ok := extractImportPath(`"fmt`)

	assert.False(t, ok)
}

func TestExtractImportPath_WithTrailingComment(t *testing.T) {
	path, ok := extractImportPath(`"os" // for file ops`)

	assert.True(t, ok)
	assert.Equal(t, "os", path)
}

func TestStripBlockComments_NoComments(t *testing.T) {
	assert.Equal(t, `"fmt"`, stripBlockComments(`"fmt"`))
}

func TestStripBlockComments_InlineComment(t *testing.T) {
	assert.Equal(t, ` "fmt"`, stripBlockComments(`/* alias */ "fmt"`))
}

func TestStripBlockComments_MultipleComments(t *testing.T) {
	assert.Equal(t, ` "fmt" `, stripBlockComments(`/* a */ "fmt" /* b */`))
}

func TestStripBlockComments_Unclosed(t *testing.T) {
	assert.Equal(t, `code /* unclosed`, stripBlockComments(`code /* unclosed`))
}

func writeGoSource(t *testing.T, source string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.go")
	require.NoError(t, os.WriteFile(path, []byte(source), 0o644))
	return path
}
