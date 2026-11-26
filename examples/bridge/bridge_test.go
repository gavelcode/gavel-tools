package bridge_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/gavelcode/gavel-tools/lint/sarif"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The files handed to this test are exactly the gavel_submissions output group
// of //examples/bridge:report, i.e. what `bazel build --output_groups=gavel_submissions`
// would yield for the report's deps: wrapper -> passthrough -> hit, plus clean.
func submissions(t *testing.T) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	for _, path := range strings.Fields(strings.Join(os.Args[1:], " ")) {
		if !strings.HasSuffix(path, ".todo.sarif") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(os.Getenv("TEST_SRCDIR"), os.Getenv("TEST_WORKSPACE"), path))
		require.NoError(t, err)
		files[filepath.Base(path)] = data
	}
	return files
}

func TestSubmissionsCarryTransitiveDeps(t *testing.T) {
	got := submissions(t)
	var names []string
	for name := range got {
		names = append(names, name)
	}
	sort.Strings(names)
	// passthrough is skipped by the aspect (forwarded via gavel_forward), so its
	// dependency's submission must still reach the top.
	assert.Equal(t, []string{
		"examples_bridge_clean.todo.sarif",
		"examples_bridge_hit.todo.sarif",
		"examples_bridge_wrapper.todo.sarif",
	}, names)
}

func TestSubmissionsConformToBridgeContract(t *testing.T) {
	got := submissions(t)
	require.NotEmpty(t, got)
	for name, data := range got {
		assert.Empty(t, sarif.ValidateBridge(data), name)
	}
}

func TestSubmissionReportsTodoWithRuleIDAndLocation(t *testing.T) {
	data := submissions(t)["examples_bridge_hit.todo.sarif"]
	require.NotNil(t, data)
	var doc struct {
		Runs []struct {
			Results []struct {
				RuleID    string `json:"ruleId"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct{ URI string }    `json:"artifactLocation"`
						Region           struct{ StartLine int } `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
		} `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(data, &doc))
	require.Len(t, doc.Runs[0].Results, 1)
	res := doc.Runs[0].Results[0]
	assert.Equal(t, "todo/found", res.RuleID)
	assert.Equal(t, "examples/bridge/hit.txt", res.Locations[0].PhysicalLocation.ArtifactLocation.URI)
	assert.Equal(t, 2, res.Locations[0].PhysicalLocation.Region.StartLine)
}

func TestCleanSourceYieldsNoResults(t *testing.T) {
	data := submissions(t)["examples_bridge_clean.todo.sarif"]
	require.NotNil(t, data)
	assert.Contains(t, string(data), `"results":[]`)
}
