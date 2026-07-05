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
	Name        string   `yaml:"name"`
	Aspect      string   `yaml:"aspect"`
	SARIFSuffix string   `yaml:"sarif_suffix"`
	BuildFlags  []string `yaml:"build_flags"`
	Binary      string   `yaml:"binary"`
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
