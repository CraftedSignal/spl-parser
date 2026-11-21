package spl

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// The root rule requires a query to span the whole input. Tokens after the last
// stage the grammar accepts must not come back as a clean, complete result.
func TestUnparsedQueryTailIsReportedIncomplete(t *testing.T) {
	query := `index=x ) | where marker=1`

	r := ExtractConditions(query)
	if !r.ExpressionIncomplete || r.Expression != nil {
		t.Fatalf("unparsed tail reported as complete: incomplete=%v expression=%v errors=%q", r.ExpressionIncomplete, r.Expression, r.Errors)
	}
	if len(r.Warnings) == 0 || !strings.HasPrefix(r.Warnings[0], "boolean expression unavailable") {
		t.Fatalf("unparsed tail has no boolean-expression warning: %q", r.Warnings)
	}
	if r.Dataflow != nil {
		t.Fatalf("dataflow built from a truncated parse: %+v", r.Dataflow)
	}

	semantic := AnalyzeQuery(query)
	if !semantic.ExpressionIncomplete || semantic.Expression != nil {
		t.Fatalf("semantic view of an unparsed tail is complete: incomplete=%v expression=%v", semantic.ExpressionIncomplete, semantic.Expression)
	}
}

// Valid SPL whose later stage the grammar once dropped without a trace: the
// `where marker=1` stage must be extracted, or the result must be flagged.
func TestUnparsedQueryTailIsExtractedOrFlagged(t *testing.T) {
	for _, query := range []string{
		`| tstats count where index=* by index, sourcetype | where marker=1`,
		`index=x | top limit=10 user | where marker=1`,
		`index=x | chart count over user by action | where marker=1`,
		`index=x | bin span=5m _time | where marker=1`,
		`index=x | fields - _* | where marker=1`,
		`index=x | lookup dnslookup clientip AS src_ip OUTPUT clienthost | where marker=1`,
		`index=x | timechart span=1h count by host limit=10 | where marker=1`,
		`index=x | timechart span=1d dc(user) as u, count as c | where marker=1`,
		`index=x | stats count by _time span=1h | where marker=1`,
		`index=x | head limit=10 | where marker=1`,
		`index=x | sort - count desc | where marker=1`,
		`index=x format=json | where marker=1`,
		`index=x tag::host=web | where marker=1`,
	} {
		r := ExtractConditions(query)
		if !hasCondition(r.Conditions, "marker", "=", "1") && !r.ExpressionIncomplete && len(r.Errors) == 0 {
			t.Errorf("%s: the where stage vanished and nothing reports it: %s", query, conditionSummary(r.Conditions))
		}
	}
}

// ClassifyPipelineStages documents "Returns nil if parsing fails". A stage
// list cut short at an unparsed token hides the stages after it, such as a
// join that FirstJoinOrSubsearchStage must find.
func TestClassifyPipelineStagesUnparsedTail(t *testing.T) {
	query := `index=a ) | join user [search index=b]`
	if stages := ClassifyPipelineStages(query); stages != nil {
		t.Fatalf("ClassifyPipelineStages returned %d stage(s) for a query it cannot parse, want nil", len(stages))
	}
	if stages, err := ClassifyPipelineStagesContext(context.Background(), query); stages != nil || err != nil {
		t.Fatalf("ClassifyPipelineStagesContext = %v, %v, want nil stages and no error", stages, err)
	}

	query = `| tstats count where index=* by index, sourcetype | join index [search index=y]`
	stages := ClassifyPipelineStages(query)
	if stages != nil && len(stages) != 2 {
		t.Fatalf("ClassifyPipelineStages returned %d stage(s), want 2 or nil", len(stages))
	}
	if stages != nil && FirstJoinOrSubsearchStage(query) != 1 {
		t.Fatalf("FirstJoinOrSubsearchStage = %d, want 1", FirstJoinOrSubsearchStage(query))
	}
}

// A wildcard value ends at whitespace: `user=*` must not take the next term
// as the rest of its value.
func TestWildcardValueEndsAtWhitespace(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []ExpectedCondition
	}{
		{`index=x user=* host=h`, []ExpectedCondition{{"index", "=", "x"}, {"user", "=", "*"}, {"host", "=", "h"}}},
		{`fail* user=bob`, []ExpectedCondition{{"_raw", "contains", "fail*"}, {"user", "=", "bob"}}},
	} {
		r := ExtractConditions(tc.query)
		if len(r.Errors) != 0 || r.ExpressionIncomplete || len(r.Conditions) != len(tc.want) {
			t.Errorf("%s: errors=%q incomplete=%v conditions=%s", tc.query, r.Errors, r.ExpressionIncomplete, conditionSummary(r.Conditions))
			continue
		}
		for _, want := range tc.want {
			assertHasCondition(t, r.Conditions, want)
		}
	}
}

func hasCondition(conditions []Condition, field, operator, value string) bool {
	for _, condition := range conditions {
		if condition.Field == field && condition.Operator == operator && condition.Value == value {
			return true
		}
	}
	return false
}

// An unquoted search value runs to the next whitespace, hyphens and other
// punctuation included. The parts after the punctuation must not be dropped
// together with the rest of the search.
func TestBareValueKeepsPunctuation(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []ExpectedCondition
	}{
		{`index=os-linux sourcetype=linux-secure action=failure`, []ExpectedCondition{{"index", "=", "os-linux"}, {"sourcetype", "=", "linux-secure"}, {"action", "=", "failure"}}},
		{`index=x host=web-01 user=bob`, []ExpectedCondition{{"index", "=", "x"}, {"host", "=", "web-01"}, {"user", "=", "bob"}}},
		{`index=x sourcetype=cisco-asa action=blocked`, []ExpectedCondition{{"index", "=", "x"}, {"sourcetype", "=", "cisco-asa"}, {"action", "=", "blocked"}}},
		{`index=x user=john.doe@example.com action=x`, []ExpectedCondition{{"index", "=", "x"}, {"user", "=", "john.doe@example.com"}, {"action", "=", "x"}}},
		{`index=x user=admin$ action=x`, []ExpectedCondition{{"index", "=", "x"}, {"user", "=", "admin$"}, {"action", "=", "x"}}},
		{`index=x host=prod-* status=500`, []ExpectedCondition{{"index", "=", "x"}, {"host", "=", "prod-*"}, {"status", "=", "500"}}},
	} {
		r := ExtractConditions(tc.query)
		if len(r.Errors) != 0 || r.ExpressionIncomplete || len(r.Conditions) != len(tc.want) {
			t.Errorf("%s: errors=%q incomplete=%v conditions=%s", tc.query, r.Errors, r.ExpressionIncomplete, conditionSummary(r.Conditions))
			continue
		}
		for _, want := range tc.want {
			assertHasCondition(t, r.Conditions, want)
		}
	}
}

// An unquoted IPv4 address or CIDR block is one value. Splitting it at the
// second dot invents a _raw term and, with a CIDR suffix, drops the rest.
func TestUnquotedIPv4AndCIDRValues(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []ExpectedCondition
	}{
		{`dest_ip=10.0.0.1`, []ExpectedCondition{{"dest_ip", "=", "10.0.0.1"}}},
		{`index=main src_ip=10.0.0.0/8 action=allowed`, []ExpectedCondition{{"index", "=", "main"}, {"src_ip", "=", "10.0.0.0/8"}, {"action", "=", "allowed"}}},
		{`index=main 10.1.2.3 action=allowed`, []ExpectedCondition{{"index", "=", "main"}, {"_raw", "contains", "10.1.2.3"}, {"action", "=", "allowed"}}},
		{`index=main src_ip IN (10.0.0.1, 192.168.0.0/16)`, []ExpectedCondition{{"index", "=", "main"}, {"src_ip", "in", "10.0.0.1"}}},
	} {
		r := ExtractConditions(tc.query)
		if len(r.Errors) != 0 || r.ExpressionIncomplete || len(r.Conditions) != len(tc.want) {
			t.Errorf("%s: errors=%q incomplete=%v conditions=%s", tc.query, r.Errors, r.ExpressionIncomplete, conditionSummary(r.Conditions))
			continue
		}
		for _, want := range tc.want {
			assertHasCondition(t, r.Conditions, want)
		}
		for _, condition := range r.Conditions {
			if condition.Field == "src_ip" && condition.Operator == "in" && !reflect.DeepEqual(condition.Alternatives, []string{"10.0.0.1", "192.168.0.0/16"}) {
				t.Errorf("%s: IN values = %q", tc.query, condition.Alternatives)
			}
		}
	}
}
