package sarif

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidateBridge checks data against the bridge SARIF contract documented in
// docs/bridge.md and returns one human-readable problem per violation (empty
// when the document conforms). It is deliberately stricter than gavel's
// ingestion parser, which tolerates missing locations; a bridge author should
// not rely on that tolerance.
func ValidateBridge(data []byte) []string {
	var doc struct {
		Runs []struct {
			Tool struct {
				Driver struct {
					Name string `json:"name"`
				} `json:"driver"`
			} `json:"tool"`
			Results []struct {
				RuleID  string `json:"ruleId"`
				Level   string `json:"level"`
				Message struct {
					Text string `json:"text"`
				} `json:"message"`
				Locations []struct {
					PhysicalLocation struct {
						ArtifactLocation struct {
							URI string `json:"uri"`
						} `json:"artifactLocation"`
						Region struct {
							StartLine int `json:"startLine"`
						} `json:"region"`
					} `json:"physicalLocation"`
				} `json:"locations"`
			} `json:"results"`
			Invocations []struct {
				ExecutionSuccessful        bool `json:"executionSuccessful"`
				ToolExecutionNotifications []struct {
					Message struct {
						Text string `json:"text"`
					} `json:"message"`
				} `json:"toolExecutionNotifications"`
			} `json:"invocations"`
		} `json:"runs"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return []string{fmt.Sprintf("not valid SARIF JSON: %v", err)}
	}
	var problems []string
	if len(doc.Runs) == 0 {
		return []string{"runs: at least one run is required"}
	}
	for ri, run := range doc.Runs {
		at := func(format string, args ...any) {
			problems = append(problems, fmt.Sprintf("runs[%d]: %s", ri, fmt.Sprintf(format, args...)))
		}
		if strings.TrimSpace(run.Tool.Driver.Name) == "" {
			at("tool.driver.name must be non-empty")
		}
		for i, res := range run.Results {
			if strings.TrimSpace(res.RuleID) == "" {
				at("results[%d]: ruleId must be non-empty", i)
			}
			if strings.TrimSpace(res.Message.Text) == "" {
				at("results[%d]: message.text must be non-empty", i)
			}
			switch res.Level {
			case "", "error", "warning", "note", "none":
			default:
				at("results[%d]: level %q must be error, warning, note or none", i, res.Level)
			}
			if len(res.Locations) == 0 {
				at("results[%d]: locations[0] is required", i)
				continue
			}
			loc := res.Locations[0].PhysicalLocation
			uri := loc.ArtifactLocation.URI
			switch {
			case uri == "":
				at("results[%d]: locations[0].physicalLocation.artifactLocation.uri must be non-empty", i)
			case !workspaceRelative(uri):
				at("results[%d]: uri %q must be workspace-relative (no scheme, leading '/', '..' or execroot)", i, uri)
			}
			if loc.Region.StartLine < 1 {
				at("results[%d]: locations[0].physicalLocation.region.startLine must be >= 1", i)
			}
		}
		for i, inv := range run.Invocations {
			if inv.ExecutionSuccessful {
				continue
			}
			explained := false
			for _, n := range inv.ToolExecutionNotifications {
				if strings.TrimSpace(n.Message.Text) != "" {
					explained = true
				}
			}
			if !explained {
				at("invocations[%d]: executionSuccessful=false requires a toolExecutionNotifications entry with message.text", i)
			}
		}
	}
	return problems
}

func workspaceRelative(uri string) bool {
	if strings.Contains(uri, "://") || strings.HasPrefix(uri, "/") || strings.Contains(uri, "execroot/") {
		return false
	}
	for _, part := range strings.Split(uri, "/") {
		if part == ".." {
			return false
		}
	}
	return true
}
