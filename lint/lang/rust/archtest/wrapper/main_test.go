package wrapper

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gavelcode/gavel-tools/lint/archtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func chdir(t *testing.T, dir string) {
	t.Helper()
	orig, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(orig) })
}

func writeRustSource(t *testing.T, subdir, source string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), subdir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "test.rs")
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
	toolArgs := []string{"--out", "/tmp/out.sarif", "file.rs"}

	code := Main(toolArgs)

	assert.Equal(t, 2, code)
}

func TestExecute_MissingOut(t *testing.T) {
	toolArgs := []string{"--config", "/tmp/arch.yml", "file.rs"}

	code := Main(toolArgs)

	assert.Equal(t, 2, code)
}

func TestExecute_MissingFiles(t *testing.T) {
	toolArgs := []string{"--config", "/tmp/arch.yml", "--out", "/tmp/out.sarif"}

	code := Main(toolArgs)

	assert.Equal(t, 2, code)
}

func TestExecute_RunError(t *testing.T) {
	dir := t.TempDir()
	toolArgs := []string{"--config", "/nonexistent/arch.yml", "--out", filepath.Join(dir, "out.sarif"), "file.rs"}

	code := Main(toolArgs)

	assert.Equal(t, 1, code)
}

func TestExecute_Success(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir)
	srcDir := filepath.Join(dir, "src", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	srcFile := filepath.Join(srcDir, "test.rs")
	require.NoError(t, os.WriteFile(srcFile, []byte("fn main() {}\n"), 0o644))
	outPath := filepath.Join(dir, "out", "result.sarif")
	toolArgs := []string{"--config", configPath, "--out", outPath, srcFile}

	code := Main(toolArgs)

	assert.Equal(t, 0, code)
}

func TestRun_FullFlow(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir)
	srcDir := filepath.Join(dir, "src", "domain", "order")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	srcFile := filepath.Join(srcDir, "order.rs")
	require.NoError(t, os.WriteFile(srcFile, []byte("fn main() {}\n"), 0o644))
	outPath := filepath.Join(dir, "out", "results.sarif")

	err := run(configPath, outPath, []string{srcFile}, resolver{})

	require.NoError(t, err)
	_, statErr := os.Stat(outPath)
	assert.NoError(t, statErr)
}

func TestRun_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir)

	err := run(configPath, "/dev/null/impossible/out.sarif", []string{"file.rs"}, resolver{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output dir")
}

func TestRun_LoadConfigError(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out", "result.sarif")

	err := run("/nonexistent/arch.yml", outPath, []string{"file.rs"}, resolver{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "load config")
}

func TestRun_EvaluateFileError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	configPath := writeConfig(t, dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src", "domain"), 0o755))
	outPath := filepath.Join(dir, "out", "result.sarif")

	err := run(configPath, outPath, []string{"src/domain/nonexistent.rs"}, resolver{})

	require.NoError(t, err)
	data, readErr := os.ReadFile(outPath)
	require.NoError(t, readErr)
	assert.Contains(t, string(data), `"executionSuccessful": false`)
}

func TestRun_WriteSARIFError(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir)
	srcDir := filepath.Join(dir, "src", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	srcFile := filepath.Join(srcDir, "test.rs")
	require.NoError(t, os.WriteFile(srcFile, []byte("fn main() {}\n"), 0o644))
	outDir := filepath.Join(dir, "out")
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	require.NoError(t, os.Chmod(outDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(outDir, 0o755) })

	err := run(configPath, filepath.Join(outDir, "result.sarif"), []string{srcFile}, resolver{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write sarif")
}

func TestEvaluateFile_SkipsUnmatchedLayer(t *testing.T) {
	file := writeRustSource(t, "unmatched", "use crate::domain::model;\n")
	cfg := archtest.Config{
		Layers: map[string][]string{
			"domain": {"src/domain/..."},
		},
		Rules: []archtest.Rule{
			{Name: "domain-isolation", Source: "domain", Deny: []string{"infra"}},
		},
	}

	violations, err := evaluateFile(file, cfg, resolver{})

	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestEvaluateFile_MatchedLayerNoViolation(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	srcDir := filepath.Join(dir, "src", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "handler.rs"), []byte("use crate::domain::model;\n"), 0o644))
	cfg := archtest.Config{
		Layers: map[string][]string{
			"domain": {"src/domain/..."},
		},
		Rules: []archtest.Rule{
			{Name: "domain-no-infra", Source: "domain", Deny: []string{"infra"}},
		},
	}

	violations, err := evaluateFile("src/domain/handler.rs", cfg, resolver{})

	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestEvaluateFile_MatchedLayerWithViolation(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	srcDir := filepath.Join(dir, "src", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "handler.rs"), []byte("use crate::infrastructure::db;\n"), 0o644))
	cfg := archtest.Config{
		Layers: map[string][]string{
			"domain": {"src/domain/..."},
			"infra":  {"src/infrastructure/..."},
		},
		Rules: []archtest.Rule{
			{Name: "domain-no-infra", Source: "domain", Deny: []string{"infra"}},
		},
	}

	violations, err := evaluateFile("src/domain/handler.rs", cfg, resolver{})

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

	_, err := evaluateFile("src/domain/nonexistent.rs", cfg, resolver{})

	require.Error(t, err)
}

func TestParseRustImports_CrateModule(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::domain::model;\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
	assert.Equal(t, 1, imports[0].Line)
}

func TestParseRustImports_CrateModuleWithItem(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::infrastructure::db::Repo;\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/infrastructure/db", imports[0].Path)
}

func TestParseRustImports_NestedUse(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::domain::{model, service};\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	require.Len(t, imports, 2)
	assert.Equal(t, "src/domain/model", imports[0].Path)
	assert.Equal(t, "src/domain/service", imports[1].Path)
}

func TestParseRustImports_StdlibSkipped(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use std::collections::HashMap;\nuse core::fmt;\nuse alloc::vec::Vec;\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	assert.Empty(t, imports)
}

func TestParseRustImports_SuperModule(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain", "sub")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "handler.rs")
	require.NoError(t, os.WriteFile(file, []byte("use super::model;\n"), 0o644))

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	require.Len(t, imports, 1)
	expected := filepath.ToSlash(filepath.Join(filepath.Dir(dir), "model"))
	assert.Equal(t, expected, imports[0].Path)
}

func TestParseRustImports_SelfModule(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "mod.rs")
	require.NoError(t, os.WriteFile(file, []byte("use self::model;\n"), 0o644))

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	require.Len(t, imports, 1)
	expected := filepath.ToSlash(filepath.Join(dir, "model"))
	assert.Equal(t, expected, imports[0].Path)
}

func TestParseRustImports_BlockCommentSkipped(t *testing.T) {
	file := writeRustSource(t, "src/domain", "/* use crate::infrastructure::db; */\nuse crate::domain::model;\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
}

func TestParseRustImports_MultiLineBlockComment(t *testing.T) {
	file := writeRustSource(t, "src/domain", "/*\nuse crate::infrastructure::db;\n*/\nuse crate::domain::model;\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
}

func TestParseRustImports_LineCommentSkipped(t *testing.T) {
	file := writeRustSource(t, "src/domain", "// use crate::infrastructure::db;\nuse crate::domain::model;\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
}

func TestParseRustImports_ModDeclarationSkipped(t *testing.T) {
	file := writeRustSource(t, "src/domain", "mod tests;\nmod model;\nuse crate::domain::service;\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/service", imports[0].Path)
}

func TestParseRustImports_ExternalCrateSkipped(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use serde::Serialize;\nuse tokio::runtime::Runtime;\nuse crate::domain::model;\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
}

func TestParseRustImports_EmptyFile(t *testing.T) {
	file := writeRustSource(t, "src/domain", "")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	assert.Empty(t, imports)
}

func TestParseRustImports_NonexistentFile(t *testing.T) {
	_, err := parseRustImports("/nonexistent/file.rs", resolver{})

	require.Error(t, err)
}

func TestParseRustImports_CrateSimpleModule(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::domain;\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain", imports[0].Path)
}

func TestParseRustImports_ScannerError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "huge.rs")
	longLine := "use crate::" + strings.Repeat("a", 70000) + ";\n"
	require.NoError(t, os.WriteFile(path, []byte(longLine), 0o644))

	_, err := parseRustImports(path, resolver{})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan")
}

func TestParseRustImports_NestedUseSelfSegment(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::domain::{self, model};\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
}

func TestParseRustImports_NestedUseSubBrace(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::domain::{model::{Entity}, service};\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	require.Len(t, imports, 2)
	assert.Equal(t, "src/domain/model", imports[0].Path)
	assert.Equal(t, "src/domain/service", imports[1].Path)
}

func TestParseRustImports_NestedUseSelfOnly(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::domain::{self};\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain", imports[0].Path)
}

func TestParseRustImports_NestedUseExternalCrate(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use serde::{Serialize, Deserialize};\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	assert.Empty(t, imports)
}

func TestParseRustImports_NestedUseLeadingColons(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::domain::{::sub};\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/sub", imports[0].Path)
}

func TestParseRustImports_NestedUseEmptySegment(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::domain::{, model};\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
}

func TestTrimToModule_TypeStripped(t *testing.T) {
	assert.Equal(t, "infrastructure::db", trimToModule("infrastructure::db::Repo"))
	assert.Equal(t, "domain::model", trimToModule("domain::model::Entity"))
}

func TestTrimToModule_ModuleKept(t *testing.T) {
	assert.Equal(t, "domain::model", trimToModule("domain::model"))
	assert.Equal(t, "domain", trimToModule("domain"))
}

func TestIsStdlibUse(t *testing.T) {
	assert.True(t, isStdlibUse("std::collections::HashMap"))
	assert.True(t, isStdlibUse("core::fmt"))
	assert.True(t, isStdlibUse("alloc::vec::Vec"))
	assert.False(t, isStdlibUse("crate::domain::model"))
	assert.False(t, isStdlibUse("serde::Serialize"))
}

const customerDomainPkg = "core/customer/domain"

func crateResolver() resolver {
	return resolver{
		pkg: "core/customer/application/command",
		crates: map[string]string{
			"floridas_customer_domain": customerDomainPkg,
		},
	}
}

func importPaths(imports []archtest.Import) []string {
	paths := make([]string, 0, len(imports))
	for _, imp := range imports {
		paths = append(paths, imp.Path)
	}
	return paths
}

func TestParseRustImports_CrossCrateUse(t *testing.T) {
	file := writeRustSource(t, "src", "use floridas_customer_domain::model::Customer;\n")

	imports, err := parseRustImports(file, crateResolver())

	require.NoError(t, err)
	assert.Equal(t, []string{"core/customer/domain/src/model"}, importPaths(imports))
}

func TestParseRustImports_CrossCrateUnknownCrateIgnored(t *testing.T) {
	file := writeRustSource(t, "src", "use serde::Serialize;\nuse other_crate::model::Thing;\n")

	imports, err := parseRustImports(file, crateResolver())

	require.NoError(t, err)
	assert.Empty(t, imports)
}

func TestParseRustImports_CrossCrateNested(t *testing.T) {
	file := writeRustSource(t, "src", "use floridas_customer_domain::{model, service::Repo};\n")

	imports, err := parseRustImports(file, crateResolver())

	require.NoError(t, err)
	assert.Equal(t, []string{"core/customer/domain/src/model", "core/customer/domain/src/service"}, importPaths(imports))
}

func TestParseRustImports_CrossCrateLeadingColons(t *testing.T) {
	file := writeRustSource(t, "src", "use ::floridas_customer_domain::model::Customer;\n")

	imports, err := parseRustImports(file, crateResolver())

	require.NoError(t, err)
	assert.Equal(t, []string{"core/customer/domain/src/model"}, importPaths(imports))
}

func TestParseRustImports_MultiLineUse(t *testing.T) {
	src := "use floridas_customer_domain::{\n    model::{Customer, CustomerId},\n    service::CustomerRepository,\n};\nuse crate::local;\n"
	file := writeRustSource(t, "src", src)

	imports, err := parseRustImports(file, crateResolver())

	require.NoError(t, err)
	assert.Equal(t, []string{
		"core/customer/domain/src/model",
		"core/customer/domain/src/model",
		"core/customer/domain/src/service",
		"core/customer/application/command/src/local",
	}, importPaths(imports))
	assert.Equal(t, 1, imports[0].Line)
	assert.Equal(t, 5, imports[3].Line)
}

func TestParseRustImports_MultiLineUseWithComments(t *testing.T) {
	src := "use floridas_customer_domain::{\n    // the model\n    model, /* inline */\n    service, // trailing\n};\n"
	file := writeRustSource(t, "src", src)

	imports, err := parseRustImports(file, crateResolver())

	require.NoError(t, err)
	assert.Equal(t, []string{"core/customer/domain/src/model", "core/customer/domain/src/service"}, importPaths(imports))
}

func TestParseRustImports_DeeplyNestedBraces(t *testing.T) {
	file := writeRustSource(t, "src", "use floridas_customer_domain::{a::{b::{C, D}, e}, f};\n")

	imports, err := parseRustImports(file, crateResolver())

	require.NoError(t, err)
	assert.Equal(t, []string{
		"core/customer/domain/src/a/b",
		"core/customer/domain/src/a/b",
		"core/customer/domain/src/a/e",
		"core/customer/domain/src/f",
	}, importPaths(imports))
}

func TestParseRustImports_TwoStatementsOnOneLine(t *testing.T) {
	file := writeRustSource(t, "src", "use crate::a; use crate::b;\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	assert.Equal(t, []string{"src/a", "src/b"}, importPaths(imports))
}

func TestParseRustImports_CrateUsesPackage(t *testing.T) {
	file := writeRustSource(t, "src", "use crate::model::Customer;\n")

	imports, err := parseRustImports(file, resolver{pkg: customerDomainPkg})

	require.NoError(t, err)
	assert.Equal(t, []string{"core/customer/domain/src/model"}, importPaths(imports))
}

func TestParseRustImports_CrateWithoutPackageStaysRelative(t *testing.T) {
	file := writeRustSource(t, "src", "use crate::model::Customer;\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	assert.Equal(t, []string{"src/model"}, importPaths(imports))
}

func TestParseRustImports_ReExports(t *testing.T) {
	src := "pub use crate::a::X;\npub(crate) use crate::b::Y;\npub(super) use crate::c::Z;\npub(in crate::x) use crate::d::W;\n"
	file := writeRustSource(t, "src", src)

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	assert.Equal(t, []string{"src/a", "src/b", "src/c", "src/d"}, importPaths(imports))
}

func TestParseRustImports_Alias(t *testing.T) {
	file := writeRustSource(t, "src", "use crate::a::X as Y;\nuse crate::{b as bee, c::{D as E}};\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	assert.Equal(t, []string{"src/a", "src/b", "src/c"}, importPaths(imports))
}

func TestParseRustImports_Glob(t *testing.T) {
	file := writeRustSource(t, "src", "use crate::a::b::*;\nuse floridas_customer_domain::model::*;\n")

	imports, err := parseRustImports(file, crateResolver())

	require.NoError(t, err)
	assert.Equal(t, []string{"core/customer/application/command/src/a/b", "core/customer/domain/src/model"}, importPaths(imports))
}

func TestParseRustImports_SuperGlobAndChain(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain", "sub")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "handler.rs")
	require.NoError(t, os.WriteFile(file, []byte("use super::*;\nuse super::super::x::Y;\n"), 0o644))

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	assert.Equal(t, []string{
		filepath.ToSlash(filepath.Dir(dir)),
		filepath.ToSlash(filepath.Join(filepath.Dir(filepath.Dir(dir)), "x")),
	}, importPaths(imports))
}

func TestEvaluateFile_CrossCrateViolation(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	srcDir := filepath.Join(dir, "core", "customer", "domain", "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "lib.rs"), []byte("use floridas_customer_postgres::repo::Pg;\n"), 0o644))
	cfg := archtest.Config{
		Layers: map[string][]string{
			"customer_domain": {"core/customer/domain/..."},
			"customer_infra":  {"core/customer/infrastructure/postgres/..."},
		},
		Rules: []archtest.Rule{
			{Name: "domain-no-infra", Source: "customer_domain", Deny: []string{"customer_infra"}},
		},
	}
	res := resolver{
		pkg:    customerDomainPkg,
		crates: map[string]string{"floridas_customer_postgres": "core/customer/infrastructure/postgres"},
	}

	violations, err := evaluateFile("core/customer/domain/src/lib.rs", cfg, res)

	require.NoError(t, err)
	require.Len(t, violations, 1)
	assert.Equal(t, "domain-no-infra", violations[0].RuleName)
}

func TestEvaluateFile_DuplicateImportsReportedOnce(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	srcDir := filepath.Join(dir, "src", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "h.rs"), []byte("use crate::infrastructure::db::{A, B};\n"), 0o644))
	cfg := archtest.Config{
		Layers: map[string][]string{
			"domain": {"src/domain/..."},
			"infra":  {"src/infrastructure/..."},
		},
		Rules: []archtest.Rule{{Name: "r", Source: "domain", Deny: []string{"infra"}}},
	}

	violations, err := evaluateFile("src/domain/h.rs", cfg, resolver{})

	require.NoError(t, err)
	assert.Len(t, violations, 1)
}

func TestCrateFlag_Set(t *testing.T) {
	flags := crateFlag{}

	require.NoError(t, flags.Set("my_crate=core/my/pkg"))

	assert.Equal(t, "core/my/pkg", flags["my_crate"])
	assert.Contains(t, flags.String(), "my_crate=core/my/pkg")
	require.Error(t, flags.Set("novalue"))
	require.Error(t, flags.Set("=pkg"))
}

func TestExecute_CrateAndPackageFlags(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	srcDir := filepath.Join(dir, "core", "customer", "domain", "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "lib.rs"), []byte("use floridas_customer_postgres::repo::Pg;\n"), 0o644))
	configPath := filepath.Join(dir, "architecture.yml")
	cfg := "layers:\n  customer_domain:\n    - core/customer/domain/...\n  customer_infra:\n    - core/customer/infrastructure/postgres/...\nrules:\n  - name: domain-no-infra\n    source: customer_domain\n    deny:\n      - customer_infra\n"
	require.NoError(t, os.WriteFile(configPath, []byte(cfg), 0o644))
	outPath := filepath.Join(dir, "out.sarif")
	toolArgs := []string{"--config", configPath, "--out", outPath,
		"--package", "core/customer/domain",
		"--crate", "floridas_customer_postgres=core/customer/infrastructure/postgres",
		"core/customer/domain/src/lib.rs"}

	assert.Equal(t, 0, Main(toolArgs))

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "domain-no-infra")
}

func TestParseRustImports_RootLevelTypeTrimsToModuleRoot(t *testing.T) {
	file := writeRustSource(t, "src", "use floridas_customer_domain::{Customer, CustomerError};\nuse crate::Foo;\nuse self::Bar;\n")

	imports, err := parseRustImports(file, crateResolver())

	require.NoError(t, err)
	assert.Equal(t, []string{
		"core/customer/domain/src",
		"core/customer/domain/src",
		"core/customer/application/command/src",
		filepath.ToSlash(filepath.Dir(file)),
	}, importPaths(imports))
}

func TestParseRustImports_SuperRootLevelType(t *testing.T) {
	file := writeRustSource(t, "src/sub", "use super::Foo;\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	assert.Equal(t, []string{filepath.ToSlash(filepath.Dir(filepath.Dir(file)))}, importPaths(imports))
}

func TestEvaluateFile_RootLevelTypesReportedOnce(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	srcDir := filepath.Join(dir, "core", "customer", "domain", "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(srcDir, "lib.rs"), []byte("use customer_postgres::{Pg, PgError};\n"), 0o644))
	cfg := archtest.Config{
		Layers: map[string][]string{
			"domain": {"core/customer/domain/..."},
			"infra":  {"core/customer/infrastructure/postgres/..."},
		},
		Rules: []archtest.Rule{{Name: "r", Source: "domain", Deny: []string{"infra"}}},
	}
	res := resolver{crates: map[string]string{"customer_postgres": "core/customer/infrastructure/postgres"}}

	violations, err := evaluateFile("core/customer/domain/src/lib.rs", cfg, res)

	require.NoError(t, err)
	assert.Len(t, violations, 1)
}

func TestTrimToModule_RootLevelType(t *testing.T) {
	assert.Equal(t, "", trimToModule("Foo"))
	assert.Equal(t, "domain", trimToModule("domain"))
}

func TestParseRustImports_UnterminatedVisibilityIsNotAUse(t *testing.T) {
	file := writeRustSource(t, "src", "pub(crate use crate::a::b;\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	assert.Empty(t, imports)
}

func TestParseRustImports_GroupedGlobImportsTheGroupPrefix(t *testing.T) {
	file := writeRustSource(t, "src", "use crate::model::{*};\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	assert.Equal(t, []string{"src/model"}, importPaths(imports))
}

func TestParseRustImports_NestedGroupWithoutHeadKeepsPrefix(t *testing.T) {
	file := writeRustSource(t, "src", "use crate::model::{{Item}};\n")

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	assert.Equal(t, []string{"src/model"}, importPaths(imports))
}

func TestParseRustImports_BareSuperChainClimbsEveryLevel(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain", "sub")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "handler.rs")
	require.NoError(t, os.WriteFile(file, []byte("use super::super;\n"), 0o644))

	imports, err := parseRustImports(file, resolver{})

	require.NoError(t, err)
	assert.Equal(t, []string{filepath.ToSlash(filepath.Dir(filepath.Dir(dir)))}, importPaths(imports))
}
