package spl

import (
	"reflect"
	"testing"
)

func leaf(index int) *ConditionExpression {
	return &ConditionExpression{Operator: BooleanCondition, ConditionIndex: index}
}

func branches(op BooleanOperator, children ...*ConditionExpression) *ConditionExpression {
	return &ConditionExpression{Operator: op, Children: children}
}

func requireProjection(t *testing.T, query string, want []Condition, expression *ConditionExpression) {
	t.Helper()
	got := ExtractConditions(query)
	if len(got.Errors) != 0 || !reflect.DeepEqual(got.Conditions, want) || !reflect.DeepEqual(got.Expression, expression) {
		t.Fatalf("ExtractConditions(%q) = conditions %#v, expression %#v, errors %v; want %#v, %#v", query, got.Conditions, got.Expression, got.Errors, want, expression)
	}
}

func TestResourceAppendRetainsDistinctMembershipAndExpression(t *testing.T) {
	first := Condition{Field: "x", Operator: "in", Value: "a", Alternatives: []string{"a", "b"}, LogicalOp: "AND"}
	second := Condition{Field: "x", Operator: "in", Value: "a", Alternatives: []string{"a", "c"}, LogicalOp: "AND", PipeStage: 1}
	query := `search x IN ("a","b") | where x IN ("a","c")`
	requireProjection(t, query, []Condition{first, second}, branches(BooleanAnd, leaf(0), leaf(1)))
	resource := Condition{Field: "_spl.outputlookup", Operator: "=", Value: "results.csv", PipeStage: 2, LogicalOp: "AND"}
	requireProjection(t, query+` | outputlookup results.csv`, []Condition{first, second, resource}, branches(BooleanAnd, leaf(0), leaf(1)))
}

func TestResourceAppendRetainsComputedFilter(t *testing.T) {
	query := `search x="a" | eval x=y | where x="a" | outputlookup results.csv`
	got := ExtractConditions(query)
	if len(got.Errors) != 0 || len(got.Conditions) != 3 || got.Conditions[0].PipeStage == got.Conditions[1].PipeStage ||
		got.Conditions[2].Field != "_spl.outputlookup" || !reflect.DeepEqual(got.Expression, branches(BooleanAnd, leaf(0), leaf(1))) {
		t.Fatalf("computed filter/resource projection: conditions %#v expression %#v errors %v", got.Conditions, got.Expression, got.Errors)
	}
}

func TestResourceAppendRetainsSameStageMembershipIntersection(t *testing.T) {
	query := `search x IN ("a","b") AND x IN ("a","c") | outputlookup results.csv`
	want := []Condition{
		{Field: "x", Operator: "in", Value: "a", Alternatives: []string{"a", "b"}, LogicalOp: "AND"},
		{Field: "x", Operator: "in", Value: "a", Alternatives: []string{"a", "c"}, LogicalOp: "AND"},
		{Field: "_spl.outputlookup", Operator: "=", Value: "results.csv", PipeStage: 1, LogicalOp: "AND"},
	}
	requireProjection(t, query, want, branches(BooleanAnd, leaf(0), leaf(1)))
	semantic := AnalyzeQuery(query)
	if len(semantic.Errors) != 0 || len(semantic.Conditions) != 3 || semantic.Expression == nil ||
		semantic.Expression.Operator != BooleanAnd || len(semantic.Expression.Children) != 2 ||
		semantic.Expression.Children[0].Condition == nil || semantic.Expression.Children[1].Condition == nil ||
		semantic.Expression.Children[1].Condition.Field != "x" {
		t.Fatalf("semantic resource append = %#v", semantic)
	}
	for _, row := range []struct {
		x    string
		want bool
	}{{"a", true}, {"b", false}, {"c", false}, {"other", false}} {
		inFirst := row.x == "a" || row.x == "b"
		inSecond := row.x == "a" || row.x == "c"
		if got := inFirst && inSecond; got != row.want {
			t.Fatalf("membership intersection x=%s = %v, want %v", row.x, got, row.want)
		}
	}
}

func TestResourceAppendRemapsExactDuplicate(t *testing.T) {
	query := `search x=a AND x=a | outputlookup results.csv`
	want := []Condition{
		{Field: "x", Operator: "=", Value: "a", LogicalOp: "AND"},
		{Field: "_spl.outputlookup", Operator: "=", Value: "results.csv", PipeStage: 1, LogicalOp: "AND"},
	}
	requireProjection(t, query, want, leaf(0))
}

func TestConditionDedupStructuralIdentityAndIndexMap(t *testing.T) {
	base := Condition{Field: "User", Operator: "=", Value: "a", LogicalOp: "AND", Alternatives: []string{"a", "b"}}
	variants := []Condition{
		{Field: "user", Operator: "=", Value: "a", LogicalOp: "AND", Alternatives: []string{"a", "b"}},
		{Field: "User", Operator: "!=", Value: "a", LogicalOp: "AND", Alternatives: []string{"a", "b"}},
		{Field: "User", Operator: "=", Value: "b", LogicalOp: "AND", Alternatives: []string{"a", "b"}},
		{Field: "User", Operator: "=", Value: "a", ValueReference: "other", LogicalOp: "AND", Alternatives: []string{"a", "b"}},
		{Field: "User", Operator: "=", Value: "a", Negated: true, LogicalOp: "AND", Alternatives: []string{"a", "b"}},
		{Field: "User", Operator: "=", Value: "a", PipeStage: 1, LogicalOp: "AND", Alternatives: []string{"a", "b"}},
		{Field: "User", Operator: "=", Value: "a", LogicalOp: "OR", Alternatives: []string{"a", "b"}},
		{Field: "User", Operator: "=", Value: "a", LogicalOp: "AND", Alternatives: []string{"b", "a"}},
		{Field: "User", Operator: "=", Value: "a", LogicalOp: "AND", Alternatives: []string{"a", "c"}},
		{Field: "User", Operator: "=", Value: "a", LogicalOp: "AND", Alternatives: []string{"a", "b"}, IsComputed: true},
		{Field: "User", Operator: "=", Value: "a", LogicalOp: "AND", Alternatives: []string{"a", "b"}, SourceField: "raw"},
	}
	input := append([]Condition{base}, variants...)
	input = append(input, base)
	got, indexMap := deduplicateConditionsWithIndexMap(input, nil)
	if !reflect.DeepEqual(got, input[:len(input)-1]) || len(indexMap) != len(input) || indexMap[len(indexMap)-1] != 0 {
		t.Fatalf("structural dedup = %#v map %v", got, indexMap)
	}
	for i := range variants {
		if indexMap[i+1] != i+1 {
			t.Fatalf("variant %d mapped to %d", i, indexMap[i+1])
		}
	}
}

func TestConditionDedupAlternativesEncodingIsInjective(t *testing.T) {
	base := Condition{Field: "x", Operator: "=", Value: "v", Alternatives: []string{"a\x00b", "c"}}
	inputs := []Condition{base, {Field: "x", Operator: "=", Value: "v", Alternatives: []string{"a", "b\x00c"}},
		{Field: "x", Operator: "=", Value: "v", Alternatives: []string{"", "a\x00b", "c"}},
		{Field: "x", Operator: "=", Value: "v", Alternatives: []string{"\xff"}}, base}
	got, indexMap := deduplicateConditionsWithIndexMap(inputs, nil)
	if len(got) != 4 || !reflect.DeepEqual(indexMap, []int{0, 1, 2, 3, 0}) {
		t.Fatalf("encoded alternatives = %#v map %v", got, indexMap)
	}
}
