package spl

import "testing"

func TestAnalyzeQueryBuildsConditionBearingSemanticExpression(t *testing.T) {
	semantic := AnalyzeQuery(`search (CommandLine="*whoami*" OR Image="*\\cmd.exe") NOT User="SYSTEM"`)
	if semantic == nil {
		t.Fatal("expected semantic query")
	}
	if len(semantic.Errors) > 0 {
		t.Fatalf("unexpected parser errors: %v", semantic.Errors)
	}
	if len(semantic.Conditions) != 3 {
		t.Fatalf("conditions = %#v, want 3", semantic.Conditions)
	}
	if semantic.Expression == nil || semantic.Expression.Operator != BooleanAnd {
		t.Fatalf("expression = %#v, want AND root", semantic.Expression)
	}
	if len(semantic.Expression.Children) != 2 {
		t.Fatalf("AND children = %#v, want 2", semantic.Expression.Children)
	}
	left := semantic.Expression.Children[0]
	if left.Operator != BooleanOr || len(left.Children) != 2 {
		t.Fatalf("left expression = %#v, want OR with two children", left)
	}
	if got := left.Children[0].Condition.Field; got != "CommandLine" {
		t.Fatalf("left first field = %q, want CommandLine", got)
	}
	if got := left.Children[1].Condition.Field; got != "Image" {
		t.Fatalf("left second field = %q, want Image", got)
	}
	right := semantic.Expression.Children[1]
	if right.Operator != BooleanCondition || right.Condition == nil || right.Condition.Field != "User" || !right.Condition.Negated {
		t.Fatalf("right expression = %#v, want negated User condition", right)
	}
}

func TestSemanticInternalExtractionBuildsLogicalOpsAndClones(t *testing.T) {
	result := &ParseResult{
		Conditions: []Condition{
			{Field: "CommandLine", Operator: "=", Value: "*whoami*", Alternatives: []string{"*whoami*", "*hostname*"}},
			{Field: "Image", Operator: "=", Value: "*cmd.exe", LogicalOp: "OR"},
		},
		GroupByFields:  []string{"user"},
		ComputedFields: map[string]string{"Alias": "CommandLine"},
		FieldAliases:   map[string]string{"NewUser": "user"},
		Commands:       []string{"search"},
	}

	semantic := semanticFromExtraction(result)
	if semantic == nil {
		t.Fatal("expected semantic query")
	}
	if semantic.Expression == nil || semantic.Expression.Operator != BooleanOr || len(semantic.Expression.Children) != 2 {
		t.Fatalf("internal expression = %#v, want OR over two conditions", semantic.Expression)
	}

	result.Conditions[0].Field = "Changed"
	result.Conditions[0].Alternatives[0] = "changed"
	result.GroupByFields[0] = "changed"
	result.ComputedFields["Alias"] = "Changed"
	result.FieldAliases["NewUser"] = "Changed"
	result.Commands[0] = "changed"
	if semantic.Conditions[0].Field != "CommandLine" ||
		semantic.Conditions[0].Alternatives[0] != "*whoami*" ||
		semantic.GroupByFields[0] != "user" ||
		semantic.ComputedFields["Alias"] != "CommandLine" ||
		semantic.FieldAliases["NewUser"] != "user" ||
		semantic.Commands[0] != "search" {
		t.Fatalf("semantic query did not clone parse result: %#v", semantic)
	}
}

func TestSemanticInternalExtractionPreservesNestedJoinAndDirectSubsearches(t *testing.T) {
	semantic := semanticFromExtraction(&ParseResult{
		Joins: []JoinInfo{{
			Type:       "left",
			JoinFields: []string{"user"},
			Options:    map[string]string{"type": "left"},
			Subsearch: &ParseResult{
				Conditions: []Condition{{Field: "status", Operator: "=", Value: "failed"}},
			},
			IsAppend:      true,
			ExposedFields: []string{"status"},
		}},
		Subsearches: []*ParseResult{{
			Conditions: []Condition{{Field: "src_ip", Operator: "=", Value: "1.2.3.4"}},
		}},
	})
	if semantic == nil || len(semantic.Joins) != 1 || len(semantic.Subsearches) != 1 {
		t.Fatalf("semantic subqueries = %#v, want one join and one direct subsearch", semantic)
	}
	join := semantic.Joins[0]
	if !join.IsAppend || join.Options["type"] != "left" || join.Subquery == nil || join.Subquery.Expression == nil {
		t.Fatalf("semantic join = %#v", join)
	}
	if got := join.Subquery.Expression.Condition.Field; got != "status" {
		t.Fatalf("join subquery field = %q, want status", got)
	}
	if got := semantic.Subsearches[0].Expression.Condition.Field; got != "src_ip" {
		t.Fatalf("direct subsearch field = %q, want src_ip", got)
	}
}

func TestSemanticExpressionConversionEdges(t *testing.T) {
	if got := semanticFromExtraction(nil); got != nil {
		t.Fatalf("nil parse result converted to %#v", got)
	}

	conditions := []Condition{
		{Field: "A", Operator: "=", Value: "1"},
		{Field: "B", Operator: "=", Value: "2"},
	}
	for name, expression := range map[string]*ConditionExpression{
		"bad_index": {
			Operator:       BooleanCondition,
			ConditionIndex: len(conditions),
		},
		"empty_boolean": {
			Operator: BooleanAnd,
		},
		"bad_child": {
			Operator: BooleanOr,
			Children: []*ConditionExpression{
				{Operator: BooleanCondition, ConditionIndex: 0},
				{Operator: BooleanCondition, ConditionIndex: len(conditions)},
			},
		},
		"unknown_operator": {
			Operator: "xor",
		},
	} {
		t.Run(name, func(t *testing.T) {
			if got := semanticExpressionFromParseExpression(expression, conditions); got != nil {
				t.Fatalf("invalid expression converted to %#v", got)
			}
		})
	}

	leafA := &SemanticExpression{Operator: BooleanCondition, Condition: &conditions[0]}
	leafB := &SemanticExpression{Operator: BooleanCondition, Condition: &conditions[1]}
	if got := combineSemanticExpressions(BooleanAnd, nil, leafA); got != leafA {
		t.Fatalf("nil left combine = %#v, want leafA", got)
	}
	if got := combineSemanticExpressions(BooleanAnd, leafA, nil); got != leafA {
		t.Fatalf("nil right combine = %#v, want leafA", got)
	}
	combined := combineSemanticExpressions(
		BooleanAnd,
		&SemanticExpression{Operator: BooleanAnd, Children: []*SemanticExpression{leafA}},
		&SemanticExpression{Operator: BooleanAnd, Children: []*SemanticExpression{leafB}},
	)
	if combined.Operator != BooleanAnd || len(combined.Children) != 2 {
		t.Fatalf("flattened combine = %#v, want AND with two children", combined)
	}
}
