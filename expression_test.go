package spl

import "testing"

func TestExpressionPreservesGroupedNotAnd(t *testing.T) {
	result := ExtractConditions(`index=events sourcetype=test:process NOT ((CommandLine="*whoami*" AND Image="*cmd.exe" AND ParentImage="*winword.exe"))`)
	if result.Expression == nil {
		t.Fatalf("expected expression, got nil with conditions %#v errors %v", result.Conditions, result.Errors)
	}
	if result.Expression.Operator != BooleanAnd {
		t.Fatalf("expected top-level scope conjunction, got %#v", result.Expression)
	}
	if len(result.Expression.Children) != 3 || result.Expression.Children[2].Operator != BooleanOr {
		t.Fatalf("expected grouped NOT AND branch to become OR of negated leaves, got %#v", result.Expression)
	}
	if len(result.Expression.Children[2].Children) != 3 {
		t.Fatalf("expected three detection children, got %#v", result.Expression)
	}
	for _, condition := range result.Conditions[2:] {
		if !condition.Negated {
			t.Fatalf("expected condition to remain negated in flat output: %#v", result.Conditions)
		}
	}
}

func TestExpressionPreservesNegatedSameFieldOr(t *testing.T) {
	result := ExtractConditions(`NOT (process="*whoami*") OR NOT (process="*/priv*")`)
	if result.Expression == nil {
		t.Fatalf("expected expression, got nil conditions %#v errors %v", result.Conditions, result.Errors)
	}
	if result.Expression.Operator != BooleanOr || len(result.Expression.Children) != 2 {
		t.Fatalf("expected OR over two negated leaves, got %#v", result.Expression)
	}
	if len(result.Conditions) != 2 {
		t.Fatalf("expected two separate negated conditions, got %#v", result.Conditions)
	}
	for _, condition := range result.Conditions {
		if !condition.Negated || len(condition.Alternatives) != 0 {
			t.Fatalf("negated OR leaves must not merge into alternatives: %#v", result.Conditions)
		}
	}
}

func TestWhereExpressionPreservesMixedOrAndNot(t *testing.T) {
	result := ExtractConditions(`index=windows | where (match(process, ".*whoami.*") OR match(process_name, ".*\\\\cmd\\.exe$")) AND NOT (user=="SYSTEM")`)
	if result.Expression == nil {
		t.Fatalf("expected expression, got nil conditions %#v errors %v", result.Conditions, result.Errors)
	}
	if len(result.Conditions) != 4 {
		t.Fatalf("expected index plus three where conditions, got %#v", result.Conditions)
	}
	if result.Expression.Operator != BooleanAnd || len(result.Expression.Children) != 3 {
		t.Fatalf("expected index AND grouped OR AND negated equality, got %#v", result.Expression)
	}
	requireConditionIndex(t, result.Expression.Children[0], 0)
	orBranch := result.Expression.Children[1]
	if orBranch.Operator != BooleanOr || len(orBranch.Children) != 2 {
		t.Fatalf("expected grouped OR branch, got %#v", orBranch)
	}
	requireConditionIndex(t, orBranch.Children[0], 1)
	requireConditionIndex(t, orBranch.Children[1], 2)
	requireConditionIndex(t, result.Expression.Children[2], 3)
	if !result.Conditions[3].Negated {
		t.Fatalf("expected NOT equality to remain negated in flat output: %#v", result.Conditions)
	}
}
