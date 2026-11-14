package spl

import "testing"

func TestSearchMatchAllStageDoesNotAddPredicate(t *testing.T) {
	for _, query := range []string{"*", "search *"} {
		t.Run(query, func(t *testing.T) {
			result := ExtractConditions(query)
			if len(result.Errors) != 0 || len(result.Conditions) != 0 || result.Expression != nil {
				t.Fatalf("match-all stage produced a filter: %#v", result)
			}
			semantic := AnalyzeQuery(query)
			if len(semantic.Errors) != 0 || len(semantic.Conditions) != 0 || semantic.Expression != nil {
				t.Fatalf("match-all stage produced a semantic filter: %#v", semantic)
			}
		})
	}
	for _, query := range []string{
		`* | where status <= 3`,
		`search * | where status <= 3`,
		`search * | search * | where status <= 3`,
	} {
		t.Run(query, func(t *testing.T) {
			result := ExtractConditions(query)
			if len(result.Errors) != 0 || len(result.Conditions) != 1 {
				t.Fatalf("match-all stage changed the following filter: %#v", result)
			}
			condition := result.Conditions[0]
			if condition.Field != "status" || condition.Operator != "<=" || condition.Value != "3" || condition.Negated {
				t.Fatalf("following filter = %#v", condition)
			}
			semantic := AnalyzeQuery(query)
			if len(semantic.Errors) != 0 || len(semantic.Conditions) != 1 || precedenceSemanticShape(semantic.Expression) != "status" {
				t.Fatalf("match-all stage changed the semantic filter: %#v", semantic)
			}
		})
	}
}

func TestSearchMatchAllStageKeepsWildcardOperands(t *testing.T) {
	for _, tc := range []struct {
		query, value, shape string
		negated             bool
	}{
		{`search key* | where status <= 3`, "key*", "and(_raw,status)", false},
		{`search *key* | where status <= 3`, "*key*", "and(_raw,status)", false},
		{`search "*" | where status <= 3`, "*", "and(_raw,status)", false},
		{`search NOT * | where status <= 3`, "*", "and(!_raw,status)", true},
		{`search * OR status=1`, "*", "or(_raw,status)", false},
		{`search NOT (*) OR status=1`, "*", "or(!_raw,status)", true},
	} {
		t.Run(tc.query, func(t *testing.T) {
			result := ExtractConditions(tc.query)
			if len(result.Errors) != 0 || len(result.Conditions) != 2 {
				t.Fatalf("wildcard operands changed: %#v", result)
			}
			condition := result.Conditions[0]
			if condition.Field != "_raw" || condition.Operator != "contains" || condition.Value != tc.value || condition.Negated != tc.negated {
				t.Fatalf("wildcard operand = %#v", condition)
			}
			semantic := AnalyzeQuery(tc.query)
			if len(semantic.Errors) != 0 || precedenceSemanticShape(semantic.Expression) != tc.shape {
				t.Fatalf("wildcard semantic tree = %s, want %s; errors %v", precedenceSemanticShape(semantic.Expression), tc.shape, semantic.Errors)
			}
		})
	}
}
