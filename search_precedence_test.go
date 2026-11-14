package spl

import (
	"strings"
	"testing"
)

// Splunk search evaluates NOT, then OR, then explicit or implicit AND;
// where evaluates NOT, then AND, then OR. Parentheses override both orders.
// https://help.splunk.com/en/splunk-enterprise/search/search-manual/9.4/expressions-and-predicates/boolean-expressions-with-logical-operators
func TestSearchBooleanPrecedence(t *testing.T) {
	cases := []struct {
		name, query, want string
		matches           func(a, b, c, d bool) bool
	}{
		{"and_or", `a=1 AND b=2 OR c=3`, "and(a,or(b,c))", func(a, b, c, d bool) bool { return a && (b || c) }},
		{"implicit_and_or", `a=1 b=2 OR c=3`, "and(a,or(b,c))", func(a, b, c, d bool) bool { return a && (b || c) }},
		{"or_and", `a=1 OR b=2 AND c=3`, "and(or(a,b),c)", func(a, b, c, d bool) bool { return (a || b) && c }},
		{"and_or_and", `a=1 AND b=2 OR c=3 AND d=4`, "and(a,or(b,c),d)", func(a, b, c, d bool) bool { return a && (b || c) && d }},
		{"or_and_or", `a=1 OR b=2 AND c=3 OR d=4`, "and(or(a,b),or(c,d))", func(a, b, c, d bool) bool { return (a || b) && (c || d) }},
		{"mixed_implicit_and", `a=1 b=2 OR c=3 d=4`, "and(a,or(b,c),d)", func(a, b, c, d bool) bool { return a && (b || c) && d }},
		{"not_term", `a=1 AND NOT b=2 OR c=3`, "and(a,or(!b,c))", func(a, b, c, d bool) bool { return a && (!b || c) }},
		{"not_group", `NOT (a=1 AND b=2 OR c=3)`, "or(!a,and(!b,!c))", func(a, b, c, d bool) bool { return !(a && (b || c)) }},
		{"parenthesized_and", `(a=1 AND b=2) OR c=3`, "or(and(a,b),c)", func(a, b, c, d bool) bool { return (a && b) || c }},
		{"parenthesized_or", `a=1 AND (b=2 OR c=3)`, "and(a,or(b,c))", func(a, b, c, d bool) bool { return a && (b || c) }},
	}
	for _, tc := range cases {
		for _, prefix := range []string{"", "search ", "| makeresults | search "} {
			t.Run(tc.name+"/"+prefix, func(t *testing.T) {
				query := prefix + tc.query
				result := ExtractConditions(query)
				if len(result.Errors) > 0 {
					t.Fatalf("parse errors: %v", result.Errors)
				}
				if got := precedenceParseShape(result.Expression, result.Conditions); got != tc.want {
					t.Errorf("extracted tree = %s, want %s", got, tc.want)
				}
				semantic := AnalyzeQuery(query)
				if len(semantic.Errors) > 0 {
					t.Fatalf("semantic errors: %v", semantic.Errors)
				}
				if got := precedenceSemanticShape(semantic.Expression); got != tc.want {
					t.Errorf("semantic tree = %s, want %s", got, tc.want)
				}
				for bits := 0; bits < 16; bits++ {
					a, b, c, d := bits&1 != 0, bits&2 != 0, bits&4 != 0, bits&8 != 0
					values := map[string]bool{"a": a, "b": b, "c": c, "d": d}
					if got, want := precedenceMatches(semantic.Expression, values), tc.matches(a, b, c, d); got != want {
						t.Errorf("truth row a=%t b=%t c=%t d=%t: got %t want %t", a, b, c, d, got, want)
					}
				}
			})
		}
	}
}

func TestWhereBooleanPrecedenceRemainsSeparate(t *testing.T) {
	for _, tc := range []struct{ query, want string }{
		{`a=1 AND b=2 OR c=3`, "or(and(a,b),c)"},
		{`a=1 OR b=2 AND c=3`, "or(a,and(b,c))"},
		{`a=1 OR b=2 AND c=3 OR d=4`, "or(a,and(b,c),d)"},
		{`a=1 AND NOT b=2 OR c=3`, "or(and(a,!b),c)"},
		{`a=1 AND (b=2 OR c=3)`, "and(a,or(b,c))"},
	} {
		t.Run(tc.query, func(t *testing.T) {
			result := ExtractConditions("| where " + tc.query)
			if len(result.Errors) > 0 {
				t.Fatalf("parse errors: %v", result.Errors)
			}
			if got := precedenceParseShape(result.Expression, result.Conditions); got != tc.want {
				t.Fatalf("tree = %s, want %s", got, tc.want)
			}
			semantic := AnalyzeQuery("| where " + tc.query)
			if got := precedenceSemanticShape(semantic.Expression); got != tc.want {
				t.Fatalf("semantic tree = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestBooleanPrecedenceAcrossPipelineStages(t *testing.T) {
	query := `a=1 AND b=2 OR c=3 | search d=4 AND e=5 OR f=6 | where g=7 AND h=8 OR i=9`
	result := ExtractConditions(query)
	if len(result.Errors) > 0 {
		t.Fatalf("parse errors: %v", result.Errors)
	}
	want := "and(a,or(b,c),d,or(e,f),or(and(g,h),i))"
	if got := precedenceParseShape(result.Expression, result.Conditions); got != want {
		t.Fatalf("tree = %s, want %s", got, want)
	}
	if got := precedenceSemanticShape(AnalyzeQuery(query).Expression); got != want {
		t.Fatalf("semantic tree = %s, want %s", got, want)
	}
}

func TestSearchBooleanPrecedenceWithSameFieldDisjunction(t *testing.T) {
	result := ExtractConditions(`a=1 AND b=2 OR b=3`)
	if len(result.Errors) > 0 {
		t.Fatalf("parse errors: %v", result.Errors)
	}
	if got := precedenceParseShape(result.Expression, result.Conditions); got != "and(a,or(b,b))" {
		t.Fatalf("tree = %s, want and(a,or(b,b))", got)
	}
	requireConditionIndex(t, result.Expression.Children[1].Children[0], 1)
	requireConditionIndex(t, result.Expression.Children[1].Children[1], 2)
	if len(result.Conditions) != 3 || result.Conditions[1].Value != "2" || result.Conditions[2].Value != "3" {
		t.Fatalf("conditions = %#v, want a and separate b=2/b=3 leaves", result.Conditions)
	}
}

func precedenceParseShape(expr *ConditionExpression, conditions []Condition) string {
	if expr == nil {
		return "nil"
	}
	if expr.Operator == BooleanCondition {
		if expr.ConditionIndex < 0 || expr.ConditionIndex >= len(conditions) {
			return "invalid index"
		}
		return precedenceLeafShape(conditions[expr.ConditionIndex])
	}
	children := make([]string, 0, len(expr.Children))
	for _, child := range expr.Children {
		children = append(children, precedenceParseShape(child, conditions))
	}
	return string(expr.Operator) + "(" + strings.Join(children, ",") + ")"
}

func precedenceSemanticShape(expr *SemanticExpression) string {
	if expr == nil {
		return "nil"
	}
	if expr.Operator == BooleanCondition {
		if expr.Condition == nil {
			return "invalid condition"
		}
		return precedenceLeafShape(*expr.Condition)
	}
	children := make([]string, 0, len(expr.Children))
	for _, child := range expr.Children {
		children = append(children, precedenceSemanticShape(child))
	}
	return string(expr.Operator) + "(" + strings.Join(children, ",") + ")"
}

func precedenceLeafShape(condition Condition) string {
	if condition.Negated {
		return "!" + condition.Field
	}
	return condition.Field
}

func precedenceMatches(expr *SemanticExpression, values map[string]bool) bool {
	if expr == nil {
		return false
	}
	if expr.Operator == BooleanCondition {
		if expr.Condition == nil {
			return false
		}
		value := values[expr.Condition.Field]
		if expr.Condition.Negated {
			return !value
		}
		return value
	}
	if expr.Operator == BooleanAnd {
		for _, child := range expr.Children {
			if !precedenceMatches(child, values) {
				return false
			}
		}
		return true
	}
	for _, child := range expr.Children {
		if precedenceMatches(child, values) {
			return true
		}
	}
	return false
}
