package converter_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gavelcode/gavel-tools/lint/lang/rust/clippy/converter"
)

const sampleDiagnostics = `{"$message_type":"artifact","artifact":"out/backend.d","emit":"dep-info"}
{"$message_type":"diagnostic","message":"unneeded return statement","code":{"code":"clippy::needless_return","explanation":null},"level":"warning","spans":[{"file_name":"backend/src/domain/order.rs","byte_start":1280,"byte_end":1290,"line_start":53,"line_end":53,"column_start":9,"column_end":19,"is_primary":true,"text":[],"label":null,"suggested_replacement":null,"suggestion_applicability":null,"expansion":null}],"children":[],"rendered":"..."}
{"$message_type":"diagnostic","message":"writing &String instead of &str","code":{"code":"clippy::ptr_arg","explanation":null},"level":"error","spans":[{"file_name":"backend/src/domain/customer.rs","byte_start":565,"byte_end":572,"line_start":27,"line_end":27,"column_start":47,"column_end":54,"is_primary":true,"text":[],"label":null,"suggested_replacement":null,"suggestion_applicability":null,"expansion":null}],"children":[],"rendered":"..."}
{"$message_type":"diagnostic","message":"some lint without code","code":null,"level":"warning","spans":[{"file_name":"backend/src/lib.rs","byte_start":0,"byte_end":10,"line_start":1,"line_end":1,"column_start":1,"column_end":11,"is_primary":true,"text":[],"label":null,"suggested_replacement":null,"suggestion_applicability":null,"expansion":null}],"children":[],"rendered":"..."}
`

func TestConvertProducesValidSARIF(t *testing.T) {
	sarif, err := converter.Convert([]byte(sampleDiagnostics))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(sarif, &doc))
	assert.Equal(t, "2.1.0", doc["version"])
	runs := doc["runs"].([]any)
	require.Len(t, runs, 1)
}

func TestConvertExtractsDiagnosticsOnly(t *testing.T) {
	sarif, err := converter.Convert([]byte(sampleDiagnostics))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(sarif, &doc))
	results := doc["runs"].([]any)[0].(map[string]any)["results"].([]any)
	assert.Len(t, results, 2, "must skip artifacts and diagnostics without code")
}

func TestConvertMapsRuleIDFromCode(t *testing.T) {
	sarif, err := converter.Convert([]byte(sampleDiagnostics))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(sarif, &doc))
	results := doc["runs"].([]any)[0].(map[string]any)["results"].([]any)

	first := results[0].(map[string]any)
	assert.Equal(t, "clippy::needless_return", first["ruleId"])

	second := results[1].(map[string]any)
	assert.Equal(t, "clippy::ptr_arg", second["ruleId"])
}

func TestConvertMapsSeverity(t *testing.T) {
	sarif, err := converter.Convert([]byte(sampleDiagnostics))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(sarif, &doc))
	results := doc["runs"].([]any)[0].(map[string]any)["results"].([]any)

	assert.Equal(t, "warning", results[0].(map[string]any)["level"])
	assert.Equal(t, "error", results[1].(map[string]any)["level"])
}

func TestConvertMapsLocation(t *testing.T) {
	sarif, err := converter.Convert([]byte(sampleDiagnostics))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(sarif, &doc))
	results := doc["runs"].([]any)[0].(map[string]any)["results"].([]any)
	loc := results[0].(map[string]any)["locations"].([]any)[0].(map[string]any)
	phys := loc["physicalLocation"].(map[string]any)

	assert.Equal(t, "backend/src/domain/order.rs", phys["artifactLocation"].(map[string]any)["uri"])
	region := phys["region"].(map[string]any)
	assert.Equal(t, float64(53), region["startLine"])
	assert.Equal(t, float64(9), region["startColumn"])
}

func TestConvertEmptyInput(t *testing.T) {
	sarif, err := converter.Convert([]byte(""))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(sarif, &doc))
	results := doc["runs"].([]any)[0].(map[string]any)["results"].([]any)
	assert.Empty(t, results)
}

func TestConvertSkipsNonPrimarySpans(t *testing.T) {
	input := `{"$message_type":"diagnostic","message":"test","code":{"code":"test::rule","explanation":null},"level":"warning","spans":[{"file_name":"a.rs","byte_start":0,"byte_end":1,"line_start":1,"line_end":1,"column_start":1,"column_end":2,"is_primary":false,"text":[],"label":null,"suggested_replacement":null,"suggestion_applicability":null,"expansion":null},{"file_name":"b.rs","byte_start":0,"byte_end":1,"line_start":5,"line_end":5,"column_start":3,"column_end":4,"is_primary":true,"text":[],"label":null,"suggested_replacement":null,"suggestion_applicability":null,"expansion":null}],"children":[],"rendered":"..."}`

	sarif, err := converter.Convert([]byte(input))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(sarif, &doc))
	results := doc["runs"].([]any)[0].(map[string]any)["results"].([]any)
	require.Len(t, results, 1)

	loc := results[0].(map[string]any)["locations"].([]any)[0].(map[string]any)
	phys := loc["physicalLocation"].(map[string]any)
	assert.Equal(t, "b.rs", phys["artifactLocation"].(map[string]any)["uri"], "must use primary span")
	assert.Equal(t, float64(5), phys["region"].(map[string]any)["startLine"])
}

func TestConvertSkipsBlankLines(t *testing.T) {
	input := "\n\n" + `{"$message_type":"diagnostic","message":"test","code":{"code":"test::rule","explanation":null},"level":"warning","spans":[{"file_name":"a.rs","byte_start":0,"byte_end":1,"line_start":1,"line_end":1,"column_start":1,"column_end":2,"is_primary":true,"text":[],"label":null,"suggested_replacement":null,"suggestion_applicability":null,"expansion":null}],"children":[],"rendered":"..."}` + "\n\n"

	sarif, err := converter.Convert([]byte(input))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(sarif, &doc))
	results := doc["runs"].([]any)[0].(map[string]any)["results"].([]any)
	assert.Len(t, results, 1)
}

func TestConvertSkipsInvalidJSON(t *testing.T) {
	input := "not json at all\n" + `{"$message_type":"diagnostic","message":"test","code":{"code":"test::rule","explanation":null},"level":"warning","spans":[{"file_name":"a.rs","byte_start":0,"byte_end":1,"line_start":1,"line_end":1,"column_start":1,"column_end":2,"is_primary":true,"text":[],"label":null,"suggested_replacement":null,"suggestion_applicability":null,"expansion":null}],"children":[],"rendered":"..."}`

	sarif, err := converter.Convert([]byte(input))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(sarif, &doc))
	results := doc["runs"].([]any)[0].(map[string]any)["results"].([]any)
	assert.Len(t, results, 1)
}

func TestConvertSkipsNoteLevelDiagnostics(t *testing.T) {
	input := `{"$message_type":"diagnostic","message":"note msg","code":{"code":"test::note","explanation":null},"level":"note","spans":[{"file_name":"a.rs","byte_start":0,"byte_end":1,"line_start":1,"line_end":1,"column_start":1,"column_end":2,"is_primary":true,"text":[],"label":null,"suggested_replacement":null,"suggestion_applicability":null,"expansion":null}],"children":[],"rendered":"..."}`

	sarif, err := converter.Convert([]byte(input))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(sarif, &doc))
	results := doc["runs"].([]any)[0].(map[string]any)["results"].([]any)
	assert.Empty(t, results)
}

func TestConvertSkipsEmptySpans(t *testing.T) {
	input := `{"$message_type":"diagnostic","message":"no spans","code":{"code":"test::empty","explanation":null},"level":"warning","spans":[],"children":[],"rendered":"..."}`

	sarif, err := converter.Convert([]byte(input))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(sarif, &doc))
	results := doc["runs"].([]any)[0].(map[string]any)["results"].([]any)
	assert.Empty(t, results)
}

func TestConvertFallsBackToFirstSpan(t *testing.T) {
	input := `{"$message_type":"diagnostic","message":"no primary","code":{"code":"test::fallback","explanation":null},"level":"warning","spans":[{"file_name":"first.rs","byte_start":0,"byte_end":1,"line_start":10,"line_end":10,"column_start":5,"column_end":6,"is_primary":false,"text":[],"label":null,"suggested_replacement":null,"suggestion_applicability":null,"expansion":null},{"file_name":"second.rs","byte_start":0,"byte_end":1,"line_start":20,"line_end":20,"column_start":1,"column_end":2,"is_primary":false,"text":[],"label":null,"suggested_replacement":null,"suggestion_applicability":null,"expansion":null}],"children":[],"rendered":"..."}`

	sarif, err := converter.Convert([]byte(input))
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(sarif, &doc))
	results := doc["runs"].([]any)[0].(map[string]any)["results"].([]any)
	require.Len(t, results, 1)

	loc := results[0].(map[string]any)["locations"].([]any)[0].(map[string]any)
	phys := loc["physicalLocation"].(map[string]any)
	assert.Equal(t, "first.rs", phys["artifactLocation"].(map[string]any)["uri"])
	assert.Equal(t, float64(10), phys["region"].(map[string]any)["startLine"])
}
