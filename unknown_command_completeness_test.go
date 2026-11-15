package spl

import (
	"strings"
	"testing"
)

func TestUnknownPipelineCommandIsIncomplete(t *testing.T) {
	for _, tc := range []struct {
		query string
		field string
	}{
		{`| mystery`, ""},
		{`| mystery y`, ""},
		{`| mystery y=z`, ""},
		{`| where x="a" | mystery`, "x"},
		{`| where x="a" | mystery y`, "x"},
		{`| where x="a" | mystery y=z`, "x"},
		{`| mystery y | where x="a"`, "x"},
		{`| where 5<10 | mystery y`, ""},
	} {
		parsed := ExtractConditions(tc.query)
		semantic := AnalyzeQuery(tc.query)
		if len(parsed.Errors) != 0 || !parsed.ExpressionIncomplete || parsed.Expression != nil ||
			len(parsed.Warnings) == 0 || !strings.HasPrefix(parsed.Warnings[0], "boolean expression unavailable") ||
			len(semantic.Errors) != 0 || semantic.Expression != nil || len(semantic.Warnings) == 0 ||
			!strings.HasPrefix(semantic.Warnings[0], "boolean expression unavailable") {
			t.Errorf("unknown command %q: parsed=%#v semantic=%#v", tc.query, parsed, semantic)
		}
		if tc.field != "" {
			found := false
			for _, condition := range semantic.Conditions {
				if condition.Field == tc.field && condition.Context == PredicateEval && condition.LHS != nil &&
					condition.LHS.Source == tc.field && condition.RHS != nil && condition.RHS.Source == `"a"` {
					found = true
				}
			}
			if !found {
				t.Errorf("known partial condition lost for %q: %#v", tc.query, semantic.Conditions)
			}
		}
	}
}

func TestUnknownNestedPipelineCommandIsIncomplete(t *testing.T) {
	for _, query := range []string{
		`search x=a | join user [ search z=a | mystery y ]`,
		`search x=a | append [ search z=a | mystery ]`,
	} {
		parsed := ExtractConditions(query)
		if len(parsed.Errors) != 0 || len(parsed.Joins) != 1 || parsed.Joins[0].Subsearch == nil {
			t.Fatalf("nested command %q: %#v", query, parsed)
		}
		nested := parsed.Joins[0].Subsearch
		if !nested.ExpressionIncomplete || len(nested.Warnings) == 0 || nested.Expression != nil ||
			!strings.HasPrefix(nested.Warnings[0], "boolean expression unavailable") {
			t.Errorf("nested command %q: %#v", query, nested)
		}
		semantic := AnalyzeQuery(query)
		if len(semantic.Errors) != 0 || len(semantic.Joins) != 1 || semantic.Joins[0].Subquery == nil ||
			len(semantic.Joins[0].Subquery.Warnings) == 0 || semantic.Joins[0].Subquery.Expression != nil {
			t.Errorf("semantic nested command %q: %#v", query, semantic)
		}
	}
}

func TestKnownPipelineCommandsStayComplete(t *testing.T) {
	for _, query := range []string{
		`x=a | head 5`,
		`| makeresults`,
		`| makeresults | search x=a`,
		`| where x="a" | head 5`,
		`| where x="a" | search y=z`,
		`| where x="a" | table x`,
		`| where x="a" | fields x`,
		`| stats count by x | where count>1`,
		`| where x="a" | outputlookup out.csv`,
		`| where x="a" | collect index=summary`,
		`| where 5<10 | outputlookup out.csv`,
	} {
		parsed := ExtractConditions(query)
		semantic := AnalyzeQuery(query)
		if len(parsed.Errors) != 0 || len(parsed.Warnings) != 0 || parsed.ExpressionIncomplete ||
			len(semantic.Errors) != 0 || len(semantic.Warnings) != 0 {
			t.Errorf("known command %q: parsed=%#v semantic=%#v", query, parsed, semantic)
		}
	}
}

func TestAuthoredEqualityOperatorOnAdditiveComparison(t *testing.T) {
	for _, tc := range []struct {
		query, field, operator string
	}{
		{`| where 'user-id'=="a"`, "user-id", "=="},
		{`| where "a"==x`, "x", "=="},
		{`| where 'user-id'="a"`, "user-id", "="},
		{`| where "a"=x`, "x", "="},
	} {
		r := AnalyzeQuery(tc.query)
		if len(r.Errors) != 0 || len(r.Warnings) != 0 || len(r.Conditions) != 1 ||
			r.Conditions[0].Field != tc.field || r.Conditions[0].Operator != tc.operator {
			t.Errorf("authored equality %q: %#v", tc.query, r)
		}
	}
}
