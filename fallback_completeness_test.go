package spl

import (
	"fmt"
	"testing"
)

// When the grammar rejects a query, the regex fallback recovers what it can.
// Every predicate it misses must be visible: either extracted, or reported
// through ExpressionIncomplete (README: "Malformed or unresolved syntax still
// reports ExpressionIncomplete"). Errors stay empty, as the recovery decided.
func TestRecoveredExtractionReportsWhatItMisses(t *testing.T) {
	reported := func(result *ParseResult) bool {
		return result.ExpressionIncomplete || len(result.Errors) > 0
	}
	has := func(result *ParseResult, field, operator, value string) bool {
		for _, condition := range result.Conditions {
			if condition.Field == field && (operator == "" || condition.Operator == operator && condition.Value == value) {
				return true
			}
		}
		return false
	}
	list := func(result *ParseResult) []string {
		conditions := make([]string, 0, len(result.Conditions))
		for _, condition := range result.Conditions {
			conditions = append(conditions, fmt.Sprintf("%s %s %q", condition.Field, condition.Operator, condition.Value))
		}
		return conditions
	}

	for _, tc := range []struct {
		query                  string
		field, operator, value string
	}{
		{`index=_audit action=IN "failed login" user=bob`, "_raw", "contains", "failed login"},
		{`index=_audit action=IN | where like(user, "adm%")`, "user", "", ""},
		{`index=_audit action=IN | stats count by user | where count>5`, "count", ">", "5"},
	} {
		if result := ExtractConditions(tc.query); !has(result, tc.field, tc.operator, tc.value) && !reported(result) {
			t.Errorf("%s: the %s predicate is lost and nothing reports it: %q", tc.query, tc.field, list(result))
		}
	}

	query := `index=_audit action=IN NOT (info=granted OR info=completed)`
	if result := ExtractConditions(query); result.Expression == nil && !reported(result) {
		t.Errorf("%s: no Expression and nothing reports it; the flat list reads NOT a OR NOT b: %q", query, list(result))
	}
	if semantic := AnalyzeQuery(query); semantic.Expression != nil && !semantic.ExpressionIncomplete && len(semantic.Errors) == 0 {
		t.Errorf("%s: AnalyzeQuery built an expression from the flat recovered list", query)
	}

	query = `index=_audit action=IN | join user [search index=b status=failed]`
	if result := ExtractConditions(query); has(result, "status", "=", "failed") {
		t.Errorf("%s: the join subsearch's status=failed is listed as a main-query condition: %q", query, list(result))
	}
}
