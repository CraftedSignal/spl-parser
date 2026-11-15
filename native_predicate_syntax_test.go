package spl

import (
	"context"
	"testing"
)

func TestTruncatedNativeWherePreservesSyntaxFailure(t *testing.T) {
	for _, query := range []string{
		`| where x >`,
		`index=main | where x >`,
		`index=main | where 5<10 | where x >`,
		`| where (`,
		`index=main | where (`,
		`index=main | where (x >)`,
		"index=main | where x ``` explanatory comment ``` >   ",
		`| where x > | outputlookup result`,
		`index=main | join user [search index=child | where x >]`,
		`index=main | append [search index=child | where x >]`,
	} {
		t.Run(query, func(t *testing.T) {
			for name, parse := range map[string]func() *ParseResult{
				"direct":  func() *ParseResult { return ExtractConditions(query) },
				"context": func() *ParseResult { return ExtractConditionsContext(context.Background(), query) },
			} {
				r := parse()
				if len(r.Errors) == 0 || r.Expression != nil || !r.ExpressionIncomplete || len(r.Conditions) != 0 ||
					len(r.Commands) != 0 || len(r.Joins) != 0 || len(r.Subsearches) != 0 {
					t.Errorf("%s extraction accepted malformed where: %#v", name, r)
				}
			}
			for name, analyze := range map[string]func() *SemanticQuery{
				"direct":  func() *SemanticQuery { return AnalyzeQuery(query) },
				"context": func() *SemanticQuery { return AnalyzeQueryContext(context.Background(), query) },
			} {
				r := analyze()
				if len(r.Errors) == 0 || r.Expression != nil || len(r.Conditions) != 0 ||
					len(r.Commands) != 0 || len(r.Joins) != 0 || len(r.Subsearches) != 0 {
					t.Errorf("%s semantic accepted malformed where: %#v", name, r)
				}
			}
		})
	}
}

func TestCompleteNativeWhereKeepsTypedAndScalarFilters(t *testing.T) {
	for _, tc := range []struct {
		query, scalar string
		numeric       bool
		negated       bool
	}{
		{query: `| where x>42`, numeric: true},
		{query: `index=main | where x>42`, numeric: true},
		{query: `| where x+1>42`, scalar: `x+1>42`},
		{query: `index=main | where x+1>42`, scalar: `x+1>42`},
		{query: `| where 5<10`, scalar: `5<10`},
		{query: `| where (x+1>42)`, scalar: `x+1>42`},
		{query: `| where NOT (x+1>42)`, scalar: `x+1>42`, negated: true},
		{query: `| where x>42 | outputlookup result`, numeric: true},
	} {
		t.Run(tc.query, func(t *testing.T) {
			for name, parse := range map[string]func() *ParseResult{
				"direct":  func() *ParseResult { return ExtractConditions(tc.query) },
				"context": func() *ParseResult { return ExtractConditionsContext(context.Background(), tc.query) },
			} {
				r := parse()
				if len(r.Errors) != 0 || len(r.Warnings) != 0 || r.ExpressionIncomplete ||
					(tc.scalar != "" && r.Expression == nil) {
					t.Errorf("%s valid extraction: %#v", name, r)
					continue
				}
				if tc.scalar != "" && !reviewScalar(r.Expression, tc.scalar, tc.negated) {
					t.Errorf("%s scalar source: %#v", name, r.Expression)
				}
				if tc.numeric && !hasNumericWhereCondition(r.Conditions) {
					t.Errorf("%s typed comparison: %#v", name, r.Conditions)
				}
			}
			for name, analyze := range map[string]func() *SemanticQuery{
				"direct":  func() *SemanticQuery { return AnalyzeQuery(tc.query) },
				"context": func() *SemanticQuery { return AnalyzeQueryContext(context.Background(), tc.query) },
			} {
				r := analyze()
				if len(r.Errors) != 0 || len(r.Warnings) != 0 || r.Expression == nil ||
					(tc.numeric && !hasNumericWhereCondition(r.Conditions)) {
					t.Errorf("%s valid semantic: %#v", name, r)
				}
			}
		})
	}
	for _, tc := range []struct {
		query    string
		computed bool
	}{
		{query: `index=main | join user [search index=child | where x>42]`},
		{query: `index=main | append [search index=child | where x>42]`},
		{query: `index=main | join user [search index=child | where x+1>42]`, computed: true},
		{query: `index=main | append [search index=child | where x+1>42]`, computed: true},
	} {
		t.Run(tc.query, func(t *testing.T) {
			outer := ExtractConditions(tc.query)
			semantic := AnalyzeQueryContext(context.Background(), tc.query)
			if len(outer.Errors) != 0 || outer.ExpressionIncomplete || len(outer.Conditions) != 1 ||
				len(outer.Joins) != 1 || outer.Joins[0].Subsearch == nil ||
				len(outer.Joins[0].Subsearch.Errors) != 0 ||
				len(semantic.Errors) != 0 || semantic.Expression == nil || len(semantic.Joins) != 1 ||
				semantic.Joins[0].Subquery == nil || semantic.Joins[0].Subquery.Expression == nil {
				t.Errorf("valid nested where: outer=%#v semantic=%#v", outer, semantic)
				return
			}
			child := outer.Joins[0].Subsearch
			semanticChild := semantic.Joins[0].Subquery
			if outer.Conditions[0].Field != "index" || outer.Conditions[0].Value != "main" ||
				len(child.Conditions) == 0 || child.Conditions[0].Field != "index" || child.Conditions[0].Value != "child" {
				t.Errorf("nested search ownership: outer=%#v child=%#v", outer.Conditions, child.Conditions)
			}
			if len(child.Conditions) == 0 || len(semanticChild.Conditions) == 0 {
				return
			}
			if hasNumericWhereCondition(child.Conditions) != !tc.computed || hasNumericWhereCondition(semanticChild.Conditions) != !tc.computed {
				t.Errorf("nested typed comparison differs: child=%#v semantic=%#v", child.Conditions, semanticChild.Conditions)
			}
			if reviewScalar(child.Expression, "x+1>42", false) != tc.computed ||
				semanticScalarAtStage(semanticChild.Expression, "x+1>42", 1) != tc.computed {
				t.Errorf("nested scalar differs: child=%#v semantic=%#v", child.Expression, semanticChild.Expression)
			}
		})
	}
}

func semanticScalarAtStage(expression *SemanticExpression, source string, stage int) bool {
	if expression == nil {
		return false
	}
	if expression.Scalar != nil && expression.Scalar.Expression == source && expression.Scalar.PipeStage == stage {
		return true
	}
	for _, child := range expression.Children {
		if semanticScalarAtStage(child, source, stage) {
			return true
		}
	}
	return false
}

func hasNumericWhereCondition(conditions []Condition) bool {
	for _, condition := range conditions {
		if condition.Field == "x" && condition.Context == PredicateEval && condition.Operator == ">" &&
			condition.RHS != nil && condition.RHS.Kind == OperandNumber && condition.RHS.Value == "42" {
			return true
		}
	}
	return false
}
