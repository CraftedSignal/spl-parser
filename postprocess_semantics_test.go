package spl

import (
	"context"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func leaf(index int) *ConditionExpression {
	return &ConditionExpression{Operator: BooleanCondition, ConditionIndex: index}
}

func branches(op BooleanOperator, children ...*ConditionExpression) *ConditionExpression {
	return &ConditionExpression{Operator: op, Children: children}
}

func projectionMatches(t *testing.T, expr *ConditionExpression, conditions []Condition, fields map[string]string) bool {
	t.Helper()
	if expr == nil {
		t.Fatal("missing expression")
	}
	switch expr.Operator {
	case BooleanCondition:
		if expr.ConditionIndex < 0 || expr.ConditionIndex >= len(conditions) {
			t.Fatalf("invalid condition index %d", expr.ConditionIndex)
		}
		condition := conditions[expr.ConditionIndex]
		if condition.Operator != "=" && condition.Operator != "in" {
			t.Fatalf("unsupported test operator %q", condition.Operator)
		}
		values := condition.Alternatives
		if len(values) == 0 {
			values = []string{condition.Value}
		}
		for _, value := range values {
			if fields[condition.Field] == value {
				return !condition.Negated
			}
		}
		return condition.Negated
	case BooleanAnd:
		for _, child := range expr.Children {
			if !projectionMatches(t, child, conditions, fields) {
				return false
			}
		}
		return true
	case BooleanOr:
		for _, child := range expr.Children {
			if projectionMatches(t, child, conditions, fields) {
				return true
			}
		}
		return false
	default:
		t.Fatalf("unsupported test expression %q", expr.Operator)
		return false
	}
}

func requireProjection(t *testing.T, query string, want []Condition, expression *ConditionExpression) {
	t.Helper()
	got := ExtractConditions(query)
	if len(got.Errors) != 0 || !reflect.DeepEqual(got.Conditions, want) || !reflect.DeepEqual(got.Expression, expression) {
		t.Fatalf("ExtractConditions(%q) = conditions %#v, expression %#v, errors %v; want %#v, %#v", query, got.Conditions, got.Expression, got.Errors, want, expression)
	}
}

func requireSemanticAndLeaves(t *testing.T, query string, want []Condition, leafFields ...string) {
	t.Helper()
	semantic := AnalyzeQuery(query)
	if len(semantic.Errors) != 0 || !reflect.DeepEqual(semantic.Conditions, want) || semantic.Expression == nil {
		t.Fatalf("AnalyzeQuery(%q) = %#v; want conditions %#v", query, semantic, want)
	}
	if len(leafFields) == 1 {
		if semantic.Expression.Operator != BooleanCondition || semantic.Expression.Condition == nil ||
			semantic.Expression.Condition.Field != leafFields[0] {
			t.Fatalf("AnalyzeQuery(%q) expression = %#v", query, semantic.Expression)
		}
		return
	}
	if semantic.Expression.Operator != BooleanAnd || len(semantic.Expression.Children) != len(leafFields) {
		t.Fatalf("AnalyzeQuery(%q) expression = %#v", query, semantic.Expression)
	}
	for i, field := range leafFields {
		child := semantic.Expression.Children[i]
		if child.Operator != BooleanCondition || child.Condition == nil || child.Condition.Field != field {
			t.Fatalf("AnalyzeQuery(%q) leaf %d = %#v, want %q", query, i, child, field)
		}
	}
}

func TestResourceAppendRetainsDistinctMembershipAndExpression(t *testing.T) {
	first := Condition{Field: "x", Operator: "in", Value: "a", Alternatives: []string{"a", "b"}, LogicalOp: "AND"}
	second := Condition{Field: "x", Operator: "in", Value: "a", Alternatives: []string{"a", "c"}, LogicalOp: "AND", PipeStage: 1}
	query := `search x IN ("a","b") | where x IN ("a","c")`
	requireProjection(t, query, []Condition{first, second}, branches(BooleanAnd, leaf(0), leaf(1)))
	requireSemanticAndLeaves(t, query, []Condition{first, second}, "x", "x")
	resource := Condition{Field: "_spl.outputlookup", Operator: "=", Value: "results.csv", PipeStage: 2, LogicalOp: "AND"}
	requireProjection(t, query+` | outputlookup results.csv`, []Condition{first, second, resource}, branches(BooleanAnd, leaf(0), leaf(1)))
	requireSemanticAndLeaves(t, query+` | outputlookup results.csv`, []Condition{first, second, resource}, "x", "x")
}

func TestResourceAppendRetainsComputedFilter(t *testing.T) {
	query := `search x="a" | eval x=y | where x="a" | outputlookup results.csv`
	got := ExtractConditions(query)
	if len(got.Errors) != 0 || len(got.Conditions) != 3 || got.Conditions[0].PipeStage == got.Conditions[1].PipeStage ||
		got.Conditions[2].Field != "_spl.outputlookup" || !reflect.DeepEqual(got.Expression, branches(BooleanAnd, leaf(0), leaf(1))) {
		t.Fatalf("computed filter/resource projection: conditions %#v expression %#v errors %v", got.Conditions, got.Expression, got.Errors)
	}
	requireSemanticAndLeaves(t, query, got.Conditions, "x", "x")
}

func TestResourceAppendRetainsSameStageMembershipIntersection(t *testing.T) {
	baseQuery := `search x IN ("a","b") AND x IN ("a","c")`
	query := baseQuery + ` | outputlookup results.csv`
	want := []Condition{
		{Field: "x", Operator: "in", Value: "a", Alternatives: []string{"a", "b"}, LogicalOp: "AND"},
		{Field: "x", Operator: "in", Value: "a", Alternatives: []string{"a", "c"}, LogicalOp: "AND"},
		{Field: "_spl.outputlookup", Operator: "=", Value: "results.csv", PipeStage: 1, LogicalOp: "AND"},
	}
	requireProjection(t, baseQuery, want[:2], branches(BooleanAnd, leaf(0), leaf(1)))
	requireSemanticAndLeaves(t, baseQuery, want[:2], "x", "x")
	requireProjection(t, query, want, branches(BooleanAnd, leaf(0), leaf(1)))
	requireSemanticAndLeaves(t, query, want, "x", "x")
	for _, row := range []struct {
		x    string
		want bool
	}{{"a", true}, {"b", false}, {"c", false}, {"other", false}} {
		got := ExtractConditions(query)
		if matched := projectionMatches(t, got.Expression, got.Conditions, map[string]string{"x": row.x}); matched != row.want {
			t.Fatalf("membership intersection x=%s = %v, want %v", row.x, matched, row.want)
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
	requireSemanticAndLeaves(t, query, want, "x")
}

func TestSemanticResourceMetadataExcludedFromFallback(t *testing.T) {
	semantic := AnalyzeQuery(`search x=a | outputlookup results.csv`)
	if len(semantic.Errors) != 0 || len(semantic.Conditions) != 2 || semantic.Expression == nil ||
		semantic.Expression.Operator != BooleanCondition || semantic.Expression.Condition == nil ||
		semantic.Expression.Condition.Field != "x" {
		t.Fatalf("semantic single-filter resource projection = %#v", semantic)
	}
	resourceOnly := AnalyzeQuery(`| outputlookup results.csv`)
	if len(resourceOnly.Errors) != 0 || len(resourceOnly.Conditions) != 1 || resourceOnly.Expression != nil ||
		resourceOnly.Conditions[0].Field != "_spl.outputlookup" {
		t.Fatalf("semantic resource-only projection = %#v", resourceOnly)
	}
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

func TestAsymmetricOrGroupsKeepAtomicPredicates(t *testing.T) {
	for _, tc := range []struct {
		query   string
		want    *ConditionExpression
		matches func(a, x string) bool
	}{
		{`search x=u OR (x=v AND a=yes)`, branches(BooleanOr, leaf(0), branches(BooleanAnd, leaf(1), leaf(2))),
			func(a, x string) bool { return x == "u" || (x == "v" && a == "yes") }},
		{`search (a=yes AND x=u) OR x=v`, branches(BooleanOr, branches(BooleanAnd, leaf(0), leaf(1)), leaf(2)),
			func(a, x string) bool { return (a == "yes" && x == "u") || x == "v" }},
	} {
		got := ExtractConditions(tc.query)
		if len(got.Errors) != 0 || len(got.Conditions) != 3 || !reflect.DeepEqual(got.Expression, tc.want) {
			t.Fatalf("asymmetric %q = conditions %#v expression %#v errors %v, want %#v", tc.query, got.Conditions, got.Expression, got.Errors, tc.want)
		}
		for _, condition := range got.Conditions {
			if len(condition.Alternatives) != 0 {
				t.Fatalf("asymmetric %q merged %#v", tc.query, got.Conditions)
			}
		}
		for _, a := range []string{"yes", "no"} {
			for _, x := range []string{"u", "v", "other"} {
				if matched := projectionMatches(t, got.Expression, got.Conditions, map[string]string{"a": a, "x": x}); matched != tc.matches(a, x) {
					t.Fatalf("%q row a=%s x=%s = %v", tc.query, a, x, matched)
				}
			}
		}
	}
}

func TestProvenSiblingOrGrouping(t *testing.T) {
	for _, tc := range []struct {
		query string
		count int
		want  *ConditionExpression
	}{
		{`search x=u OR x=v`, 1, leaf(0)},
		{`search a=yes AND (x=u OR x=v)`, 2, branches(BooleanAnd, leaf(0), leaf(1))},
		{`search index=main AND (x=u OR x=v)`, 2, branches(BooleanAnd, leaf(0), leaf(1))},
	} {
		got := ExtractConditions(tc.query)
		if len(got.Errors) != 0 || len(got.Conditions) != tc.count || !reflect.DeepEqual(got.Expression, tc.want) {
			t.Fatalf("sibling %q = conditions %#v expression %#v errors %v, want %#v", tc.query, got.Conditions, got.Expression, got.Errors, tc.want)
		}
		last := got.Conditions[len(got.Conditions)-1]
		if !reflect.DeepEqual(last.Alternatives, []string{"u", "v"}) {
			t.Fatalf("sibling %q alternatives %#v", tc.query, last.Alternatives)
		}
	}
}

func TestCaseDistinctFieldsDoNotGroup(t *testing.T) {
	got := ExtractConditions(`search User=a OR user=b`)
	if len(got.Errors) != 0 || len(got.Conditions) != 2 || !reflect.DeepEqual(got.Expression, branches(BooleanOr, leaf(0), leaf(1))) ||
		got.Conditions[0].Field != "User" || got.Conditions[1].Field != "user" {
		t.Fatalf("case-distinct fields = conditions %#v expression %#v errors %v", got.Conditions, got.Expression, got.Errors)
	}
}

func TestUnsupportedSearchOperandBlocksAlternativeProjection(t *testing.T) {
	got := ExtractConditions("search x=u OR `unknown` OR x=v")
	if !got.ExpressionIncomplete || got.Expression != nil || len(got.Conditions) != 2 ||
		len(got.Conditions[0].Alternatives) != 0 || len(got.Conditions[1].Alternatives) != 0 {
		t.Fatalf("unsupported operand was crossed by grouping: %#v", got)
	}
}

func TestUnsupportedWhereAtomKeepsExpressionIncomplete(t *testing.T) {
	for _, tc := range []struct {
		query  string
		values []string
	}{
		{`| where x="u" OR (x="v" AND len(y)>2)`, []string{"u", "v"}},
		{`| where (x="u" AND len(y)>2) OR x="v"`, []string{"u", "v"}},
		{`| where x="u" AND len(y)>2`, []string{"u"}},
		{`| where NOT (x="u" OR len(y)>2)`, []string{"u"}},
	} {
		t.Run(tc.query, func(t *testing.T) {
			got := ExtractConditions(tc.query)
			if len(got.Errors) != 0 || !got.ExpressionIncomplete || got.Expression != nil || len(got.Warnings) == 0 ||
				len(got.Conditions) != len(tc.values) {
				t.Fatalf("incomplete where extraction = %#v", got)
			}
			for i, condition := range got.Conditions {
				if condition.Field != "x" || condition.Value != tc.values[i] || len(condition.Alternatives) != 0 {
					t.Fatalf("unsupported where atom merged or changed event predicates: %#v", got.Conditions)
				}
			}
			semantic := AnalyzeQuery(tc.query)
			if len(semantic.Errors) != 0 || semantic.Expression != nil || len(semantic.Warnings) == 0 ||
				!reflect.DeepEqual(semantic.Conditions, got.Conditions) {
				t.Fatalf("semantic incomplete where = %#v; extract %#v", semantic, got)
			}
		})
	}
	for _, query := range []string{`search *`, `search index=main | outputlookup results.csv`} {
		got := ExtractConditions(query)
		if len(got.Errors) != 0 || got.ExpressionIncomplete || len(got.Warnings) != 0 {
			t.Fatalf("intentional nonpredicate marked incomplete: %#v", got)
		}
	}
}

func TestUnrepresentedWhereBooleanAtomsRemainIncomplete(t *testing.T) {
	for _, tc := range []struct {
		query          string
		values         []string
		conditionCount int
	}{
		{`| where x="u" OR (x="v" AND false())`, []string{"u", "v"}, 2},
		{`| where (x="u" AND false()) OR x="v"`, []string{"u", "v"}, 2},
		{`| where NOT (x="u" OR false())`, []string{"u"}, 1},
		{`| where x="u" OR (x="v" AND mystery(y))`, []string{"u", "v"}, 2},
		{`| where x="u" OR (x="v" AND y)`, []string{"u", "v"}, 2},
		{`| where x="u" OR (x="v" AND isnull(y)=false)`, []string{"u", "v"}, 3},
		{`| where x="u" OR (x="v" AND isnull(y)=true)`, []string{"u", "v"}, 3},
		{`| where false()`, nil, 0},
	} {
		t.Run(tc.query, func(t *testing.T) {
			got := ExtractConditions(tc.query)
			if len(got.Errors) != 0 || !got.ExpressionIncomplete || got.Expression != nil || len(got.Warnings) == 0 {
				t.Fatalf("unrepresented atom has complete expression: %#v", got)
			}
			if len(got.Conditions) != tc.conditionCount {
				t.Fatalf("event predicates changed: %#v", got.Conditions)
			}
			for i, value := range tc.values {
				if got.Conditions[i].Field != "x" || got.Conditions[i].Value != value || len(got.Conditions[i].Alternatives) != 0 {
					t.Fatalf("event predicates changed: %#v", got.Conditions)
				}
			}
			semantic := AnalyzeQuery(tc.query)
			if len(semantic.Errors) != 0 || semantic.Expression != nil || len(semantic.Warnings) == 0 ||
				(len(tc.values) > 0 && !reflect.DeepEqual(semantic.Conditions, got.Conditions)) ||
				(len(tc.values) == 0 && len(semantic.Conditions) != 0) {
				t.Fatalf("semantic projection of incomplete expression: %#v", semantic)
			}
		})
	}
}

func TestRepresentedBareWhereFunctionControls(t *testing.T) {
	for _, query := range []string{
		`| where x="u" AND isnull(y)`,
		`| where x="u" AND isnotnull(y)`,
		`| where x="u" AND match(y, "abc")`,
	} {
		t.Run(query, func(t *testing.T) {
			got := ExtractConditions(query)
			if len(got.Errors) != 0 || got.ExpressionIncomplete || len(got.Warnings) != 0 ||
				len(got.Conditions) != 2 || !reflect.DeepEqual(got.Expression, branches(BooleanAnd, leaf(0), leaf(1))) {
				t.Fatalf("represented bare predicate rejected: %#v", got)
			}
		})
	}
}

func TestLargeProvenOrGroupsStayWithinNormalBudget(t *testing.T) {
	for _, count := range []int{950, 1000} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			var query strings.Builder
			query.WriteString("search ")
			for i := range count {
				if i > 0 {
					query.WriteString(" OR ")
				}
				query.WriteString("x=v")
				query.WriteString(strconv.Itoa(i))
			}
			got := ExtractConditions(query.String())
			if len(got.Errors) != 0 || got.ExpressionIncomplete || len(got.Conditions) != 1 ||
				len(got.Conditions[0].Alternatives) != count || !reflect.DeepEqual(got.Expression, leaf(0)) {
				t.Fatalf("%d-term extraction: conditions=%d errors=%v incomplete=%v expression=%#v", count, len(got.Conditions), got.Errors, got.ExpressionIncomplete, got.Expression)
			}
			for i, value := range got.Conditions[0].Alternatives {
				if value != "v"+strconv.Itoa(i) {
					t.Fatalf("%d-term alternatives[%d] = %q", count, i, value)
				}
			}
			semantic := AnalyzeQuery(query.String())
			if len(semantic.Errors) != 0 || len(semantic.Conditions) != 1 || semantic.Expression == nil ||
				semantic.Expression.Condition == nil || len(semantic.Expression.Condition.Alternatives) != count {
				t.Fatalf("%d-term semantic: %#v", count, semantic)
			}
		})
	}
}

func TestOrGroupingRequiresMatchingLiteralContext(t *testing.T) {
	base := Condition{Field: "x", Operator: "=", Value: "u", LogicalOp: "AND"}
	for _, tc := range []struct {
		name string
		next Condition
	}{
		{"case", Condition{Field: "X", Operator: "=", Value: "v"}},
		{"stage", Condition{Field: "x", Operator: "=", Value: "v", PipeStage: 1}},
		{"reference", Condition{Field: "x", Operator: "=", Value: "v", ValueReference: "other"}},
		{"computed", Condition{Field: "x", Operator: "=", Value: "v", IsComputed: true}},
		{"source", Condition{Field: "x", Operator: "=", Value: "v", SourceField: "raw"}},
		{"negated", Condition{Field: "x", Operator: "=", Value: "v", Negated: true}},
		{"operator", Condition{Field: "x", Operator: "!=", Value: "v"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conditions := []Condition{base, tc.next}
			got, indexMap := groupORConditionsByExpression(conditions, branches(BooleanOr, leaf(0), leaf(1)), nil)
			if !reflect.DeepEqual(got, conditions) || !reflect.DeepEqual(indexMap, []int{0, 1}) {
				t.Fatalf("grouped incompatible conditions: %#v map %v", got, indexMap)
			}
		})
	}
	if got, indexMap := groupORConditionsByExpression([]Condition{base, {Field: "x", Operator: "=", Value: "v"}}, nil, nil); len(got) != 2 || !reflect.DeepEqual(indexMap, []int{0, 1}) {
		t.Fatalf("missing proof grouped conditions: %#v map %v", got, indexMap)
	}
}

func TestPostprocessingOwnsAlternativeSlices(t *testing.T) {
	original := []Condition{
		{Field: "x", Operator: "=", Value: "a", Alternatives: []string{"a"}},
		{Field: "x", Operator: "=", Value: "b", Alternatives: []string{"b"}},
	}
	grouped, indexMap := groupORConditionsByExpression(original, branches(BooleanOr, leaf(0), leaf(1)), nil)
	if len(grouped) != 1 || !reflect.DeepEqual(indexMap, []int{0, 0}) {
		t.Fatalf("grouped = %#v map %v", grouped, indexMap)
	}
	grouped[0].Alternatives[0] = "changed"
	if original[0].Alternatives[0] != "a" {
		t.Fatalf("grouping aliased caller alternatives: %#v", original)
	}
	deduped, _ := deduplicateConditionsWithIndexMap(original, nil)
	deduped[0].Alternatives[0] = "changed"
	if original[0].Alternatives[0] != "a" {
		t.Fatalf("dedup aliased caller alternatives: %#v", original)
	}
}

func TestPostprocessingUsesSharedWorkBudget(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*parseBudget)
	}{
		{"dedup", func(b *parseBudget) {
			deduplicateConditionsWithIndexMap([]Condition{{Field: "x", Alternatives: []string{"a", "b"}}}, b)
		}},
		{"group", func(b *parseBudget) {
			groupORConditionsByExpression([]Condition{{Field: "x", Value: "a"}, {Field: "x", Value: "b"}}, branches(BooleanOr, leaf(0), leaf(1)), b)
		}},
		{"remap", func(b *parseBudget) { remapConditionExpression(branches(BooleanAnd, leaf(0), leaf(1)), []int{0, 1}, b) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			limits := defaultParseLimits()
			limits.maxWork = 1
			budget, cancel := newParseBudget(context.Background(), limits)
			defer cancel()
			defer func() {
				abort, ok := recover().(budgetAbort)
				if !ok || !strings.Contains(abort.err.Error(), "work budget") {
					t.Fatalf("postprocessing escaped shared work budget: %#v", abort)
				}
			}()
			tc.run(budget)
		})
	}
}
