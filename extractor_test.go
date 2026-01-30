package spl

import (
	"strings"
	"testing"
)

func TestExtractConditions_Simple(t *testing.T) {
	// All field conditions should be extracted including index, sourcetype, source
	query := `index=main sourcetype="access_combined" status=200`

	result := ExtractConditions(query)

	if len(result.Errors) > 0 {
		t.Logf("Parse errors: %v", result.Errors)
	}

	// All 3 conditions should be extracted
	if len(result.Conditions) != 3 {
		t.Errorf("Expected 3 conditions, got %d", len(result.Conditions))
		for _, c := range result.Conditions {
			t.Logf("Condition: %+v", c)
		}
		return
	}

	// Check that we have all fields
	fields := make(map[string]string)
	for _, c := range result.Conditions {
		fields[c.Field] = c.Value
	}
	if fields["index"] != "main" {
		t.Errorf("Expected index=main")
	}
	if fields["sourcetype"] != "access_combined" {
		t.Errorf("Expected sourcetype=access_combined")
	}
	if fields["status"] != "200" {
		t.Errorf("Expected status=200")
	}
}

func TestExtractConditions_ORConditions(t *testing.T) {
	// Both index and action should be extracted
	query := `index=main (action="success" OR action="failure")`

	result := ExtractConditions(query)

	if len(result.Errors) > 0 {
		t.Logf("Parse errors: %v", result.Errors)
	}

	// Should have index and action conditions
	if len(result.Conditions) != 2 {
		t.Errorf("Expected 2 conditions (index, action), got %d", len(result.Conditions))
		for _, c := range result.Conditions {
			t.Logf("Condition: %+v", c)
		}
	}

	foundAction := false
	foundIndex := false
	for _, c := range result.Conditions {
		t.Logf("Condition: %+v", c)
		if c.Field == "action" {
			foundAction = true
			if len(c.Alternatives) != 2 {
				t.Errorf("Expected 2 alternatives for action, got %d", len(c.Alternatives))
			}
		}
		if c.Field == "index" {
			foundIndex = true
		}
	}

	if !foundAction {
		t.Error("Expected to find action condition with alternatives")
	}
	if !foundIndex {
		t.Error("Expected to find index condition")
	}
}

func TestExtractConditions_WhereClause(t *testing.T) {
	// Both index and status should be extracted
	query := `index=main | where status=200`

	result := ExtractConditions(query)

	if len(result.Errors) > 0 {
		t.Logf("Parse errors: %v", result.Errors)
	}

	// Should have 2 conditions: index and status from where
	if len(result.Conditions) != 2 {
		t.Errorf("Expected 2 conditions (index, status), got %d", len(result.Conditions))
		for _, c := range result.Conditions {
			t.Logf("Condition: %+v", c)
		}
		return
	}

	// Check that status has pipe stage > 0
	for _, c := range result.Conditions {
		if c.Field == "status" && c.PipeStage == 0 {
			t.Error("Expected status to have pipe stage > 0")
		}
	}
}

func TestExtractConditions_JoinSubsearch(t *testing.T) {
	// index is metadata, subsearch conditions should be ignored
	query := `index=main | join type=left user [search index=users status="active"]`

	result := ExtractConditions(query)

	if len(result.Errors) > 0 {
		t.Logf("Parse errors: %v", result.Errors)
	}

	t.Logf("Found %d conditions", len(result.Conditions))
	for _, c := range result.Conditions {
		t.Logf("Condition: %+v", c)
	}

	// Should have no conditions: index is metadata, subsearch is ignored
	// Conditions from subsearch should be ignored
	for _, c := range result.Conditions {
		if c.Field == "status" && c.Value == "active" {
			t.Error("Should not extract conditions from join subsearch")
		}
	}
}

func TestExtractConditions_Negation(t *testing.T) {
	// Both index and status should be extracted, status should be negated
	query := `index=main NOT status="error"`

	result := ExtractConditions(query)

	if len(result.Errors) > 0 {
		t.Logf("Parse errors: %v", result.Errors)
	}

	if len(result.Conditions) != 2 {
		t.Errorf("Expected 2 conditions (index, status), got %d", len(result.Conditions))
	}

	for _, c := range result.Conditions {
		t.Logf("Condition: %+v", c)
		if c.Field == "status" && !c.Negated {
			t.Error("Expected status condition to be negated")
		}
	}
}

func TestExtractConditions_ComplexQuery(t *testing.T) {
	// index, sourcetype are metadata; action and count>100 are data conditions
	query := `index=security sourcetype="firewall" action="blocked"
| stats count by src_ip
| where count > 100`

	result := ExtractConditions(query)

	if len(result.Errors) > 0 {
		t.Logf("Parse errors: %v", result.Errors)
	}

	t.Logf("Found %d conditions", len(result.Conditions))
	for _, c := range result.Conditions {
		t.Logf("Condition: %+v", c)
	}

	// Should have action and count conditions (index, sourcetype are metadata)
	// Note: count is a keyword so it's excluded too
	fieldCount := make(map[string]int)
	for _, c := range result.Conditions {
		fieldCount[c.Field]++
	}

	if fieldCount["action"] == 0 {
		t.Error("Expected to find action condition")
	}
}

func TestExtractConditions_INOperator(t *testing.T) {
	// Both index and status should be extracted
	query := `index=main status IN ("200", "201", "204")`

	result := ExtractConditions(query)

	if len(result.Errors) > 0 {
		t.Logf("Parse errors: %v", result.Errors)
	}

	// Should have index and status conditions
	if len(result.Conditions) != 2 {
		t.Errorf("Expected 2 conditions (index, status), got %d", len(result.Conditions))
	}

	statusFound := false
	for _, c := range result.Conditions {
		t.Logf("Condition: %+v", c)
		if c.Field == "status" {
			statusFound = true
			if len(c.Alternatives) < 2 {
				t.Errorf("Expected multiple alternatives for status IN, got %d", len(c.Alternatives))
			}
		}
	}

	if !statusFound {
		t.Error("Expected to find status condition")
	}
}

func TestExtractConditions_PipedSearch(t *testing.T) {
	// Test complex query from fuzz failures
	// source is metadata, f_54401 and f_4400 are data fields
	query := `source="/var/log/auth.log" f_54401!="fulbwnvc" | fields f_54401 event f_80119 | sort - f_54401 | transaction f_54401 | rex field=f_54401 "(?<extract_7614>pixzc)" | search f_4400 <= 82`

	result := ExtractConditions(query)

	t.Logf("Parse errors: %v", result.Errors)
	t.Logf("Found %d conditions", len(result.Conditions))
	for _, c := range result.Conditions {
		t.Logf("Condition: %+v", c)
	}

	// Should have conditions for f_54401 and f_4400 (source is metadata)
	found := make(map[string]bool)
	for _, c := range result.Conditions {
		found[c.Field] = true
	}

	if !found["f_54401"] {
		t.Error("Expected to find f_54401 condition")
	}
	if !found["f_4400"] {
		t.Error("Expected to find f_4400 condition from piped search")
	}
}

func TestExtractConditions_ComputedFields(t *testing.T) {
	query := `index=endpoint EventCode=4688
| eval cmd=lower(CommandLine)
| search cmd="powershell" AND cmd="-enc"`

	result := ExtractConditions(query)

	t.Logf("Parse errors: %v", result.Errors)
	t.Logf("Found %d conditions", len(result.Conditions))
	for _, c := range result.Conditions {
		t.Logf("Condition: %+v", c)
	}

	// cmd should be in conditions but marked as computed
	foundCmd := false
	for _, c := range result.Conditions {
		if strings.ToLower(c.Field) == "cmd" {
			foundCmd = true
			if !c.IsComputed {
				t.Errorf("Expected computed field 'cmd' to be marked as IsComputed=true")
			}
		}
	}
	if !foundCmd {
		t.Error("Expected computed field 'cmd' to be present in conditions")
	}

	// EventCode should be present
	found := false
	for _, c := range result.Conditions {
		if c.Field == "EventCode" {
			found = true
		}
	}
	if !found {
		t.Error("Expected EventCode condition to be present")
	}
}

func TestExtractConditions_ColonValue(t *testing.T) {
	// Test colon-separated values on a non-metadata field
	// index, sourcetype, host are metadata, so use eventtype instead
	query := `eventtype=network:connection:allowed status=200`

	result := ExtractConditions(query)

	t.Logf("Parse errors: %v", result.Errors)
	t.Logf("Found %d conditions", len(result.Conditions))
	for _, c := range result.Conditions {
		t.Logf("Condition: %+v", c)
	}

	// Check that eventtype has the full colon-separated value
	found := false
	for _, c := range result.Conditions {
		if c.Field == "eventtype" {
			found = true
			if c.Value != "network:connection:allowed" {
				t.Errorf("Expected eventtype=network:connection:allowed, got %s", c.Value)
			}
		}
	}
	if !found {
		t.Error("Expected to find eventtype condition")
	}
}

func TestDeduplicateConditions(t *testing.T) {
	conditions := []Condition{
		{Field: "status", Operator: "=", Value: "*", PipeStage: 0},
		{Field: "status", Operator: "=", Value: "200", PipeStage: 1},
	}

	result := DeduplicateConditions(conditions)

	if len(result) != 1 {
		t.Errorf("Expected 1 condition after dedup, got %d", len(result))
	}

	if result[0].Value != "200" {
		t.Errorf("Expected value 200 (from later stage), got %s", result[0].Value)
	}
}

func TestExtractConditions_WildcardValue(t *testing.T) {
	// Test wildcard values on a non-metadata field
	query := `CommandLine=powershell* status=200`

	result := ExtractConditions(query)

	if len(result.Errors) > 0 {
		t.Logf("Parse errors: %v", result.Errors)
	}

	for _, c := range result.Conditions {
		t.Logf("Condition: %+v", c)
	}

	// Should recognize wildcard values
	found := false
	for _, c := range result.Conditions {
		if c.Field == "CommandLine" {
			found = true
			if c.Value != "powershell*" {
				t.Errorf("Expected CommandLine=powershell*, got %s", c.Value)
			}
		}
	}

	if !found {
		t.Error("Expected to find CommandLine condition")
	}
}

func TestExtractConditions_QuotedWildcard(t *testing.T) {
	// Test from fuzz failure: wildcard inside quotes after piped search
	query := `index=main "error" OR "failed" | search f_5226 = "*bakvf*"`

	result := ExtractConditions(query)

	t.Logf("Parse errors: %v", result.Errors)
	t.Logf("Found %d conditions", len(result.Conditions))
	for _, c := range result.Conditions {
		t.Logf("Condition: %+v", c)
	}

	// Should have f_5226 condition from piped search
	found := false
	for _, c := range result.Conditions {
		if c.Field == "f_5226" {
			found = true
			if c.Value != "*bakvf*" {
				t.Errorf("Expected f_5226=*bakvf*, got %s", c.Value)
			}
		}
	}

	if !found {
		t.Error("Expected to find f_5226 condition from piped search")
	}
}

func TestExtractConditions_HostIN(t *testing.T) {
	// Test host IN operator - all conditions are extracted
	query := `index=sysmon NOT host IN ("gzs", "pmagc", "hok") EventCode=1`

	result := ExtractConditions(query)

	t.Logf("Parse errors: %v", result.Errors)
	t.Logf("Found %d conditions", len(result.Conditions))
	for _, c := range result.Conditions {
		t.Logf("Condition: %+v", c)
	}

	// Should have index, host and EventCode
	if len(result.Conditions) != 3 {
		t.Errorf("Expected 3 conditions (index, host, EventCode), got %d", len(result.Conditions))
	}

	// host should be present and negated
	hostFound := false
	eventCodeFound := false
	indexFound := false
	for _, c := range result.Conditions {
		if c.Field == "host" {
			hostFound = true
			if !c.Negated {
				t.Error("Expected host condition to be negated")
			}
		}
		if c.Field == "EventCode" {
			eventCodeFound = true
		}
		if c.Field == "index" {
			indexFound = true
		}
	}
	if !hostFound {
		t.Error("Expected host condition to be present")
	}
	if !eventCodeFound {
		t.Error("Expected EventCode condition to be present")
	}
	if !indexFound {
		t.Error("Expected index condition to be present")
	}
}

func TestExtractConditions_HostWildcard(t *testing.T) {
	// Test host with wildcard - all conditions are extracted
	query := `index=sysmon host="*xnsnlyh*" EventCode=1`

	result := ExtractConditions(query)

	t.Logf("Parse errors: %v", result.Errors)
	t.Logf("Found %d conditions", len(result.Conditions))
	for _, c := range result.Conditions {
		t.Logf("Condition: %+v", c)
	}

	// Should have index, host and EventCode
	if len(result.Conditions) != 3 {
		t.Errorf("Expected 3 conditions (index, host, EventCode), got %d", len(result.Conditions))
	}

	hostFound := false
	eventCodeFound := false
	indexFound := false
	for _, c := range result.Conditions {
		if c.Field == "host" {
			hostFound = true
			if c.Value != "*xnsnlyh*" {
				t.Errorf("Expected host=*xnsnlyh*, got %s", c.Value)
			}
		}
		if c.Field == "EventCode" {
			eventCodeFound = true
		}
		if c.Field == "index" {
			indexFound = true
		}
	}
	if !hostFound {
		t.Error("Expected host condition to be present")
	}
	if !eventCodeFound {
		t.Error("Expected EventCode condition to be present")
	}
	if !indexFound {
		t.Error("Expected index condition to be present")
	}
}

func TestExtractConditions_NestedParens(t *testing.T) {
	// Test deeply nested parentheses
	query := `index=main ((status="200" OR status="201") AND (action="success" OR action="failed"))`

	result := ExtractConditions(query)

	t.Logf("Parse errors: %v", result.Errors)
	t.Logf("Found %d conditions", len(result.Conditions))
	for _, c := range result.Conditions {
		t.Logf("Condition: %+v", c)
	}

	// Should have both status and action
	statusFound := false
	actionFound := false
	for _, c := range result.Conditions {
		if c.Field == "status" {
			statusFound = true
		}
		if c.Field == "action" {
			actionFound = true
		}
	}

	if !statusFound {
		t.Error("Expected status condition")
	}
	if !actionFound {
		t.Error("Expected action condition")
	}
}

func TestExtractConditions_NumericComparisons(t *testing.T) {
	// Test various numeric comparison operators
	query := `EventCode>1000 bytes>=500 duration<30 count<=10`

	result := ExtractConditions(query)

	t.Logf("Parse errors: %v", result.Errors)
	t.Logf("Found %d conditions", len(result.Conditions))
	for _, c := range result.Conditions {
		t.Logf("Condition: %+v", c)
	}

	expectedOps := map[string]string{
		"EventCode": ">",
		"bytes":     ">=",
		"duration":  "<",
	}

	for field, expectedOp := range expectedOps {
		found := false
		for _, c := range result.Conditions {
			if c.Field == field {
				found = true
				if c.Operator != expectedOp {
					t.Errorf("Expected %s to have operator %s, got %s", field, expectedOp, c.Operator)
				}
			}
		}
		if !found {
			t.Errorf("Expected to find condition for %s", field)
		}
	}
}

func TestExtractConditions_MixedPipelineCommands(t *testing.T) {
	// Test complex pipeline with multiple command types
	query := `index=main EventCode=4688 | eval cmd=lower(CommandLine) | rex field=CommandLine "(?<extract>powershell)" | where bytes > 1000 | search user="admin*"`

	result := ExtractConditions(query)

	t.Logf("Parse errors: %v", result.Errors)
	t.Logf("Found %d conditions", len(result.Conditions))
	for _, c := range result.Conditions {
		t.Logf("Condition: %+v", c)
	}

	// EventCode should be present (stage 0)
	eventCodeFound := false
	for _, c := range result.Conditions {
		if c.Field == "EventCode" {
			eventCodeFound = true
			if c.PipeStage != 0 {
				t.Errorf("Expected EventCode at stage 0, got %d", c.PipeStage)
			}
		}
	}
	if !eventCodeFound {
		t.Error("Expected EventCode condition")
	}

	// cmd should NOT be present (it's computed by eval)
	for _, c := range result.Conditions {
		if c.Field == "cmd" {
			t.Error("cmd is computed by eval and should be excluded")
		}
	}

	// bytes should be present (from where clause)
	bytesFound := false
	for _, c := range result.Conditions {
		if c.Field == "bytes" {
			bytesFound = true
			if c.PipeStage == 0 {
				t.Errorf("Expected bytes at pipe stage > 0")
			}
		}
	}
	if !bytesFound {
		t.Error("Expected bytes condition from where clause")
	}

	// user should be present (from piped search)
	userFound := false
	for _, c := range result.Conditions {
		if c.Field == "user" {
			userFound = true
		}
	}
	if !userFound {
		t.Error("Expected user condition from piped search")
	}
}

func TestExtractConditions_NEQOperator(t *testing.T) {
	// Test != operator
	query := `status!="error" action!="blocked"`

	result := ExtractConditions(query)

	t.Logf("Parse errors: %v", result.Errors)
	t.Logf("Found %d conditions", len(result.Conditions))
	for _, c := range result.Conditions {
		t.Logf("Condition: %+v", c)
	}

	if len(result.Conditions) != 2 {
		t.Errorf("Expected 2 conditions, got %d", len(result.Conditions))
	}

	for _, c := range result.Conditions {
		if c.Operator != "!=" {
			t.Errorf("Expected != operator for %s, got %s", c.Field, c.Operator)
		}
	}
}

func TestExtractConditions_MetadataFiltering(t *testing.T) {
	// Verify that all conditions including metadata fields are extracted
	// Only earliest/latest time modifiers are excluded
	testCases := []struct {
		name          string
		query         string
		expectedCount int
		expectedField string
	}{
		// All conditions should be extracted
		{"host_only", `host IN ("a", "b", "c")`, 1, "host"},
		{"host_with_data", `index=sysmon EventCode=4625 host="server1"`, 3, "index"},
		// index, sourcetype, source, host are all extracted
		{"all_metadata_with_host", `index=main sourcetype=syslog source="/var/log" host="*"`, 4, "index"},
		{"data_with_host", `index=main action="blocked" host="server1"`, 3, "index"},
		{"host_comparison", `index=sysmon host>8632`, 2, "index"},
		// index, sourcetype, source are now extracted
		{"metadata_fields", `index=main sourcetype=syslog source="/var/log"`, 3, "index"},
		// earliest is still excluded (time modifier)
		{"index_with_time", `index=main earliest=-24h`, 1, "index"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ExtractConditions(tc.query)
			t.Logf("Query: %s", tc.query)
			t.Logf("Conditions: %d, Errors: %d", len(result.Conditions), len(result.Errors))
			for _, c := range result.Conditions {
				t.Logf("  %s %s %q", c.Field, c.Operator, c.Value)
			}

			if len(result.Conditions) != tc.expectedCount {
				t.Errorf("Expected %d conditions, got %d", tc.expectedCount, len(result.Conditions))
			}

			if tc.expectedField != "" && len(result.Conditions) > 0 {
				if result.Conditions[0].Field != tc.expectedField {
					t.Errorf("Expected field %s, got %s", tc.expectedField, result.Conditions[0].Field)
				}
			}
		})
	}
}

func TestExtractConditions_FunctionConditions(t *testing.T) {
	testCases := []struct {
		name           string
		query          string
		expectedField  string
		expectedOp     string
		expectedValue  string
	}{
		{
			name:          "cidrmatch",
			query:         `index=network | where cidrmatch("10.0.0.0/8", src_ip)`,
			expectedField: "src_ip",
			expectedOp:    "cidrmatch",
			expectedValue: "10.0.0.0/8",
		},
		{
			name:          "match",
			query:         `index=main | where match(CommandLine, "(?i)invoke-mimikatz")`,
			expectedField: "CommandLine",
			expectedOp:    "matches",
			expectedValue: "(?i)invoke-mimikatz",
		},
		{
			name:          "like",
			query:         `index=main | where like(process_name, "%.exe")`,
			expectedField: "process_name",
			expectedOp:    "like",
			expectedValue: "*.exe",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ExtractConditions(tc.query)
			t.Logf("Query: %s", tc.query)
			t.Logf("Found %d conditions", len(result.Conditions))
			for _, c := range result.Conditions {
				t.Logf("  Condition: %+v", c)
			}

			// Find the function condition (skip index=)
			var funcCond *Condition
			for i, c := range result.Conditions {
				if c.Operator == tc.expectedOp {
					funcCond = &result.Conditions[i]
					break
				}
			}

			if funcCond == nil {
				t.Errorf("Expected to find a %s condition", tc.expectedOp)
				return
			}

			if funcCond.Field != tc.expectedField {
				t.Errorf("Expected field %s, got %s", tc.expectedField, funcCond.Field)
			}
			if funcCond.Value != tc.expectedValue {
				t.Errorf("Expected value %s, got %s", tc.expectedValue, funcCond.Value)
			}
		})
	}
}

func TestExtractConditions_GroupByFields(t *testing.T) {
	testCases := []struct {
		name           string
		query          string
		expectedFields []string
	}{
		{
			name:           "stats_single_field",
			query:          `index=main | stats count by user`,
			expectedFields: []string{"user"},
		},
		{
			name:           "stats_multiple_fields",
			query:          `index=main | stats count by user, host`,
			expectedFields: []string{"user", "host"},
		},
		{
			name:           "eventstats",
			query:          `index=main | eventstats sum(bytes) by src_ip`,
			expectedFields: []string{"src_ip"},
		},
		{
			name:           "streamstats",
			query:          `index=main | streamstats count by user`,
			expectedFields: []string{"user"},
		},
		{
			name:           "timechart",
			query:          `index=main | timechart count by host`,
			expectedFields: []string{"host"},
		},
		{
			name:           "chart_by",
			query:          `index=main | chart count by src_ip`,
			expectedFields: []string{"src_ip"},
		},
		{
			name:           "chart_by_over",
			query:          `index=main | chart count by src_ip over time`,
			expectedFields: []string{"time", "src_ip"},
		},
		{
			name:           "no_by_clause",
			query:          `index=main | stats count`,
			expectedFields: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := ExtractConditions(tc.query)

			t.Logf("Query: %s", tc.query)
			t.Logf("GroupByFields: %v (expected: %v)", result.GroupByFields, tc.expectedFields)

			if tc.expectedFields == nil {
				if len(result.GroupByFields) != 0 {
					t.Errorf("Expected no group-by fields, got %v", result.GroupByFields)
				}
				return
			}

			if len(result.GroupByFields) != len(tc.expectedFields) {
				t.Errorf("Expected %d group-by fields, got %d: %v",
					len(tc.expectedFields), len(result.GroupByFields), result.GroupByFields)
				return
			}

			// Check each expected field is present (order may vary)
			fieldSet := make(map[string]bool)
			for _, f := range result.GroupByFields {
				fieldSet[strings.ToLower(f)] = true
			}
			for _, expected := range tc.expectedFields {
				if !fieldSet[strings.ToLower(expected)] {
					t.Errorf("Expected group-by field %s not found in %v", expected, result.GroupByFields)
				}
			}
		})
	}
}

func TestJoinExtraction_Simple(t *testing.T) {
	query := `index=main action="login" | join type=left user [search index=assets status="active"]`
	result := ExtractConditions(query)

	if len(result.Joins) != 1 {
		t.Fatalf("Expected 1 join, got %d", len(result.Joins))
	}

	j := result.Joins[0]
	if j.Type != "left" {
		t.Errorf("Expected join type 'left', got %q", j.Type)
	}
	if len(j.JoinFields) != 1 || j.JoinFields[0] != "user" {
		t.Errorf("Expected join fields [user], got %v", j.JoinFields)
	}
	if j.Subsearch == nil {
		t.Fatal("Expected subsearch ParseResult, got nil")
	}

	hasStatus := false
	for _, c := range j.Subsearch.Conditions {
		if c.Field == "status" && c.Value == "active" {
			hasStatus = true
		}
	}
	if !hasStatus {
		t.Error("Expected subsearch to contain status=active condition")
	}
}

func TestSubsearchTextExtraction(t *testing.T) {
	query := `index=main | join user [search index=assets department="engineering" | where risk_score > 50]`
	result := ExtractConditions(query)

	if len(result.Joins) == 0 {
		t.Fatal("Expected at least 1 join")
	}

	sub := result.Joins[0].Subsearch
	if sub == nil {
		t.Fatal("Expected subsearch to be parsed")
	}

	hasDept := false
	hasRisk := false
	for _, c := range sub.Conditions {
		if c.Field == "department" && c.Value == "engineering" {
			hasDept = true
		}
		if c.Field == "risk_score" && c.Operator == ">" {
			hasRisk = true
		}
	}
	if !hasDept {
		t.Error("Expected subsearch to have department=engineering")
	}
	if !hasRisk {
		t.Error("Expected subsearch to have risk_score > 50")
	}
}

func TestJoinExtraction_ExposedFields(t *testing.T) {
	query := `index=auth EventID=4625 | join type=inner user [search index=endpoint EventID=4688 | where ParentProcessName="cmd.exe" | table user, ProcessName, ParentProcessName, ComputerName]`
	result := ExtractConditions(query)

	if len(result.Joins) == 0 {
		t.Fatal("Expected at least 1 join")
	}

	j := result.Joins[0]

	expectedExposed := map[string]bool{
		"user": true, "ProcessName": true, "ParentProcessName": true, "ComputerName": true,
	}
	actualExposed := make(map[string]bool)
	for _, f := range j.ExposedFields {
		actualExposed[f] = true
	}
	for field := range expectedExposed {
		if !actualExposed[field] {
			t.Errorf("Expected exposed field %q not found in %v", field, j.ExposedFields)
		}
	}
}

func TestJoinExtraction_FieldProvenance(t *testing.T) {
	query := `index=auth EventID=4625 | join type=inner user [search index=endpoint EventID=4688 | table user, ProcessName, ComputerName] | where ProcessName="*mimikatz*"`
	result := ExtractConditions(query)

	if len(result.Joins) == 0 {
		t.Fatal("Expected at least 1 join")
	}

	tests := []struct {
		field    string
		expected FieldProvenance
	}{
		{"user", ProvenanceJoinKey},
		{"ProcessName", ProvenanceJoined},
		{"ComputerName", ProvenanceJoined},
		{"EventID", ProvenanceMain},
	}

	for _, tc := range tests {
		actual := ClassifyFieldProvenance(result, tc.field)
		if actual != tc.expected {
			t.Errorf("Field %q: expected provenance %q, got %q", tc.field, tc.expected, actual)
		}
	}
}
