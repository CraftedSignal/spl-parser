package spl

import (
	"context"
	"reflect"
	"testing"

	"github.com/antlr4-go/antlr/v4"
)

func TestWhereBuilderRejectsUnrepresentedWholeShapes(t *testing.T) {
	fragments := []string{
		`(x="u")=false`, `(isnull(y))=false`, `((isnull(y)))=true`,
		`isnull(y)=false`, `len(y)>2`, `mystery(y)`, `false()`, `true()`,
		`-(x="u")`, `-((isnull(y)))`,
		`(x="u")+1`, `1+(x="u")`, `(x="u")-1`, `(x="u")."a"`,
		`(x="u")*2`, `2/(x="u")`, `(x="u")%2`,
		`NOT ((x="u")=false)`, `NOT (x="u" OR false())`,
		`y`, `1`, `"literal"`, `x IN [search y=z]`,
		`isnull(lower(y))`, `match(y,otherField)`, `isnull("literal")`,
		`match(y)`, `isnull(y,z)`,
	}
	for _, fragment := range fragments {
		t.Run(fragment, func(t *testing.T) {
			query := `| where ` + fragment
			assertWholeSPLQueryConsumed(t, query)
			got := ExtractConditions(query)
			if len(got.Errors) != 0 || !got.ExpressionIncomplete || got.Expression != nil || len(got.Warnings) == 0 {
				t.Fatalf("whole unsupported shape: %#v", got)
			}
			semantic := AnalyzeQuery(query)
			if len(semantic.Errors) != 0 || semantic.Expression != nil || len(semantic.Warnings) == 0 ||
				len(semantic.Conditions) != len(got.Conditions) {
				t.Fatalf("semantic unsupported shape: %#v", semantic)
			}
		})
	}
}

func TestWhereBuilderRequiresExactListenerSourceBinding(t *testing.T) {
	tree := splParserForTest(t, `search x=u AND y=v`).Query()
	var atoms []antlr.Tree
	var visit func(antlr.Tree)
	visit = func(node antlr.Tree) {
		if node == nil {
			return
		}
		if condition, ok := node.(IConditionContext); ok {
			atoms = append(atoms, condition)
		}
		for i := 0; i < node.GetChildCount(); i++ {
			visit(node.GetChild(i))
		}
	}
	visit(tree)
	if len(atoms) != 2 {
		t.Fatalf("expected two source conditions, got %d", len(atoms))
	}
	conditions := []Condition{{Field: "x", Operator: "=", Value: "u"}, {Field: "y", Operator: "=", Value: "v"}}
	for _, tc := range []struct {
		name    string
		sources conditionSourceBindings
		valid   bool
	}{
		{"exact", conditionSourceBindings{atoms[0]: {first: 0, count: 1}, atoms[1]: {first: 1, count: 1}}, true},
		{"missing", conditionSourceBindings{atoms[0]: {first: 0, count: 1}}, false},
		{"swapped", conditionSourceBindings{atoms[0]: {first: 1, count: 1}, atoms[1]: {first: 0, count: 1}}, false},
		{"wrong cardinality", conditionSourceBindings{atoms[0]: {first: 0, count: 2}, atoms[1]: {first: 1, count: 1}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			expression, incomplete := buildConditionExpressionWithCompleteness(tree, conditions, nil, tc.sources)
			if tc.valid {
				if incomplete || !reflect.DeepEqual(expression, branches(BooleanAnd, leaf(0), leaf(1))) {
					t.Fatalf("valid binding: expression %#v incomplete %v", expression, incomplete)
				}
			} else if !incomplete || expression != nil {
				t.Fatalf("invalid binding: expression %#v incomplete %v", expression, incomplete)
			}
		})
	}
}

func TestWhereClassifierSharesWorkBudget(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("tiny work budget did not reject classifier traversal")
		} else if _, ok := recovered.(budgetAbort); !ok {
			t.Fatalf("unexpected panic: %v", recovered)
		}
	}()
	budget := &parseBudget{ctx: context.Background(), limits: parseLimits{maxWork: 1}}
	classifyComparison(splWhereExpressionForTest(t, `(x=u)`).OrExpression().AndExpression(0).NotExpression(0).ComparisonExpression(), budget)
}

func TestWhereClassifierHonorsCanceledBudget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("canceled budget did not stop classifier traversal")
		} else if _, ok := recovered.(budgetAbort); !ok {
			t.Fatalf("unexpected panic: %v", recovered)
		}
	}()
	budget := &parseBudget{ctx: ctx, limits: defaultParseLimits()}
	classifyComparison(splWhereExpressionForTest(t, `(x=u)`).OrExpression().AndExpression(0).NotExpression(0).ComparisonExpression(), budget)
}

func TestWhereBuilderKeepsTransparentAndBareAtoms(t *testing.T) {
	fragments := []string{
		`x="u"`, `x=y`, `x IN ("u","v")`, `(x="u")`, `((x="u"))`,
		`isnull(y)`, `((isnull(y)))`, `isnotnull(y)`,
		`match(y,"abc")`, `like(y,"a%")`, `cidrmatch("10.0.0.0/8",src)`,
		`NOT ((x="u"))`, `NOT NOT ((isnull(y)))`,
	}
	for _, fragment := range fragments {
		t.Run(fragment, func(t *testing.T) {
			query := `| where z=1 AND ` + fragment
			assertWholeSPLQueryConsumed(t, query)
			got := ExtractConditions(query)
			if len(got.Errors) != 0 || got.ExpressionIncomplete || len(got.Warnings) != 0 ||
				len(got.Conditions) != 2 || !reflect.DeepEqual(got.Expression, branches(BooleanAnd, leaf(0), leaf(1))) {
				t.Fatalf("represented atom: %#v", got)
			}
			switch fragment {
			case `x=y`:
				if got.Conditions[1].ValueReference != "y" || got.Conditions[1].Value != "" {
					t.Fatalf("field reference changed: %#v", got.Conditions[1])
				}
			case `x IN ("u","v")`:
				if !reflect.DeepEqual(got.Conditions[1].Alternatives, []string{"u", "v"}) {
					t.Fatalf("IN values changed: %#v", got.Conditions[1])
				}
			}
			semantic := AnalyzeQuery(query)
			if len(semantic.Errors) != 0 || semantic.Expression == nil || len(semantic.Conditions) != 2 {
				t.Fatalf("semantic represented atom: %#v", semantic)
			}
		})
	}
}

func TestWhereBuilderIncompleteBlocksSiblingAlternatives(t *testing.T) {
	for _, fragment := range []string{`(isnull(y))=false`, `(x="w")=false`, `false()`, `isnull(lower(y))`} {
		for _, query := range []string{
			`| where x="u" OR (x="v" AND ` + fragment + `)`,
			`| where (x="u" AND ` + fragment + `) OR x="v"`,
		} {
			t.Run(query, func(t *testing.T) {
				assertWholeSPLQueryConsumed(t, query)
				got := ExtractConditions(query)
				if len(got.Errors) != 0 || !got.ExpressionIncomplete || got.Expression != nil || len(got.Warnings) == 0 {
					t.Fatalf("unsupported sibling merged: %#v", got)
				}
				var xValues []string
				for _, condition := range got.Conditions {
					if len(condition.Alternatives) != 0 {
						t.Fatalf("unsupported sibling gained alternatives: %#v", got.Conditions)
					}
					if condition.Field == "x" && (condition.Value == "u" || condition.Value == "v") {
						xValues = append(xValues, condition.Value)
					}
				}
				if !reflect.DeepEqual(xValues, []string{"u", "v"}) {
					t.Fatalf("original x conditions changed: %#v", got.Conditions)
				}
				semantic := AnalyzeQuery(query)
				if len(semantic.Errors) != 0 || semantic.Expression != nil || len(semantic.Warnings) == 0 {
					t.Fatalf("semantic unsupported sibling: %#v", semantic)
				}
			})
		}
	}
}

func assertWholeSPLQueryConsumed(t *testing.T, query string) {
	t.Helper()
	parsed := splParserForTest(t, query).Query()
	if parsed.GetStop() == nil {
		t.Fatalf("parser did not consume %q", query)
	}
	if parsed.GetStop().GetStop() != len(query)-1 {
		t.Fatalf("parser consumed through byte %d of %d for %q", parsed.GetStop().GetStop(), len(query), query)
	}
}
