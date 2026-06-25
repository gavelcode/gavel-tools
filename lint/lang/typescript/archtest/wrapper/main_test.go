package main

import (
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
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	t.Cleanup(func() { _ = os.Chdir(origDir) })
}

func writeArchConfig(t *testing.T, dir string) string {
	t.Helper()
	configPath := filepath.Join(dir, "architecture.yml")
	require.NoError(t, os.WriteFile(configPath, []byte(`layers:
  domain:
    - src/domain/...
  infra:
    - src/infrastructure/...
rules:
  - name: domain-no-infra
    source: domain
    deny:
      - infra
`), 0o644))
	return configPath
}

func TestExecute_MissingConfig(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "--out", "/tmp/out.sarif", "file.ts"})

	code := execute()

	assert.Equal(t, 2, code)
}

func TestExecute_MissingOut(t *testing.T) {
	resetFlags(t)
	setArgs(t, []string{"test", "--config", "/tmp/arch.yml", "file.ts"})

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
	setArgs(t, []string{"test", "--config", "/nonexistent/arch.yml", "--out", "/tmp/out.sarif", "file.ts"})

	code := execute()

	assert.Equal(t, 1, code)
}

func TestExecute_Success(t *testing.T) {
	dir := t.TempDir()
	configPath := writeArchConfig(t, dir)
	srcFile := filepath.Join(dir, "app.ts")
	require.NoError(t, os.WriteFile(srcFile, []byte("const x = 1;\n"), 0o644))
	outPath := filepath.Join(dir, "out", "result.sarif")

	resetFlags(t)
	setArgs(t, []string{"test", "--config", configPath, "--out", outPath, srcFile})

	code := execute()

	assert.Equal(t, 0, code)
}

func TestParseTypeScriptImports_NamedImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "order.ts")
	require.NoError(t, os.WriteFile(file, []byte(`import { X } from './model'
`), 0o644))

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, filepath.ToSlash(filepath.Join(dir, "model")), imports[0].Path)
	assert.Equal(t, 1, imports[0].Line)
}

func TestParseTypeScriptImports_RelativeUp(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "order.ts")
	require.NoError(t, os.WriteFile(file, []byte(`import X from '../infrastructure/db'
`), 0o644))

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	expected := filepath.ToSlash(filepath.Join(filepath.Dir(dir), "infrastructure", "db"))
	assert.Equal(t, expected, imports[0].Path)
}

func TestParseTypeScriptImports_ExternalSkipped(t *testing.T) {
	file := writeTSSource(t, "src/domain", `import 'react'
import lodash from 'lodash'
`)

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	assert.Empty(t, imports)
}

func TestParseTypeScriptImports_TypeImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "order.ts")
	require.NoError(t, os.WriteFile(file, []byte(`import type { X } from './types'
`), 0o644))

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, filepath.ToSlash(filepath.Join(dir, "types")), imports[0].Path)
}

func TestParseTypeScriptImports_Require(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "order.ts")
	require.NoError(t, os.WriteFile(file, []byte(`const x = require('./utils')
`), 0o644))

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, filepath.ToSlash(filepath.Join(dir, "utils")), imports[0].Path)
}

func TestParseTypeScriptImports_RequireExternal(t *testing.T) {
	file := writeTSSource(t, "src/domain", `const x = require('lodash')
`)

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	assert.Empty(t, imports)
}

func TestParseTypeScriptImports_RequireRelativeUp(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "order.ts")
	require.NoError(t, os.WriteFile(file, []byte(`const x = require('../service')
`), 0o644))

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	expected := filepath.ToSlash(filepath.Join(filepath.Dir(dir), "service"))
	assert.Equal(t, expected, imports[0].Path)
}

func TestParseTypeScriptImports_MultiLineImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "order.ts")
	require.NoError(t, os.WriteFile(file, []byte(`import {
  Foo,
  Bar,
} from '../infrastructure/db';
`), 0o644))

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	expected := filepath.ToSlash(filepath.Join(filepath.Dir(dir), "infrastructure", "db"))
	assert.Equal(t, expected, imports[0].Path)
	assert.Equal(t, 1, imports[0].Line)
}

func TestParseTypeScriptImports_BlockCommentsSkipped(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "order.ts")
	require.NoError(t, os.WriteFile(file, []byte(`/* import { X } from './forbidden' */
import { Y } from './model'
`), 0o644))

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, filepath.ToSlash(filepath.Join(dir, "model")), imports[0].Path)
}

func TestParseTypeScriptImports_MultiLineBlockComments(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "order.ts")
	require.NoError(t, os.WriteFile(file, []byte(`/*
import { X } from './forbidden'
*/
import { Y } from './model'
`), 0o644))

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, filepath.ToSlash(filepath.Join(dir, "model")), imports[0].Path)
}

func TestParseTypeScriptImports_BlockCommentEndWithCode(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "order.ts")
	require.NoError(t, os.WriteFile(file, []byte(`/*
comment
*/ import { X } from './model'
`), 0o644))

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, filepath.ToSlash(filepath.Join(dir, "model")), imports[0].Path)
}

func TestParseTypeScriptImports_InlineBlockCommentBeforeImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "order.ts")
	require.NoError(t, os.WriteFile(file, []byte(`/* comment */ import { X } from './model'
`), 0o644))

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
}

func TestParseTypeScriptImports_BlockCommentStartMidLine(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "order.ts")
	require.NoError(t, os.WriteFile(file, []byte(`const x = 1; /* start of
multiline comment */
import { Y } from './model'
`), 0o644))

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, filepath.ToSlash(filepath.Join(dir, "model")), imports[0].Path)
}

func TestParseTypeScriptImports_DynamicImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "order.ts")
	require.NoError(t, os.WriteFile(file, []byte(`const lazy = import('./lazy')
`), 0o644))

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, filepath.ToSlash(filepath.Join(dir, "lazy")), imports[0].Path)
}

func TestParseTypeScriptImports_SideEffectImport(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "order.ts")
	require.NoError(t, os.WriteFile(file, []byte(`import './polyfill'
`), 0o644))

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, filepath.ToSlash(filepath.Join(dir, "polyfill")), imports[0].Path)
}

func TestParseTypeScriptImports_LineCommentSkipped(t *testing.T) {
	file := writeTSSource(t, "src/domain", `// import { X } from './forbidden'
`)

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	assert.Empty(t, imports)
}

func TestParseTypeScriptImports_EmptyFile(t *testing.T) {
	file := writeTSSource(t, "src/domain", ``)

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	assert.Empty(t, imports)
}

func TestParseTypeScriptImports_NonexistentFile(t *testing.T) {
	_, err := parseTypeScriptImports("/nonexistent/file.ts")

	require.Error(t, err)
}

func TestParseTypeScriptImports_DoubleQuotes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src", "domain")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "order.ts")
	require.NoError(t, os.WriteFile(file, []byte(`import { X } from "./model"
`), 0o644))

	imports, err := parseTypeScriptImports(file)

	require.NoError(t, err)
	require.Len(t, imports, 1)
	assert.Equal(t, filepath.ToSlash(filepath.Join(dir, "model")), imports[0].Path)
}

func TestParseTypeScriptImports_ScannerError(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "src")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	file := filepath.Join(dir, "big.ts")
	longLine := strings.Repeat("x", 70000)
	require.NoError(t, os.WriteFile(file, []byte(longLine), 0o644))

	_, err := parseTypeScriptImports(file)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scan")
}

func TestEvaluateFile_SkipsUnmatchedLayer(t *testing.T) {
	file := writeTSSource(t, "unmatched", `import { X } from './model'
`)

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

func TestEvaluateFile_MatchedLayerWithViolation(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	srcDir := filepath.Join("src", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	srcFile := filepath.Join(srcDir, "order.ts")
	require.NoError(t, os.WriteFile(srcFile, []byte(`import { DB } from '../infrastructure/db'
`), 0o644))

	cfg := archtest.Config{
		Layers: map[string][]string{
			"domain": {"src/domain/..."},
			"infra":  {"src/infrastructure/..."},
		},
		Rules: []archtest.Rule{
			{Name: "domain-no-infra", Source: "domain", Deny: []string{"infra"}},
		},
	}

	violations, err := evaluateFile(srcFile, cfg)

	require.NoError(t, err)
	assert.NotEmpty(t, violations)
}

func TestEvaluateFile_MatchedLayerNoViolation(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	srcDir := filepath.Join("src", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	srcFile := filepath.Join(srcDir, "order.ts")
	require.NoError(t, os.WriteFile(srcFile, []byte(`import { Model } from './model'
`), 0o644))

	cfg := archtest.Config{
		Layers: map[string][]string{
			"domain": {"src/domain/..."},
			"infra":  {"src/infrastructure/..."},
		},
		Rules: []archtest.Rule{
			{Name: "domain-no-infra", Source: "domain", Deny: []string{"infra"}},
		},
	}

	violations, err := evaluateFile(srcFile, cfg)

	require.NoError(t, err)
	assert.Empty(t, violations)
}

func TestEvaluateFile_FileOpenError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	srcDir := filepath.Join("src", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	srcFile := filepath.Join(srcDir, "unreadable.ts")
	require.NoError(t, os.WriteFile(srcFile, []byte("x"), 0o644))
	require.NoError(t, os.Chmod(srcFile, 0o000))
	t.Cleanup(func() { _ = os.Chmod(srcFile, 0o644) })

	cfg := archtest.Config{
		Layers: map[string][]string{
			"domain": {"src/domain/..."},
		},
		Rules: []archtest.Rule{},
	}

	_, err := evaluateFile(srcFile, cfg)

	require.Error(t, err)
}

func TestRun_FullFlow(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	configPath := writeArchConfig(t, dir)

	srcDir := filepath.Join("src", "domain", "order")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	srcFile := filepath.Join(srcDir, "order.ts")
	require.NoError(t, os.WriteFile(srcFile, []byte("const x = 42;\n"), 0o644))

	outPath := filepath.Join(dir, "out", "results.sarif")

	err := run(configPath, outPath, []string{srcFile})

	require.NoError(t, err)
	_, statErr := os.Stat(outPath)
	assert.NoError(t, statErr)
}

func TestRun_MkdirError(t *testing.T) {
	err := run("/tmp/arch.yml", "/dev/null/impossible/out.sarif", []string{"file.ts"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "create output dir")
}

func TestRun_LoadConfigError(t *testing.T) {
	dir := t.TempDir()
	outPath := filepath.Join(dir, "out", "result.sarif")

	err := run("/nonexistent/arch.yml", outPath, []string{"file.ts"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "load config")
}

func TestRun_EvaluateFileError(t *testing.T) {
	dir := t.TempDir()
	chdir(t, dir)

	configPath := writeArchConfig(t, dir)

	srcDir := filepath.Join("src", "domain")
	require.NoError(t, os.MkdirAll(srcDir, 0o755))
	srcFile := filepath.Join(srcDir, "unreadable.ts")
	require.NoError(t, os.WriteFile(srcFile, []byte("x"), 0o644))
	require.NoError(t, os.Chmod(srcFile, 0o000))
	t.Cleanup(func() { _ = os.Chmod(srcFile, 0o644) })

	outPath := filepath.Join(dir, "out", "result.sarif")

	err := run(configPath, outPath, []string{srcFile})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "evaluate")
}

func TestRun_WriteSARIFError(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "architecture.yml")
	require.NoError(t, os.WriteFile(configPath, []byte("layers:\n  domain:\n    - src/domain/...\nrules: []\n"), 0o644))

	srcFile := filepath.Join(dir, "app.ts")
	require.NoError(t, os.WriteFile(srcFile, []byte("const x = 1;\n"), 0o644))

	outDir := filepath.Join(dir, "out")
	require.NoError(t, os.MkdirAll(outDir, 0o755))
	require.NoError(t, os.Chmod(outDir, 0o555))
	t.Cleanup(func() { _ = os.Chmod(outDir, 0o755) })

	outPath := filepath.Join(outDir, "result.sarif")

	err := run(configPath, outPath, []string{srcFile})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "write sarif")
}

func TestResolveRelativeImport_Relative(t *testing.T) {
	resolved, ok := resolveRelativeImport("./model", "/project/src/domain")

	assert.True(t, ok)
	assert.Equal(t, filepath.ToSlash(filepath.Join("/project/src/domain", "model")), resolved)
}

func TestResolveRelativeImport_External(t *testing.T) {
	_, ok := resolveRelativeImport("react", "/project/src/domain")

	assert.False(t, ok)
}

func writeTSSource(t *testing.T, subdir, source string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), subdir)
	require.NoError(t, os.MkdirAll(dir, 0o755))
	path := filepath.Join(dir, "test.ts")
	require.NoError(t, os.WriteFile(path, []byte(source), 0o644))
	return path
}
