package spl

import (
	"context"
	"strings"
	"testing"
)

func TestSPLNormalizationRemainingEdges(t *testing.T) {
	comment := stripSPLBacktickComments("search a=1 ```unterminated comment\n| where b=2")
	if strings.Contains(comment, "unterminated comment") {
		t.Fatalf("unterminated triple-backtick comment was preserved: %q", comment)
	}
	if !strings.Contains(comment, "\n| where b=2") {
		t.Fatalf("unterminated comment stripped following pipeline: %q", comment)
	}

	invalidUTF8 := string([]byte{'a', 0xff, 'b'})
	if got := normalizeSPLUnicodeWhitespace(invalidUTF8); got != invalidUTF8 {
		t.Fatalf("invalid utf-8 bytes changed: %q", got)
	}

	unicodeSpace := normalizeSPLUnicodeWhitespace("index=main\u00a0user=\"a\u00a0b\"\u200bhost=server")
	if unicodeSpace != "index=main user=\"a\u00a0b\"host=server" {
		t.Fatalf("unicode whitespace normalized incorrectly: %q", unicodeSpace)
	}

	segment, ok := portablePredicateSegment(`search user="alice"`, false)
	if !ok || segment != `user="alice"` {
		t.Fatalf("search segment = (%q, %v), want predicate", segment, ok)
	}
}

func TestSPLNormalizationPreservesMacroArguments(t *testing.T) {
	cases := []struct {
		name  string
		query string
	}{
		{"double equals", "| eval x=`my_macro(field==1)`"},
		{"bang function", "| eval x=`my_macro(!isnull(user))`"},
		{"adjacent wildcard predicates", "| eval x=`my_macro(field=* other=*)`"},
		{"inputlookup option", "| eval x=`my_macro(inputlookup append=1)`"},
		{"Unicode whitespace", "| eval x=`my_macro(field\u00a0value)`"},
		{"line continuation", "| eval x=`my_macro(field\\\nvalue)`"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := normalizeSPLQuery(tc.query); got != tc.query {
				t.Fatalf("normalization changed macro contents: got %q, want %q", got, tc.query)
			}
			budget, cancel := newParseBudget(context.Background(), defaultParseLimits())
			defer cancel()
			if got, _ := normalizeSPLQueryMapped(tc.query, budget); got != tc.query {
				t.Fatalf("mapped normalization changed macro contents: got %q, want %q", got, tc.query)
			}
		})
	}
}

func TestSPLMacroPipesDoNotCreatePipelineStages(t *testing.T) {
	query := "index=main `my_macro | where user=alice` | where status=200"
	for _, condition := range ExtractConditions(query).Conditions {
		if condition.Field == "user" {
			t.Fatalf("extracted condition from inside macro: %+v", condition)
		}
	}
}

func TestSPLMacroQuotesDoNotHideFollowingOperators(t *testing.T) {
	result := ExtractConditions("index=main `my_macro(\"unterminated` | where status==200")
	if len(result.Errors) != 0 {
		t.Fatalf("valid query produced errors: %v", result.Errors)
	}
	for _, condition := range result.Conditions {
		if condition.Field == "status" && condition.Operator == "==" && condition.Value == "200" {
			return
		}
	}
	t.Fatalf("following equality condition was not extracted: %+v", result.Conditions)
}
