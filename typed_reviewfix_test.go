package spl

import (
	"strings"
	"testing"
)

func reviewScalar(node *ConditionExpression, source string, negated bool) bool {
	if node == nil {
		return false
	}
	if node.Scalar != nil && node.Scalar.Expression == source && node.Scalar.Negated == negated {
		return true
	}
	for _, child := range node.Children {
		if reviewScalar(child, source, negated) {
			return true
		}
	}
	return false
}

func TestReviewResourceKeepsScalarExpression(t *testing.T) {
	for _, tc := range []struct {
		query    string
		resource string
		negated  bool
	}{
		{`| inputlookup in.csv | where 5<10`, "_spl.inputlookup", false},
		{`| where 5<10 | outputlookup out.csv`, "_spl.outputlookup", false},
		{`| inputlookup in.csv | where NOT 5<10`, "_spl.inputlookup", true},
	} {
		r := ExtractConditions(tc.query)
		semantic := AnalyzeQuery(tc.query)
		if len(r.Errors) != 0 || len(r.Warnings) != 0 || r.ExpressionIncomplete ||
			len(r.Conditions) != 1 || r.Conditions[0].Field != tc.resource ||
			!reviewScalar(r.Expression, "5<10", tc.negated) || semantic.Expression == nil {
			t.Errorf("resource scalar %q: %#v", tc.query, r)
		}
	}
}

func TestReviewTypedStringSinglePassEscapes(t *testing.T) {
	for _, tc := range []struct{ query, source, value string }{
		{`| where x="C:\\windows\temp"`, `"C:\\windows\temp"`, "C:\\windows\temp"},
		{`| where x="a\\\"b"`, `"a\\\"b"`, "a\\\"b"},
		{`| where x="a\"b"`, `"a\"b"`, "a\"b"},
		{`| where x="line\nnext"`, `"line\nnext"`, "line\nnext"},
	} {
		r := AnalyzeQuery(tc.query)
		if len(r.Errors) != 0 || len(r.Warnings) != 0 || len(r.Conditions) != 1 ||
			r.Conditions[0].RHS == nil || r.Conditions[0].RHS.Source != tc.source ||
			r.Conditions[0].RHS.Value != tc.value {
			t.Errorf("typed escape %q: %#v", tc.query, r)
		}
		if len(r.Conditions) == 1 && r.Conditions[0].Value != trimMatchingQuotes(tc.source) {
			t.Errorf("legacy quoted value changed for %q: %q", tc.query, r.Conditions[0].Value)
		}
	}
	list := AnalyzeQuery(`| where x IN ("C:\\windows\temp", "a\\\"b")`)
	if len(list.Errors) != 0 || len(list.Warnings) != 0 || len(list.Conditions) != 1 || list.Conditions[0].RHS == nil ||
		len(list.Conditions[0].RHS.Items) != 2 || list.Conditions[0].RHS.Items[0].Value != "C:\\windows\temp" ||
		list.Conditions[0].RHS.Items[1].Value != "a\\\"b" || list.Conditions[0].Alternatives[0] != `C:\\windows\temp` {
		t.Errorf("typed/legacy IN escapes: %#v", list)
	}
	function := AnalyzeQuery(`| where match(x,"C:\\windows\temp")`)
	if len(function.Errors) != 0 || len(function.Warnings) != 0 || len(function.Conditions) != 1 ||
		function.Conditions[0].RHS == nil || function.Conditions[0].RHS.Value != "C:\\windows\temp" ||
		function.Conditions[0].Value != `C:\\windows\temp` {
		t.Errorf("typed/legacy function escapes: %#v", function)
	}
}

func TestReviewUnresolvedSubsearchCannotBecomeScalar(t *testing.T) {
	r := ExtractConditions(`| where len([search x=a])>1`)
	if !r.ExpressionIncomplete || len(r.Warnings) == 0 || r.Expression != nil {
		t.Fatalf("unresolved subsearch was treated as complete: %#v", r)
	}
}

func TestReviewEvalWildcardsDoNotBecomeBareConditions(t *testing.T) {
	for _, query := range []string{`| where x=*`, `| where x=foo*`} {
		r := ExtractConditions(query)
		if !r.ExpressionIncomplete || len(r.Warnings) == 0 {
			t.Errorf("unsupported eval wildcard lacks diagnostic: %q %#v", query, r)
		}
		for _, condition := range r.Conditions {
			if condition.Context == PredicateEval && condition.RHS != nil && condition.RHS.Kind == OperandBare &&
				!r.ExpressionIncomplete && len(r.Warnings) == 0 {
				t.Errorf("unsupported eval wildcard claimed ordinary predicate: %q %#v", query, r)
			}
		}
	}
	for _, query := range []string{`search x=*`, `search x=foo*`} {
		r := AnalyzeQuery(query)
		if len(r.Errors) != 0 || len(r.Warnings) != 0 || len(r.Conditions) != 1 ||
			r.Conditions[0].Context != PredicateSearch || r.Conditions[0].RHS == nil ||
			r.Conditions[0].RHS.Kind != OperandBare {
			t.Errorf("search wildcard role %q: %#v", query, r)
		}
	}
}

func TestReviewUnresolvedINItemsAreWholeScalars(t *testing.T) {
	for _, name := range []string{"true", "false", "null"} {
		atom := `x IN (1,` + name + `,"true")`
		r := ExtractConditions(`| where ` + atom)
		if len(r.Errors) != 0 || len(r.Warnings) != 0 || r.ExpressionIncomplete ||
			len(r.Conditions) != 0 || !reviewScalar(r.Expression, atom, false) {
			t.Errorf("unresolved IN %q: %#v", atom, r)
		}
	}
	r := AnalyzeQuery(`| where x IN (y,'z',"z",42)`)
	if len(r.Errors) != 0 || len(r.Warnings) != 0 || len(r.Conditions) != 1 || r.Conditions[0].RHS == nil ||
		r.Conditions[0].RHS.Kind != OperandList || len(r.Conditions[0].RHS.Items) != 4 {
		t.Errorf("ordinary mixed IN: %#v", r)
	}
}

func TestReviewSearchBareOperandSourceCoversWildcard(t *testing.T) {
	for _, term := range []string{`foo*`, `*foo`, `"foo*"`} {
		r := AnalyzeQuery(`search ` + term)
		if len(r.Errors) != 0 || len(r.Warnings) != 0 || len(r.Conditions) != 1 ||
			r.Conditions[0].RHS == nil || r.Conditions[0].RHS.Source != term ||
			!strings.Contains(r.Conditions[0].Value, "foo") {
			t.Errorf("search term %q: %#v", term, r)
		}
	}
}
