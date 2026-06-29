package main

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gavelcode/gavel-tools/lint/archtest"
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

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func TestExecute_MissingConfig(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "--out", "/tmp/out.sarif", "file.java"})

	code := execute()

	assert.Equal(t, 2, code)
}

func TestExecute_MissingOut(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "--config", "/tmp/arch.yml", "file.java"})

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
	setArgs(t, []string{"test", "--config", "/nonexistent/arch.yml", "--out", filepath.Join(dir, "out.sarif"), "file.java"})

	code := execute()

	assert.Equal(t, 1, code)
}

func TestExecute_Success(t *testing.T) {
	resetFlags(t)
	dir := t.TempDir()
	chdir(t, dir)
	configContent := "layers:\n  domain:\n    - src/main/java/**/domain/...\nrules:\n  - name: test-rule\n    source: domain\n    deny: []\n"
	configPath := filepath.Join(dir, "architecture.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(configContent), 0o644))
	srcDir := filepath.Join(dir, "src", "main", "java", "com", "example", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	javaFile := filepath.Join(srcDir, "Order.java")
	require.NoError(t, os.WriteFile(javaFile, []byte("package com.example.domain;\n\npublic class Order {}\n"), 0o644))
	relPath := "src/main/java/com/example/domain/Order.java"
	outPath := filepath.Join(dir, "out", "result.sarif")
	setArgs(t, []string{"test", "--config", configPath, "--out", outPath, relPath})

	code := execute()

	assert.Equal(t, 0, code)
}

func TestRun_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "arch.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("layers:\n  domain:\n    - src/domain/...\nrules: []\n"), 0o644))

	err := run(configPath, "/dev/null/impossible/out.sarif", []string{"file.java"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output dir")
}

func TestRun_LoadConfigError(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out", "result.sarif")

	err := run("/nonexistent/arch.yml", outPath, []string{"file.java"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "load config")
}

func TestRun_EvaluateFileError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	configPath := filepath.Join(dir, "arch.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("layers:\n  domain:\n    - src/main/java/**/domain/...\nrules: []\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src", "main", "java", "com", "example", "domain"), 0o755))
	outPath := filepath.Join(dir, "out", "result.sarif")

	err := run(configPath, outPath, []string{"src/main/java/com/example/domain/Missing.java"})

	require.NoError(t, err)
	data, readErr := os.ReadFile(outPath)
	require.NoError(t, readErr)
	assert.Contains(t, string(data), `"executionSuccessful": false`)
}

func TestRun_WriteSARIFError(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "arch.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("layers:\n  domain:\n    - src/domain/...\nrules: []\n"), 0o644))
	srcDir := filepath.Join(dir, "src", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	javaFile := filepath.Join(srcDir, "Order.java")
	require.NoError(t, os.WriteFile(javaFile, []byte("package com.example.domain;\n"), 0o644))
	outDir := filepath.Join(dir, "out")
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	require.NoError(t, os.Chmod(outDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(outDir, 0o755) })

	err := run(configPath, filepath.Join(outDir, "result.sarif"), []string{javaFile})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write sarif")
}

func TestRun_ProducesValidSARIF(t *testing.T) {
	javaContent := "package com.example.domain;\n\nimport com.example.infrastructure.Repo;\n\npublic class Order {}\n"
	configContent := "layers:\n  domain:\n    - \"src/main/java/**/domain/...\"\n  infrastructure:\n    - \"src/main/java/**/infrastructure/...\"\nrules:\n  - name: domain-no-infrastructure\n    source: domain\n    deny: [infrastructure]\n"

	dir := t.TempDir()
	chdir(t, dir)

	srcDir := filepath.Join(dir, "src", "main", "java", "com", "example", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	javaFile := filepath.Join(srcDir, "Order.java")
	require.NoError(t, os.WriteFile(javaFile, []byte(javaContent), 0o644))
	configFile := filepath.Join(dir, "architecture.yml")
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0o644))
	outFile := filepath.Join(dir, "out", "results.sarif")
	relPath, err := filepath.Rel(dir, javaFile)
	require.NoError(t, err)
	relPath = filepath.ToSlash(relPath)

	err = run(configFile, outFile, []string{relPath})

	require.NoError(t, err)
	data, readErr := os.ReadFile(outFile)
	require.NoError(t, readErr)
	var sarif map[string]any
	require.NoError(t, json.Unmarshal(data, &sarif))
	assert.Equal(t, "2.1.0", sarif["version"])
	runs := sarif["runs"].([]any)
	require.Len(t, runs, 1)
	results := runs[0].(map[string]any)["results"].([]any)
	require.Len(t, results, 1)
}

func TestEvaluateFile_SkipsUnmatchedLayer(t *testing.T) {
	dir := t.TempDir()
	javaFile := filepath.Join(dir, "Order.java")
	require.NoError(t, os.WriteFile(javaFile, []byte("package com.example;\n\nimport com.example.infra.Repo;\n"), 0o644))
	cfg := archtest.Config{
		Layers: map[string][]string{"domain": {"src/main/java/**/domain/..."}},
		Rules:  []archtest.Rule{{Name: "test", Source: "domain", Deny: []string{"infra"}}},
	}

	violations, err := evaluateFile(javaFile, cfg)

	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestEvaluateFile_MatchedLayerNoViolation(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	srcDir := filepath.Join(dir, "src", "main", "java", "com", "example", "application")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	javaFile := filepath.Join(srcDir, "Service.java")
	require.NoError(t, os.WriteFile(javaFile, []byte("package com.example.application;\n\nimport com.example.domain.Order;\n\npublic class Service {}\n"), 0o644))
	cfg := archtest.Config{
		Layers: map[string][]string{
			"domain":      {"src/main/java/**/domain/..."},
			"application": {"src/main/java/**/application/..."},
			"infra":       {"src/main/java/**/infrastructure/..."},
		},
		Rules: []archtest.Rule{{Name: "domain-no-infra", Source: "domain", Deny: []string{"infra"}}},
	}
	relPath := "src/main/java/com/example/application/Service.java"

	violations, err := evaluateFile(relPath, cfg)

	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestEvaluateFile_MatchedLayerWithViolation(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	srcDir := filepath.Join(dir, "src", "main", "java", "com", "example", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	javaFile := filepath.Join(srcDir, "Order.java")
	require.NoError(t, os.WriteFile(javaFile, []byte("package com.example.domain;\n\nimport com.example.infrastructure.Repo;\n\npublic class Order {}\n"), 0o644))
	cfg := archtest.Config{
		Layers: map[string][]string{
			"domain": {"src/main/java/**/domain/..."},
			"infra":  {"src/main/java/**/infrastructure/..."},
		},
		Rules: []archtest.Rule{{Name: "domain-no-infra", Source: "domain", Deny: []string{"infra"}}},
	}
	relPath := "src/main/java/com/example/domain/Order.java"

	violations, err := evaluateFile(relPath, cfg)

	require.NoError(t, err)
	require.NotEmpty(t, violations)
	assert.Equal(t, "domain-no-infra", violations[0].RuleName)
}

func TestEvaluateFile_ParseError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src", "main", "java", "com", "example", "domain"), 0o755))
	cfg := archtest.Config{
		Layers: map[string][]string{"domain": {"src/main/java/**/domain/..."}},
		Rules:  []archtest.Rule{},
	}

	_, err := evaluateFile("src/main/java/com/example/domain/Missing.java", cfg)

	require.Error(t, err)
}

func TestParseJavaImports(t *testing.T) {
	tests := []struct {
		name     string
		content  string
		expected []archtest.Import
	}{
		{
			name:     "regular import",
			content:  "package com.example.domain;\n\nimport com.example.domain.Order;\n",
			expected: []archtest.Import{{Path: "com/example/domain", Line: 3}},
		},
		{
			name:     "static import",
			content:  "package com.example.app;\n\nimport static com.example.domain.Order.create;\n",
			expected: []archtest.Import{{Path: "com/example/domain/Order", Line: 3}},
		},
		{
			name:     "wildcard import",
			content:  "package com.example.app;\n\nimport com.example.domain.*;\n",
			expected: []archtest.Import{{Path: "com/example/domain", Line: 3}},
		},
		{
			name: "multiple imports",
			content: "package com.example.app;\n\n" +
				"import com.example.domain.Order;\n" +
				"import com.example.infrastructure.Repo;\n",
			expected: []archtest.Import{
				{Path: "com/example/domain", Line: 3},
				{Path: "com/example/infrastructure", Line: 4},
			},
		},
		{
			name: "line comment before import",
			content: "package com.example.app;\n\n" +
				"// this is a comment\n" +
				"import com.example.domain.Order;\n",
			expected: []archtest.Import{{Path: "com/example/domain", Line: 4}},
		},
		{
			name: "block comment around import",
			content: "package com.example.app;\n\n" +
				"/* import com.example.infrastructure.Repo; */\n" +
				"import com.example.domain.Order;\n",
			expected: []archtest.Import{{Path: "com/example/domain", Line: 4}},
		},
		{
			name: "multiline block comment around import",
			content: "package com.example.app;\n\n" +
				"/*\n * import com.example.infrastructure.Repo;\n */\n" +
				"import com.example.domain.Order;\n",
			expected: []archtest.Import{{Path: "com/example/domain", Line: 6}},
		},
		{
			name: "stops at class declaration",
			content: "package com.example.app;\n\n" +
				"import com.example.domain.Order;\n\n" +
				"public class App {\n" +
				"    import com.example.infrastructure.Repo;\n}\n",
			expected: []archtest.Import{{Path: "com/example/domain", Line: 3}},
		},
		{
			name: "stops at annotation",
			content: "package com.example.app;\n\n" +
				"import com.example.domain.Order;\n\n" +
				"@SuppressWarnings(\"unused\")\npublic class App {}\n",
			expected: []archtest.Import{{Path: "com/example/domain", Line: 3}},
		},
		{
			name:     "no imports",
			content:  "package com.example.domain;\n\npublic class Order {}\n",
			expected: nil,
		},
		{
			name:     "empty file",
			content:  "",
			expected: nil,
		},
		{
			name: "blank lines between imports",
			content: "package com.example.app;\n\n" +
				"import com.example.domain.Order;\n\n" +
				"import com.example.domain.Customer;\n\npublic class App {}\n",
			expected: []archtest.Import{
				{Path: "com/example/domain", Line: 3},
				{Path: "com/example/domain", Line: 5},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			path := writeTemp(t, "Test.java", testCase.content)

			got, err := parseJavaImports(path)

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, got)
		})
	}
}

func TestParseJavaImports_NonexistentFile(t *testing.T) {
	_, err := parseJavaImports("/nonexistent/Test.java")

	require.Error(t, err)
}

func TestParseJavaImports_ScannerError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Huge.java")
	longLine := "import " + strings.Repeat("a", 70000) + ";\n"
	require.NoError(t, os.WriteFile(path, []byte(longLine), 0o644))

	_, err := parseJavaImports(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan")
}

func TestParseJavaImports_BlockCommentStartMidLine(t *testing.T) {
	content := "package com.example;\n\nimport com.example.domain.Order; /* start comment\nstill comment\n*/\nimport com.example.app.Service;\n"
	path := writeTemp(t, "Test.java", content)

	got, err := parseJavaImports(path)

	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "com/example/domain", got[0].Path)
	assert.Equal(t, "com/example/app", got[1].Path)
}

func TestParseJavaImports_CodeAfterBlockCommentClose(t *testing.T) {
	content := "package com.example;\n\n/* comment */import com.example.domain.Order;\n"
	path := writeTemp(t, "Test.java", content)

	got, err := parseJavaImports(path)

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "com/example/domain", got[0].Path)
}

func TestJavaImportToPackagePath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"fully qualified class", "com.example.domain.Order", "com/example/domain"},
		{"wildcard import", "com.example.domain.*", "com/example/domain"},
		{"deep package", "com.example.domain.model.Order", "com/example/domain/model"},
		{"single segment", "Order", "Order"},
		{"two segments", "domain.Order", "domain"},
		{"static member reference", "com.example.domain.Order.create", "com/example/domain/Order"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := javaImportToPackagePath(tt.input)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestExtractSourcePrefix(t *testing.T) {
	tests := []struct {
		name     string
		source   string
		expected string
	}{
		{"standard maven layout", "src/main/java/com/example/domain/Order.java", "src/main/java/"},
		{"with leading path", "modules/core/src/main/java/com/example/domain/Order.java", "modules/core/src/main/java/"},
		{"no java source root", "com/example/domain/Order.java", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSourcePrefix(tt.source)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestResolveImportPaths(t *testing.T) {
	imports := []archtest.Import{
		{Path: "com/example/infrastructure", Line: 3},
		{Path: "com/example/domain", Line: 4},
	}
	sourceFile := "src/main/java/com/example/application/Service.java"

	resolved := resolveImportPaths(imports, sourceFile)

	expected := []archtest.Import{
		{Path: "src/main/java/com/example/infrastructure", Line: 3},
		{Path: "src/main/java/com/example/domain", Line: 4},
	}
	assert.Equal(t, expected, resolved)
}

func TestResolveImportPaths_NoSourceRoot(t *testing.T) {
	imports := []archtest.Import{{Path: "com/example/domain", Line: 3}}
	sourceFile := "com/example/application/Service.java"

	resolved := resolveImportPaths(imports, sourceFile)

	expected := []archtest.Import{{Path: "com/example/domain", Line: 3}}
	assert.Equal(t, expected, resolved)
}

func TestIsJavaDeclaration(t *testing.T) {
	tests := []struct {
		name     string
		line     string
		expected bool
	}{
		{"public class", "public class Order {", true},
		{"class", "class Order {", true},
		{"interface", "interface OrderRepository {", true},
		{"enum", "enum Status {", true},
		{"record", "record OrderDTO() {", true},
		{"annotation", "@Override", true},
		{"abstract class", "abstract class Base {", true},
		{"final class", "final class Config {", true},
		{"private class", "private class Inner {", true},
		{"protected class", "protected class Impl {", true},
		{"import statement", "import com.example.Order;", false},
		{"package statement", "package com.example;", false},
		{"blank", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isJavaDeclaration(tt.line)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestStripInlineBlockComments(t *testing.T) {
	assert.Equal(t, "import com.example.Order;", stripInlineBlockComments("import com.example.Order;"))
	assert.Equal(t, "", stripInlineBlockComments("/* comment */"))
	assert.Equal(t, "import com.example.Order; ", stripInlineBlockComments("import com.example.Order; /* comment */"))
	assert.Equal(t, "before  after", stripInlineBlockComments("before /* mid */ after"))
	assert.Equal(t, "code /* unclosed", stripInlineBlockComments("code /* unclosed"))
}

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}
