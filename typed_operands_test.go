package spl

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestWhereTypedOperandRoles(t *testing.T) {
	tests := []struct {
		query, field, op string
		rhs              OperandKind
		value            string
	}{
		{`| where x>42`, "x", ">", OperandNumber, "42"},
		{`| where x>"42"`, "x", ">", OperandString, "42"},
		{`| where x='user-id'`, "x", "=", OperandField, "user-id"},
		{`| where 42<x`, "x", ">", OperandNumber, "42"},
		{`| where 42<=x`, "x", ">=", OperandNumber, "42"},
		{`| where "a"=x`, "x", "=", OperandString, "a"},
		{`| where -3.5<x`, "x", ">", OperandNumber, "-3.5"},
		{`| where 'user-id'="a"`, "user-id", "=", OperandString, "a"},
		{`| where '42'>5`, "42", ">", OperandNumber, "5"},
	}
	for _, tt := range tests {
		r := AnalyzeQuery(tt.query)
		if len(r.Errors) != 0 || len(r.Warnings) != 0 || len(r.Conditions) != 1 {
			t.Fatalf("%q: %#v", tt.query, r)
		}
		c := r.Conditions[0]
		if c.Field != tt.field || c.Operator != tt.op || c.Context != PredicateEval || c.LHS == nil || c.LHS.Kind != OperandField || c.RHS == nil || c.RHS.Kind != tt.rhs || c.RHS.Value != tt.value {
			t.Errorf("%q: got %#v", tt.query, c)
		}
	}
}

func TestWhereScalarRetainsSource(t *testing.T) {
	for _, query := range []string{`| where 5<10`, `| where x + 1 > 42`} {
		r := AnalyzeQuery(query)
		if len(r.Conditions) != 0 || r.Expression == nil || r.Expression.Operator != BooleanScalar || r.Expression.Scalar == nil || r.Expression.Scalar.Expression != query[len(`| where `):] || len(r.Warnings) != 0 || len(r.Errors) != 0 {
			t.Errorf("%q: %#v", query, r)
		}
	}
}

func TestWhereScalarNegationAndMixedTree(t *testing.T) {
	r := AnalyzeQuery(`| where NOT (5<10) OR x="a"`)
	if len(r.Errors) != 0 || len(r.Warnings) != 0 || r.Expression == nil || r.Expression.Operator != BooleanOr || len(r.Expression.Children) != 2 {
		t.Fatalf("mixed tree: %#v", r)
	}
	left := r.Expression.Children[0]
	if left.Operator != BooleanScalar || left.Scalar == nil || left.Scalar.Expression != `5<10` || !left.Scalar.Negated {
		t.Errorf("negated scalar: %#v", left)
	}
	if len(r.Conditions) != 1 || r.Conditions[0].Field != "x" || r.Expression.Children[1].Operator != BooleanCondition {
		t.Errorf("field branch: %#v", r)
	}
}

func TestWhereFunctionOperandRoles(t *testing.T) {
	r := AnalyzeQuery(`| where like('user-id', "a%b") OR match(x,"^a") OR isnull('42')`)
	if len(r.Errors) != 0 || len(r.Warnings) != 0 || len(r.Conditions) != 3 {
		t.Fatalf("functions: %#v", r)
	}
	if r.Conditions[0].Field != "user-id" || r.Conditions[0].LHS == nil || r.Conditions[0].LHS.Source != `'user-id'` || r.Conditions[0].RHS == nil || r.Conditions[0].RHS.Value != "a%b" || r.Conditions[0].Value != "a*b" {
		t.Errorf("like: %#v", r.Conditions[0])
	}
	if r.Conditions[2].Field != "42" || r.Conditions[2].RHS != nil {
		t.Errorf("null: %#v", r.Conditions[2])
	}
	s := AnalyzeQuery(`| where like(x,'pattern')`)
	if len(s.Conditions) != 0 || s.Expression == nil || s.Expression.Operator != BooleanScalar {
		t.Errorf("single-quoted pattern must be opaque: %#v", s)
	}
}

func TestAuthoredOperandAndScalarSource(t *testing.T) {
	r := AnalyzeQuery("search msg=\"é\" | where x==\"a\\\"b\"")
	if len(r.Conditions) != 2 || r.Conditions[1].Operator != "==" || r.Conditions[1].RHS == nil || r.Conditions[1].RHS.Source != `"a\"b"` {
		t.Errorf("unicode and double equals: %#v", r)
	}
	s := AnalyzeQuery("search msg=\"é\" | where x + ``` note ``` 1 > 42")
	if s.Expression == nil || len(s.Expression.Children) != 2 || s.Expression.Children[1].Scalar == nil || s.Expression.Children[1].Scalar.Expression != "x + ``` note ``` 1 > 42" {
		t.Errorf("comment source: %#v", s)
	}
}

func TestUnresolvedBareEvalNamesRemainScalar(t *testing.T) {
	for _, query := range []string{`| where x=true`, `| where x=false`, `| where x=null`, `| where true=x`} {
		r := AnalyzeQuery(query)
		if len(r.Conditions) != 0 || r.Expression == nil || r.Expression.Operator != BooleanScalar || r.Expression.Scalar == nil {
			t.Errorf("%q: %#v", query, r)
		}
	}
	r := AnalyzeQuery(`| where x='true'`)
	if len(r.Conditions) != 1 || r.Conditions[0].RHS == nil || r.Conditions[0].RHS.Kind != OperandField {
		t.Errorf("quoted field: %#v", r)
	}
}

func TestMalformedWhereIsNotCleanScalar(t *testing.T) {
	for _, query := range []string{`| where x >`, `| where x="unterminated`, `| where match(x,)`} {
		r := ExtractConditions(query)
		if len(r.Errors) == 0 && len(r.Warnings) == 0 {
			t.Errorf("%q diagnosed no malformed syntax: %#v", query, r)
		}
		if containsScalarExpression(r.Expression) && !r.ExpressionIncomplete {
			t.Errorf("%q marked malformed scalar complete: %#v", query, r)
		}
	}
}

func TestSearchAndEvalOperandContextsDiffer(t *testing.T) {
	r := AnalyzeQuery(`search 3=3 x=y x="y" | where x=y`)
	if len(r.Conditions) != 4 {
		t.Fatalf("conditions: %#v", r)
	}
	if r.Conditions[0].Field != "3" || r.Conditions[0].Context != PredicateSearch || r.Conditions[0].LHS == nil || r.Conditions[0].LHS.Kind != OperandField || r.Conditions[0].RHS == nil || r.Conditions[0].RHS.Kind != OperandNumber {
		t.Errorf("numeric search field: %#v", r.Conditions[0])
	}
	if r.Conditions[1].RHS == nil || r.Conditions[1].RHS.Kind != OperandBare || r.Conditions[1].ValueReference != "" {
		t.Errorf("bare search value: %#v", r.Conditions[1])
	}
	if r.Conditions[3].Context != PredicateEval || r.Conditions[3].RHS == nil || r.Conditions[3].RHS.Kind != OperandField || r.Conditions[3].ValueReference != "y" {
		t.Errorf("eval reference: %#v", r.Conditions[3])
	}
}

func TestScalarSourceSurvivesNormalizationEdits(t *testing.T) {
	tests := []struct {
		query, source string
		negated       bool
	}{
		{"| where x + \\\n1 > 42", "x + \\\n1 > 42", false},
		{"| where x +\u00a01 > 42", "x +\u00a01 > 42", false},
		{`| where !match(x,'pattern')`, `match(x,'pattern')`, true},
	}
	for _, tt := range tests {
		r := AnalyzeQuery(tt.query)
		if r.Expression == nil || r.Expression.Scalar == nil || r.Expression.Scalar.Expression != tt.source || r.Expression.Scalar.Negated != tt.negated {
			t.Errorf("%q: %#v", tt.query, r)
		}
	}
}

func TestLongCommentSourceSpan(t *testing.T) {
	for _, comment := range []string{"```" + strings.Repeat("note ", 40) + "```", "```" + strings.Repeat(" 1 > 42", 20) + "```"} {
		atom := "x + " + comment + " 1 > 42"
		r := AnalyzeQuery("| where " + atom)
		if r.Expression == nil || r.Expression.Scalar == nil || r.Expression.Scalar.Expression != atom {
			t.Fatalf("long comment: %#v", r)
		}
	}
}

func TestTypedORGroupingRetainsDistinctItems(t *testing.T) {
	r := AnalyzeQuery(`| where x=42 OR x="42"`)
	if len(r.Conditions) != 1 || r.Conditions[0].RHS == nil || r.Conditions[0].RHS.Kind != OperandList || len(r.Conditions[0].RHS.Items) != 2 || len(r.Conditions[0].Alternatives) != 2 {
		t.Fatalf("typed alternatives: %#v", r)
	}
	if r.Conditions[0].RHS.Items[0].Kind != OperandNumber || r.Conditions[0].RHS.Items[1].Kind != OperandString || r.Conditions[0].RHS.Source != "" {
		t.Errorf("typed items: %#v", r.Conditions[0].RHS)
	}
	references := AnalyzeQuery(`| where x=y OR x="y"`)
	if len(references.Conditions) != 2 || references.Expression == nil || references.Expression.Operator != BooleanOr {
		t.Errorf("reference barrier: %#v", references)
	}
	scalars := AnalyzeQuery(`| where x="a" OR 5<10 OR x="b"`)
	if len(scalars.Conditions) != 2 || scalars.Expression == nil || scalars.Expression.Operator != BooleanOr || len(scalars.Expression.Children) != 3 || scalars.Expression.Children[1].Operator != BooleanScalar {
		t.Errorf("scalar barrier: %#v", scalars)
	}
}

func TestTypedSemanticMutationOwnershipAndJSON(t *testing.T) {
	r := AnalyzeQuery(`| where x IN (42,"42") OR 5<10`)
	if len(r.Conditions) != 1 || r.Conditions[0].RHS == nil || len(r.Conditions[0].RHS.Items) != 2 || r.Expression == nil || r.Expression.Operator != BooleanOr {
		t.Fatalf("model: %#v", r)
	}
	encoded, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var restored SemanticQuery
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Expression == nil || restored.Expression.Children[1].Scalar == nil || restored.Expression.Children[1].Scalar.Expression != "5<10" || restored.Conditions[0].RHS.Items[1].Kind != OperandString {
		t.Errorf("round trip: %#v", restored)
	}
	leaf := r.Expression.Children[0].Condition
	if leaf == nil || leaf.RHS == nil {
		t.Fatalf("condition leaf: %#v", r.Expression)
	}
	r.Conditions[0].RHS.Items[0].Value = "changed"
	if leaf.RHS.Items[0].Value == "changed" {
		t.Error("semantic leaf aliases flat condition metadata")
	}
	other := AnalyzeQuery(`| where x IN (42,"42") OR 5<10`)
	if other.Conditions[0].RHS.Items[0].Value == "changed" {
		t.Error("independent parse results alias")
	}
}

func TestRepeatedScalarOccurrencesKeepStages(t *testing.T) {
	r := AnalyzeQuery(`| where 5<10 | where 5<10`)
	if len(r.Errors) != 0 || len(r.Warnings) != 0 || r.Expression == nil || r.Expression.Operator != BooleanAnd || len(r.Expression.Children) != 2 {
		t.Fatalf("repeated scalars: %#v", r)
	}
	if r.Expression.Children[0].Scalar == nil || r.Expression.Children[1].Scalar == nil ||
		r.Expression.Children[0].Scalar.PipeStage != 0 || r.Expression.Children[1].Scalar.PipeStage != 1 {
		t.Errorf("stages: %#v", r.Expression)
	}
}

func TestNestedJoinPredicateUsesNestedContextAndSource(t *testing.T) {
	r := AnalyzeQuery(`search x=a | join user [ search z="é" | where 5<10 ]`)
	if len(r.Joins) == 0 || r.Joins[0].Subquery == nil {
		t.Fatalf("join: %#v", r)
	}
	nested := r.Joins[0].Subquery
	if len(nested.Conditions) != 1 || nested.Conditions[0].Context != PredicateSearch ||
		nested.Expression == nil || nested.Expression.Operator != BooleanAnd || len(nested.Expression.Children) != 2 ||
		nested.Expression.Children[1].Scalar == nil || nested.Expression.Children[1].Scalar.Expression != "5<10" {
		t.Errorf("nested predicate: %#v", nested)
	}
}

func TestWildcardDedupUsesSearchRole(t *testing.T) {
	conditions := []Condition{
		{Field: "x", Context: PredicateSearch, RHS: &Operand{Kind: OperandBare, Value: "*"}, Operator: "=", Value: "*"},
		{Field: "x", Context: PredicateEval, RHS: &Operand{Kind: OperandString, Source: `"*"`, Value: "*"}, Operator: "=", Value: "*"},
	}
	got := DeduplicateConditions(conditions)
	if len(got) != 1 || got[0].Context != PredicateEval || got[0].RHS.Kind != OperandString {
		t.Errorf("wildcards: %#v", got)
	}
}

func TestOptInDedupKeepsDifferentContexts(t *testing.T) {
	got := DeduplicateConditions([]Condition{
		{Field: "x", Context: PredicateSearch, Operator: "=", Value: "42", PipeStage: 0},
		{Field: "x", Context: PredicateEval, Operator: "=", Value: "42", PipeStage: 1},
	})
	if len(got) != 2 {
		t.Fatalf("cross-context conditions collapsed: %#v", got)
	}
}

func TestQuotedLookupResourceKeepsAuthoredRole(t *testing.T) {
	r := AnalyzeQuery(`| inputlookup "risky users.csv"`)
	if len(r.Conditions) != 1 || r.Conditions[0].Context != PredicateResource || r.Conditions[0].RHS == nil ||
		r.Conditions[0].RHS.Kind != OperandString || r.Conditions[0].RHS.Source != `"risky users.csv"` ||
		r.Conditions[0].RHS.Value != "risky users.csv" {
		t.Fatalf("resource role: %#v", r)
	}
}

func TestTypedOutputLimitsReturnOnlyFailure(t *testing.T) {
	for _, query := range []string{`| where x IN (1,2,3)`, `| where 5<10`} {
		limits := defaultParseLimits()
		limits.maxOutputItems = 1
		r := analyzeQueryContext(context.Background(), query, limits)
		if len(r.Errors) != 1 || !strings.Contains(r.Errors[0], "output items") || len(r.Conditions) != 0 || r.Expression != nil {
			t.Errorf("%q: %#v", query, r)
		}
	}
}

func TestUnknownPipelineCommandDoesNotClaimSearchRoles(t *testing.T) {
	r := ExtractConditions(`search x=a | mystery y=z`)
	for _, condition := range r.Conditions {
		if condition.Field == "y" && condition.Context == PredicateSearch {
			t.Fatalf("unknown command claimed search syntax: %#v", r)
		}
	}
}

func TestWhereLiteralLeftDoesNotFabricateNumericField(t *testing.T) {
	for _, query := range []string{`| where 42<x`, `| where 5<10`} {
		result := AnalyzeQuery(query)
		for _, condition := range result.Conditions {
			if condition.Field == "42" || condition.Field == "5" {
				t.Fatalf("%q fabricated numeric field: %#v", query, result.Conditions)
			}
		}
	}
}

func TestRawAdjacentWildcardSearchPredicates(t *testing.T) {
	for _, tc := range []struct {
		query  string
		fields []string
	}{
		{`index=main user=* action=*`, []string{"index", "user", "action"}},
		{`search user=* action=*`, []string{"user", "action"}},
	} {
		parsed := ExtractConditions(tc.query)
		r := AnalyzeQuery(tc.query)
		if len(parsed.Errors) != 0 || len(parsed.Warnings) != 0 || parsed.ExpressionIncomplete ||
			len(r.Errors) != 0 || len(r.Warnings) != 0 || r.Expression == nil || len(r.Conditions) != len(tc.fields) {
			t.Fatalf("raw adjacent wildcards %q: parsed=%#v semantic=%#v", tc.query, parsed, r)
		}
		for i, field := range tc.fields {
			condition := r.Conditions[i]
			if condition.Field != field || condition.Context != PredicateSearch || condition.LHS == nil ||
				condition.LHS.Kind != OperandField || condition.LHS.Source != field || condition.LHS.Value != field ||
				condition.RHS == nil || condition.RHS.Kind != OperandBare || condition.RHS.Source == "" {
				t.Errorf("raw adjacent wildcards %q, condition %d: %#v", tc.query, i, condition)
			}
		}
		for _, condition := range r.Conditions[1:] {
			if condition.RHS.Source != "*" || condition.RHS.Value != "*" {
				t.Errorf("wildcard source %q: %#v", tc.query, condition)
			}
		}
	}
}
