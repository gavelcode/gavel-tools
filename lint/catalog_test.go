package catalog_test

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

type catalogFile struct {
	Version    int                    `yaml:"version"`
	AspectsBzl string                 `yaml:"aspects_bzl"`
	Languages  map[string][]toolEntry `yaml:"languages"`
}

type toolEntry struct {
	Name        string    `yaml:"name"`
	Module      string    `yaml:"module"`
	AspectsBzl  string    `yaml:"aspects_bzl"`
	Aspect      string    `yaml:"aspect"`
	SARIFSuffix string    `yaml:"sarif_suffix"`
	BuildFlags  []string  `yaml:"build_flags"`
	Binary      string    `yaml:"binary"`
	Variants    []variant `yaml:"variants"`
}

// variant is an alternative way to run a tool, selected by a Bazel module the
// consumer already depends on (see the catalog.yaml header).
type variant struct {
	WhenBazelDep string   `yaml:"when_bazel_dep"`
	Module       string   `yaml:"module"`
	AspectsBzl   string   `yaml:"aspects_bzl"`
	Aspect       string   `yaml:"aspect"`
	BuildFlags   []string `yaml:"build_flags"`
}

func loadCatalog(t *testing.T) catalogFile {
	t.Helper()
	data, err := os.ReadFile("catalog.yaml")
	require.NoError(t, err)
	var catalog catalogFile
	require.NoError(t, yaml.Unmarshal(data, &catalog))
	return catalog
}

// exportedAspects extracts the public aspect names defs.bzl re-binds, which is
// the actual set a consumer can invoke.
func exportedAspects(t *testing.T) []string {
	t.Helper()
	defs, err := os.ReadFile("aspects/defs.bzl")
	require.NoError(t, err)
	re := regexp.MustCompile(`(?m)^([a-z_]+_submission_aspect) = `)
	var exported []string
	for _, match := range re.FindAllStringSubmatch(string(defs), -1) {
		exported = append(exported, match[1])
	}
	return exported
}

func TestCatalog_ListsExactlyTheExportedAspects(t *testing.T) {
	catalog := loadCatalog(t)

	var catalogAspects []string
	for _, tools := range catalog.Languages {
		for _, tool := range tools {
			catalogAspects = append(catalogAspects, tool.Aspect)
		}
	}

	exported := exportedAspects(t)
	sort.Strings(catalogAspects)
	sort.Strings(exported)
	assert.Equal(t, exported, catalogAspects,
		"catalog.yaml must list exactly the aspects exported by defs.bzl — no drift")
}

func TestCatalog_ClippyCapturesOutputInsteadOfFailingTheBuild(t *testing.T) {
	catalog := loadCatalog(t)

	var clippy toolEntry
	for _, tool := range catalog.Languages["rust"] {
		if tool.Name == "clippy" {
			clippy = tool
		}
	}
	require.Equal(t, "clippy", clippy.Name, "rust/clippy must exist in the catalog")

	assert.Contains(t, clippy.BuildFlags, "--@rules_rust//rust/settings:capture_clippy_output=True",
		"without capture, rules_rust runs Clippy as a check that fails the build on findings")
	assert.Contains(t, clippy.BuildFlags, "--@rules_rust//rust/settings:clippy_output_diagnostics=True",
		"the submission aspect reads the captured diagnostics to produce SARIF")
}

func TestCatalog_GoStdlibFlagUsesCanonicalRulesGoName(t *testing.T) {
	catalog := loadCatalog(t)

	var golangci toolEntry
	for _, tool := range catalog.Languages["go"] {
		if tool.Name == "golangci-lint" {
			golangci = tool
		}
	}
	require.Equal(t, "golangci-lint", golangci.Name, "go/golangci-lint must exist in the catalog")

	require.Len(t, golangci.BuildFlags, 1)
	assert.True(t, strings.HasPrefix(golangci.BuildFlags[0], "--@@rules_go+//"),
		"export_stdlib must use rules_go's canonical repo name (@@rules_go+) so it resolves on consumers that alias rules_go under a different apparent name")
}

func TestCatalog_EntriesAreWellFormed(t *testing.T) {
	catalog := loadCatalog(t)

	assert.Equal(t, 1, catalog.Version)
	assert.NotEmpty(t, catalog.AspectsBzl)

	seen := map[string]bool{}
	for language, tools := range catalog.Languages {
		require.NotEmpty(t, tools, "language %q lists no tools", language)
		for _, tool := range tools {
			assert.NotEmpty(t, tool.Name, "a tool in %q has no name", language)
			assert.NotEmpty(t, tool.Aspect, "%s/%s has no aspect", language, tool.Name)
			assert.True(t, strings.HasPrefix(tool.SARIFSuffix, "."),
				"%s/%s sarif_suffix must start with a dot", language, tool.Name)

			key := language + "/" + tool.Name
			assert.False(t, seen[key], "duplicate tool %q", key)
			seen[key] = true
		}
	}
}

func TestCatalog_VariantsAreWellFormed(t *testing.T) {
	catalog := loadCatalog(t)
	count := 0
	for language, tools := range catalog.Languages {
		for _, tool := range tools {
			for _, v := range tool.Variants {
				count++
				id := language + "/" + tool.Name
				assert.NotEmpty(t, v.WhenBazelDep, "%s variant has no when_bazel_dep", id)
				assert.NotEmpty(t, v.Module, "%s variant has no module", id)
				assert.NotEmpty(t, v.AspectsBzl, "%s variant has no aspects_bzl", id)
				assert.NotEmpty(t, v.Aspect, "%s variant has no aspect", id)
			}
		}
	}
	assert.NotZero(t, count, "the catalog must publish at least the rules_rs clippy variant")
}

func TestCatalog_RulesRsClippyVariantBindsToTheRulesRsModule(t *testing.T) {
	catalog := loadCatalog(t)
	var clippy toolEntry
	for _, tool := range catalog.Languages["rust"] {
		if tool.Name == "clippy" {
			clippy = tool
		}
	}
	assert.Equal(t, "gavel_tools_rust", clippy.Module)
	require.Len(t, clippy.Variants, 1)
	v := clippy.Variants[0]
	assert.Equal(t, "rules_rs", v.WhenBazelDep)
	assert.Equal(t, "gavel_tools_rust_rs", v.Module)
	assert.Equal(t, "//lint/aspects:defs.bzl", v.AspectsBzl)
	assert.Equal(t, "rust_clippy_submission_aspect", v.Aspect)
}
