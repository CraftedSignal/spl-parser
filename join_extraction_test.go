package spl

import (
	"strings"
	"testing"
)

var joinTestCases = []JoinTestCase{
	{
		Name:  "enrichment join - left join with table output",
		Query: `index=auth action="failed" | join type=left user [search index=assets status="active" | table user, department, location]`,
		MainConditions: []ExpectedCondition{
			{Field: "action", Operator: "=", Value: "failed"},
		},
		ExpectedJoins: []ExpectedJoin{{
			Type:       "left",
			JoinFields: []string{"user"},
			SubsearchConditions: []ExpectedCondition{
				{Field: "status", Operator: "=", Value: "active"},
			},
		}},
		Provenance: map[string]FieldProvenance{
			"user":       ProvenanceJoinKey,
			"department": ProvenanceJoined,
			"location":   ProvenanceJoined,
			"action":     ProvenanceMain,
		},
	},
	{
		Name:  "correlation join - inner join two event sources",
		Query: `index=auth EventID=4625 | join type=inner user [search index=endpoint EventID=4688 | where ParentProcessName="cmd.exe" | table user, ProcessName, ParentProcessName] | where ProcessName="*mimikatz*"`,
		MainConditions: []ExpectedCondition{
			{Field: "EventID", Operator: "=", Value: "4625"},
		},
		ExpectedJoins: []ExpectedJoin{{
			Type:       "inner",
			JoinFields: []string{"user"},
			SubsearchConditions: []ExpectedCondition{
				{Field: "EventID", Operator: "=", Value: "4688"},
				{Field: "ParentProcessName", Operator: "=", Value: "cmd.exe"},
			},
		}},
		Provenance: map[string]FieldProvenance{
			"user":              ProvenanceJoinKey,
			"ProcessName":       ProvenanceJoined,
			"ParentProcessName": ProvenanceJoined,
			"EventID":           ProvenanceMain,
		},
	},
	{
		Name:  "default join type (inner)",
		Query: `index=main | join user [search index=other field="val"]`,
		ExpectedJoins: []ExpectedJoin{{
			Type:       "inner",
			JoinFields: []string{"user"},
			SubsearchConditions: []ExpectedCondition{
				{Field: "field", Operator: "=", Value: "val"},
			},
		}},
	},
	{
		Name:  "join with no field list (no ON clause)",
		Query: `index=main | join type=left [search index=other field="val"]`,
		ExpectedJoins: []ExpectedJoin{{
			Type:       "left",
			JoinFields: nil,
			SubsearchConditions: []ExpectedCondition{
				{Field: "field", Operator: "=", Value: "val"},
			},
		}},
	},
	{
		Name:  "join with multiple join fields",
		Query: `index=main | join type=left user, host [search index=assets status="active"]`,
		ExpectedJoins: []ExpectedJoin{{
			Type:       "left",
			JoinFields: []string{"user", "host"},
			SubsearchConditions: []ExpectedCondition{
				{Field: "status", Operator: "=", Value: "active"},
			},
		}},
	},
	{
		Name:  "append command",
		Query: `index=main action="blocked" | append [search index=secondary action="denied"]`,
		MainConditions: []ExpectedCondition{
			{Field: "action", Operator: "=", Value: "blocked"},
		},
		ExpectedJoins: []ExpectedJoin{{
			Type:     "append",
			IsAppend: true,
			SubsearchConditions: []ExpectedCondition{
				{Field: "action", Operator: "=", Value: "denied"},
			},
		}},
	},
	{
		Name:  "subsearch with eval computed fields",
		Query: `index=main | join user [search index=endpoint | eval cmd=lower(CommandLine) | search cmd="powershell"]`,
		ExpectedJoins: []ExpectedJoin{{
			Type:       "inner",
			JoinFields: []string{"user"},
		}},
	},
	{
		Name:  "join with complex subsearch pipeline",
		Query: `index=auth EventID=4625 | join user [search index=endpoint EventID=1 | where CommandLine="*whoami*" | stats count by user | where count > 5 | table user]`,
		MainConditions: []ExpectedCondition{
			{Field: "EventID", Operator: "=", Value: "4625"},
		},
		ExpectedJoins: []ExpectedJoin{{
			Type:       "inner",
			JoinFields: []string{"user"},
			SubsearchConditions: []ExpectedCondition{
				{Field: "EventID", Operator: "=", Value: "1"},
				{Field: "CommandLine", Operator: "=", Value: "*whoami*"},
			},
		}},
	},
}

func TestJoinExtraction_TableDriven(t *testing.T) {
	for _, tc := range joinTestCases {
		t.Run(tc.Name, func(t *testing.T) {
			result := ExtractConditions(tc.Query)

			if len(result.Errors) > 0 {
				t.Logf("Parse warnings: %v", result.Errors)
			}

			// Verify main conditions
			for _, exp := range tc.MainConditions {
				assertHasCondition(t, result.Conditions, exp)
			}

			// Verify join count
			if len(result.Joins) != len(tc.ExpectedJoins) {
				t.Fatalf("Expected %d joins, got %d", len(tc.ExpectedJoins), len(result.Joins))
			}

			// Verify each join
			for i, expJoin := range tc.ExpectedJoins {
				j := result.Joins[i]

				if j.Type != expJoin.Type {
					t.Errorf("Join[%d]: expected type %q, got %q", i, expJoin.Type, j.Type)
				}

				if j.IsAppend != expJoin.IsAppend {
					t.Errorf("Join[%d]: expected IsAppend=%v, got %v", i, expJoin.IsAppend, j.IsAppend)
				}

				// Join fields: nil means we don't check, empty slice means expect none
				if expJoin.JoinFields != nil {
					if len(j.JoinFields) != len(expJoin.JoinFields) {
						t.Errorf("Join[%d]: expected join fields %v, got %v", i, expJoin.JoinFields, j.JoinFields)
					} else {
						for k, ef := range expJoin.JoinFields {
							if !strings.EqualFold(j.JoinFields[k], ef) {
								t.Errorf("Join[%d]: join field[%d] expected %q, got %q", i, k, ef, j.JoinFields[k])
							}
						}
					}
				}

				// Verify subsearch conditions
				if j.Subsearch != nil {
					for _, expCond := range expJoin.SubsearchConditions {
						assertHasCondition(t, j.Subsearch.Conditions, expCond)
					}
				} else if len(expJoin.SubsearchConditions) > 0 {
					t.Errorf("Join[%d]: expected subsearch conditions but Subsearch is nil", i)
				}
			}

			// Verify provenance
			for field, expectedProv := range tc.Provenance {
				actual := ClassifyFieldProvenance(result, field)
				if actual != expectedProv {
					t.Errorf("Provenance for %q: expected %q, got %q", field, expectedProv, actual)
				}
			}
		})
	}
}

// TestJoinExtraction_BackwardCompatibility verifies existing behavior is unchanged
func TestJoinExtraction_BackwardCompatibility(t *testing.T) {
	query := `index=main | join type=left user [search index=users status="active"]`
	result := ExtractConditions(query)

	// Main conditions should NOT include subsearch conditions (backward compat)
	for _, c := range result.Conditions {
		if c.Field == "status" && c.Value == "active" {
			t.Error("Main conditions should not include subsearch conditions (backward compatibility)")
		}
	}

	// But now we ALSO have the subsearch conditions available via Joins
	if len(result.Joins) != 1 {
		t.Fatalf("Expected 1 join, got %d", len(result.Joins))
	}
	hasStatus := false
	for _, c := range result.Joins[0].Subsearch.Conditions {
		if c.Field == "status" {
			hasStatus = true
		}
	}
	if !hasStatus {
		t.Error("Subsearch conditions should be available via result.Joins[0].Subsearch")
	}
}

// TestJoinExtraction_ExistingTestStillPasses is the original test verbatim
func TestJoinExtraction_ExistingTestStillPasses(t *testing.T) {
	query := `index=main | join type=left user [search index=users status="active"]`
	result := ExtractConditions(query)

	for _, c := range result.Conditions {
		if c.Field == "status" && c.Value == "active" {
			t.Error("Should not extract conditions from join subsearch")
		}
	}
}
