package converter_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gavelcode/gavel-tools/lint/lang/typescript/eslint/converter"
)

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(b, &out))
	return out
}

func TestConvert_EmptyResultsProducesValidSARIF(t *testing.T) {
	out, err := converter.Convert([]byte(`[]`))

	require.NoError(t, err)
	d := decode(t, out)
	assert.Equal(t, "2.1.0", d["version"])
	runs := d["runs"].([]any)
	require.Len(t, runs, 1)
	run := runs[0].(map[string]any)
	driver := run["tool"].(map[string]any)["driver"].(map[string]any)
	assert.Equal(t, "eslint", driver["name"])
	assert.Empty(t, run["results"])
}

func TestConvert_MapsErrorSeverityAndLocation(t *testing.T) {
	in := `[{"filePath":"/ws/src/App.tsx","messages":[
		{"ruleId":"react-hooks/rules-of-hooks","severity":2,"message":"bad hook","line":10,"column":5,"endLine":10,"endColumn":20}
	]}]`

	out, err := converter.Convert([]byte(in))

	require.NoError(t, err)
	res := decode(t, out)["runs"].([]any)[0].(map[string]any)["results"].([]any)
	require.Len(t, res, 1)
	r := res[0].(map[string]any)
	assert.Equal(t, "react-hooks/rules-of-hooks", r["ruleId"])
	assert.Equal(t, "error", r["level"])
	assert.Equal(t, "bad hook", r["message"].(map[string]any)["text"])
	region := r["locations"].([]any)[0].(map[string]any)["physicalLocation"].(map[string]any)["region"].(map[string]any)
	assert.EqualValues(t, 10, region["startLine"])
	assert.EqualValues(t, 5, region["startColumn"])
	assert.EqualValues(t, 20, region["endColumn"])
}

func TestConvert_Severity1IsWarning(t *testing.T) {
	in := `[{"filePath":"a.ts","messages":[{"ruleId":"no-unused-vars","severity":1,"message":"m","line":1,"column":1}]}]`

	out, err := converter.Convert([]byte(in))

	require.NoError(t, err)
	r := decode(t, out)["runs"].([]any)[0].(map[string]any)["results"].([]any)[0].(map[string]any)
	assert.Equal(t, "warning", r["level"])
}

func TestConvert_DeclaresEachRuleOnce(t *testing.T) {
	in := `[{"filePath":"a.ts","messages":[
		{"ruleId":"r1","severity":2,"message":"m","line":1,"column":1},
		{"ruleId":"r1","severity":2,"message":"m2","line":2,"column":1}
	]}]`

	out, err := converter.Convert([]byte(in))

	require.NoError(t, err)
	rules := decode(t, out)["runs"].([]any)[0].(map[string]any)["tool"].(map[string]any)["driver"].(map[string]any)["rules"].([]any)
	assert.Len(t, rules, 1, "duplicate rule ids collapse to one rule declaration")
}

func TestConvert_NullRuleIDFallsBackToEslint(t *testing.T) {
	in := `[{"filePath":"a.ts","messages":[{"ruleId":null,"severity":2,"message":"Parsing error","line":1,"column":1}]}]`

	out, err := converter.Convert([]byte(in))

	require.NoError(t, err)
	r := decode(t, out)["runs"].([]any)[0].(map[string]any)["results"].([]any)[0].(map[string]any)
	assert.Equal(t, "eslint", r["ruleId"], "syntax errors with no rule id are attributed to eslint itself")
}

func TestConvert_FilesWithNoMessagesYieldNoResults(t *testing.T) {
	in := `[{"filePath":"clean.ts","messages":[]}]`

	out, err := converter.Convert([]byte(in))

	require.NoError(t, err)
	assert.Empty(t, decode(t, out)["runs"].([]any)[0].(map[string]any)["results"])
}

func TestConvert_InvalidJSONErrors(t *testing.T) {
	_, err := converter.Convert([]byte(`not json`))

	require.Error(t, err)
}
