package main

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/gavelcode/gavel-tools/tools/archtest"
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
	resetFlags(t)
	setArgs(t, []string{"test", "--out", "/tmp/out.sarif", "file.rs"})

	code := execute()

	assert.Equal(t, 2, code)
}

func TestExecute_MissingOut(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "--config", "/tmp/arch.yml", "file.rs"})

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
	setArgs(t, []string{"test", "--config", "/nonexistent/arch.yml", "--out", filepath.Join(dir, "out.sarif"), "file.rs"})

	code := execute()

	assert.Equal(t, 1, code)
}

func TestExecute_Success(t *testing.T) {
	resetFlags(t)
	dir := t.TempDir()
	configPath := writeConfig(t, dir)
	srcDir := filepath.Join(dir, "src", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	srcFile := filepath.Join(srcDir, "test.rs")
	require.NoError(t, os.WriteFile(srcFile, []byte("fn main() {}\n"), 0o644))
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
	srcFile := filepath.Join(srcDir, "order.rs")
	require.NoError(t, os.WriteFile(srcFile, []byte("fn main() {}\n"), 0o644))
	outPath := filepath.Join(dir, "out", "results.sarif")

	err := run(configPath, outPath, []string{srcFile})

	require.NoError(t, err)
	_, statErr := os.Stat(outPath)
	assert.NoError(t, statErr)
}

func TestRun_MkdirAllError(t *testing.T) {
	dir := t.TempDir()
	configPath := writeConfig(t, dir)

	err := run(configPath, "/dev/null/impossible/out.sarif", []string{"file.rs"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output dir")
}

func TestRun_LoadConfigError(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out", "result.sarif")

	err := run("/nonexistent/arch.yml", outPath, []string{"file.rs"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "load config")
}

func TestRun_EvaluateFileError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)
	configPath := writeConfig(t, dir)
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "src", "domain"), 0o755))
	outPath := filepath.Join(dir, "out", "result.sarif")

	err := run(configPath, outPath, []string{"src/domain/nonexistent.rs"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "evaluate")
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

	err := run(configPath, filepath.Join(outDir, "result.sarif"), []string{srcFile})

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

	violations, err := evaluateFile(file, cfg)

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

	violations, err := evaluateFile("src/domain/handler.rs", cfg)

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

	violations, err := evaluateFile("src/domain/handler.rs", cfg)

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

	_, err := evaluateFile("src/domain/nonexistent.rs", cfg)

	require.Error(t, err)
}

func TestParseRustImports_CrateModule(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::domain::model;\n")

	imports, err := parseRustImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
	assert.Equal(t, 1, imports[0].Line)
}

func TestParseRustImports_CrateModuleWithItem(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::infrastructure::db::Repo;\n")

	imports, err := parseRustImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/infrastructure/db", imports[0].Path)
}

func TestParseRustImports_NestedUse(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::domain::{model, service};\n")

	imports, err := parseRustImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 2)
	assert.Equal(t, "src/domain/model", imports[0].Path)
	assert.Equal(t, "src/domain/service", imports[1].Path)
}

func TestParseRustImports_StdlibSkipped(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use std::collections::HashMap;\nuse core::fmt;\nuse alloc::vec::Vec;\n")

	imports, err := parseRustImports(file)

	require.NoError(t, err)
	assert.Empty(t, imports)
}

func TestParseRustImports_SuperModule(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain", "sub")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "handler.rs")
	require.NoError(t, os.WriteFile(file, []byte("use super::model;\n"), 0o644))

	imports, err := parseRustImports(file)

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

	imports, err := parseRustImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	expected := filepath.ToSlash(filepath.Join(dir, "model"))
	assert.Equal(t, expected, imports[0].Path)
}

func TestParseRustImports_BlockCommentSkipped(t *testing.T) {
	file := writeRustSource(t, "src/domain", "/* use crate::infrastructure::db; */\nuse crate::domain::model;\n")

	imports, err := parseRustImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
}

func TestParseRustImports_MultiLineBlockComment(t *testing.T) {
	file := writeRustSource(t, "src/domain", "/*\nuse crate::infrastructure::db;\n*/\nuse crate::domain::model;\n")

	imports, err := parseRustImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
}

func TestParseRustImports_LineCommentSkipped(t *testing.T) {
	file := writeRustSource(t, "src/domain", "// use crate::infrastructure::db;\nuse crate::domain::model;\n")

	imports, err := parseRustImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
}

func TestParseRustImports_ModDeclarationSkipped(t *testing.T) {
	file := writeRustSource(t, "src/domain", "mod tests;\nmod model;\nuse crate::domain::service;\n")

	imports, err := parseRustImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/service", imports[0].Path)
}

func TestParseRustImports_ExternalCrateSkipped(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use serde::Serialize;\nuse tokio::runtime::Runtime;\nuse crate::domain::model;\n")

	imports, err := parseRustImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
}

func TestParseRustImports_EmptyFile(t *testing.T) {
	file := writeRustSource(t, "src/domain", "")

	imports, err := parseRustImports(file)

	require.NoError(t, err)
	assert.Empty(t, imports)
}

func TestParseRustImports_NonexistentFile(t *testing.T) {
	_, err := parseRustImports("/nonexistent/file.rs")

	require.Error(t, err)
}

func TestParseRustImports_CrateSimpleModule(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::domain;\n")

	imports, err := parseRustImports(file)

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

	_, err := parseRustImports(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan")
}

func TestParseRustImports_NestedUseSelfSegment(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::domain::{self, model};\n")

	imports, err := parseRustImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/model", imports[0].Path)
}

func TestParseRustImports_NestedUseSubBrace(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::domain::{model::{Entity}, service};\n")

	imports, err := parseRustImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 2)
	assert.Equal(t, "src/domain/model", imports[0].Path)
	assert.Equal(t, "src/domain/service", imports[1].Path)
}

func TestParseRustImports_NestedUseSelfOnly(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::domain::{self};\n")

	imports, err := parseRustImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain", imports[0].Path)
}

func TestParseRustImports_NestedUseExternalCrate(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use serde::{Serialize, Deserialize};\n")

	imports, err := parseRustImports(file)

	require.NoError(t, err)
	assert.Empty(t, imports)
}

func TestParseRustImports_NestedUseLeadingColons(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::domain::{::sub};\n")

	imports, err := parseRustImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, "src/domain/sub", imports[0].Path)
}

func TestParseRustImports_NestedUseEmptySegment(t *testing.T) {
	file := writeRustSource(t, "src/domain", "use crate::domain::{, model};\n")

	imports, err := parseRustImports(file)

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
