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

func TestPackagePath(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"main root", "src/main/java/com/example/domain", "com/example/domain"},
		{"module prefix", "modules/core/src/main/java/com/example/domain", "com/example/domain"},
		{"test root", "core/src/test/java/com/example/domain", "com/example/domain"},
		{"pattern with wildcard", "core/src/main/java/**/domain/...", "**/domain/..."},
		{"default package", "core/src/main/java", ""},
		{"no source root", "com/example/domain", "com/example/domain"},
		{"wildcard pattern without root", "**/com/example/...", "**/com/example/..."},
		{"lookalike directory is not a root", "xsrc/main/java/com/example", "xsrc/main/java/com/example"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, packagePath(tt.path))
		})
	}
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

func writeJava(t *testing.T, root, relPath, content string) string {
	t.Helper()
	full := filepath.Join(root, relPath)
	require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o755))
	require.NoError(t, os.WriteFile(full, []byte(content), 0o644))
	return relPath
}

func TestEvaluateFile_MultiModuleCrossModuleImportIsReported(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)
	source := writeJava(t, root, "core/users/domain/src/main/java/com/acme/users/domain/Order.java",
		"package com.acme.users.domain;\n\nimport com.acme.platform.app.Service;\n\npublic class Order {}\n")
	writeJava(t, root, "core/platform/app/src/main/java/com/acme/platform/app/Service.java",
		"package com.acme.platform.app;\n\npublic class Service {}\n")
	cfg := archtest.Config{
		Layers: map[string][]string{
			"users-domain": {"com/acme/users/domain/..."},
			"platform-app": {"com/acme/platform/app/..."},
		},
		Rules: []archtest.Rule{{Name: "users-domain-no-platform-app", Source: "users-domain", Deny: []string{"platform-app"}}},
	}

	violations, err := evaluateFile(source, cfg)

	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.Equal(t, "users-domain-no-platform-app", violations[0].RuleName)
}

func TestEvaluateFile_MultiModuleWildcardPatternsReportCrossModuleImport(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)
	source := writeJava(t, root, "core/users/domain/src/main/java/com/acme/users/domain/Order.java",
		"package com.acme.users.domain;\n\nimport com.acme.platform.app.Service;\n")
	cfg := archtest.Config{
		Layers: map[string][]string{
			"users-domain": {"**/com/acme/users/domain/..."},
			"platform-app": {"**/com/acme/platform/app/..."},
		},
		Rules: []archtest.Rule{{Name: "r", Source: "users-domain", Deny: []string{"platform-app"}}},
	}

	violations, err := evaluateFile(source, cfg)

	require.NoError(t, err)
	assert.Len(t, violations, 1)
}

func legacyRutivaConfig() archtest.Config {
	return archtest.Config{
		Layers: map[string][]string{
			"domain": {
				"core/src/main/java/com/rutiva/club/domain/...",
				"com/rutiva/club/domain/...",
			},
			"infra": {
				"core/src/main/java/com/rutiva/club/infrastructure/...",
				"com/rutiva/club/infrastructure/...",
			},
			"server": {
				"apps/server/src/main/java/com/rutiva/...",
				"com/rutiva/...",
			},
		},
		Rules: []archtest.Rule{{Name: "domain-no-infra", Source: "domain", Deny: []string{"infra"}}},
	}
}

func TestEvaluateFile_LegacyPathPrefixedPatternsStillDenyImports(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)
	source := writeJava(t, root, "core/src/main/java/com/rutiva/club/domain/model/Club.java",
		"package com.rutiva.club.domain.model;\n\nimport com.rutiva.club.infrastructure.Repo;\n")

	violations, err := evaluateFile(source, legacyRutivaConfig())

	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.Equal(t, "domain-no-infra", violations[0].RuleName)
}

func TestEvaluateFile_LegacyPathPrefixedPatternsStillAllowImports(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)
	domain := writeJava(t, root, "core/src/main/java/com/rutiva/club/domain/model/Club.java",
		"package com.rutiva.club.domain.model;\n\nimport com.rutiva.club.domain.shared.Id;\nimport java.util.List;\n")
	server := writeJava(t, root, "apps/server/src/main/java/com/rutiva/server/Main.java",
		"package com.rutiva.server;\n\nimport com.rutiva.club.domain.model.Club;\nimport com.rutiva.club.infrastructure.Repo;\n")

	domainViolations, err := evaluateFile(domain, legacyRutivaConfig())
	require.NoError(t, err)
	serverViolations, err := evaluateFile(server, legacyRutivaConfig())
	require.NoError(t, err)

	assert.Empty(t, domainViolations)
	assert.Empty(t, serverViolations)
}

func TestRun_ModuleOnlyPatternFailsLoudly(t *testing.T) {
	root := t.TempDir()
	chdir(t, root)
	source := writeJava(t, root, "core/platform/application/src/main/java/com/acme/platform/application/Svc.java",
		"package com.acme.platform.application;\n\nimport com.acme.users.domain.Order;\n")
	config := "layers:\n  platform_application:\n    - core/platform/application/src/main/java/...\n  users_domain:\n    - com/acme/users/domain/...\nrules:\n  - name: r\n    source: platform_application\n    deny: [users_domain]\n"
	configPath := filepath.Join(root, "architecture.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(config), 0o644))

	err := run(configPath, filepath.Join(root, "out.sarif"), []string{source})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `layer "platform_application"`)
	assert.Contains(t, err.Error(), "core/platform/application/src/main/java/...")
	assert.Contains(t, err.Error(), "selects a module, not a package")
}

func TestValidateLayers(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		wantErr bool
	}{
		{"module only recursive", "core/app/src/main/java/...", true},
		{"module only wildcard", "core/app/src/test/java/**/...", true},
		{"module only bare root", "core/app/src/main/java", true},
		{"legacy package under root", "core/app/src/main/java/com/acme/...", false},
		{"package space", "com/acme/app/...", false},
		{"wildcard package", "**/com/acme/app/...", false},
		{"no root, wildcard only", "**/...", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateLayers(map[string][]string{"layer": {tt.pattern}})
			assert.Equal(t, tt.wantErr, err != nil)
		})
	}
}
