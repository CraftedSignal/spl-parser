package spl

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/antlr4-go/antlr/v4"
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
		if errs := syntaxErrors(tc.query); len(errs) != 0 {
			t.Errorf("%s: syntax errors %q", tc.query, errs)
		}
		if len(r.Errors) != 0 || r.ExpressionIncomplete || len(r.Conditions) != len(tc.want) {
			t.Errorf("%s: errors=%q incomplete=%v conditions=%s", tc.query, r.Errors, r.ExpressionIncomplete, conditionSummary(r.Conditions))
			continue
		}
		for _, want := range tc.want {
			assertHasCondition(t, r.Conditions, want)
		}
	}
}

// syntaxErrors parses query as ExtractConditions does and returns the
// grammar's syntax errors, which the regex fallback would otherwise hide.
func syntaxErrors(query string) []string {
	lexer := NewSPLLexer(antlr.NewInputStream(normalizeSPLQuery(query)))
	lexer.RemoveErrorListeners()
	listener := &errorListener{}
	lexer.AddErrorListener(listener)
	parser := NewSPLParser(antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel))
	parser.RemoveErrorListeners()
	parser.AddErrorListener(listener)
	parser.Root()
	return listener.errors
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
		if errs := syntaxErrors(tc.query); len(errs) != 0 {
			t.Errorf("%s: syntax errors %q", tc.query, errs)
		}
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
		if errs := syntaxErrors(tc.query); len(errs) != 0 {
			t.Errorf("%s: syntax errors %q", tc.query, errs)
		}
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

// A command or function name is a plain word as a value or a field name
// (index=_audit action=search is Splunk's own audit idiom). It must not send
// the query off the native parser.
func TestCommandNameAsValueOrFieldName(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []ExpectedCondition
	}{
		{`index=_audit action=search "failed login" user=bob`, []ExpectedCondition{{"index", "=", "_audit"}, {"action", "=", "search"}, {"_raw", "contains", "failed login"}, {"user", "=", "bob"}}},
		{`index=_audit action=search | where like(user, "adm%")`, []ExpectedCondition{{"index", "=", "_audit"}, {"action", "=", "search"}, {"user", "like", "adm*"}}},
		{`index=_audit action=search | stats count by user | where count>5`, []ExpectedCondition{{"index", "=", "_audit"}, {"action", "=", "search"}, {"count", ">", "5"}}},
		{`index=x type=top mode=head format=json`, []ExpectedCondition{{"index", "=", "x"}, {"type", "=", "top"}, {"mode", "=", "head"}, {"format", "=", "json"}}},
	} {
		r := ExtractConditions(tc.query)
		if errs := syntaxErrors(tc.query); len(errs) != 0 {
			t.Errorf("%s: syntax errors %q", tc.query, errs)
		}
		if len(r.Errors) != 0 || r.ExpressionIncomplete || len(r.Conditions) != len(tc.want) {
			t.Errorf("%s: errors=%q incomplete=%v conditions=%s", tc.query, r.Errors, r.ExpressionIncomplete, conditionSummary(r.Conditions))
			continue
		}
		for _, want := range tc.want {
			assertHasCondition(t, r.Conditions, want)
		}
	}

	r := ExtractConditions(`index=_audit action=search NOT (info=granted OR info=completed)`)
	if len(r.Errors) != 0 || r.ExpressionIncomplete || r.Expression == nil || len(r.Conditions) != 4 {
		t.Errorf("NOT (.. OR ..) lost its grouping: errors=%q incomplete=%v expression=%v conditions=%s", r.Errors, r.ExpressionIncomplete, r.Expression, conditionSummary(r.Conditions))
	}
	requireSPLCondition(t, r.Conditions, "info", "=", "granted", true)
	requireSPLCondition(t, r.Conditions, "info", "=", "completed", true)
	r = ExtractConditions(`index=_audit action=search | join user [search index=b status=failed]`)
	if len(r.Errors) != 0 || r.ExpressionIncomplete || len(r.Joins) != 1 {
		t.Errorf("join query: errors=%q incomplete=%v joins=%d", r.Errors, r.ExpressionIncomplete, len(r.Joins))
	}
	assertNoCondition(t, r.Conditions, "status")

	query := `| rest /servicesNS/-/-/saved/searches splunk_server=local | table title search`
	if r := ExtractConditions(query); len(r.Errors) != 0 || r.ExpressionIncomplete {
		t.Errorf("%s: errors=%q incomplete=%v", query, r.Errors, r.ExpressionIncomplete)
	}
	if stages := ClassifyPipelineStages(query); len(stages) != 2 || stages[1].CommandType != "table" {
		t.Errorf("%s: stages=%+v", query, stages)
	}
}

// Splunk requires quotation marks around an operator or keyword searched as a
// value (country=IN, country=AS, iso=AND, state=OR), so the grammar keeps
// rejecting the unquoted forms and accepts the quoted ones.
func TestOperatorKeywordValueNeedsQuotes(t *testing.T) {
	for query, wantErrors := range map[string]bool{
		`index=x country=IN`: true, `index=x country=AS`: true, `index=x state=OR`: true, `index=x a=by`: true,
		`index=x country="IN"`: false, `index=x state="OR"`: false,
	} {
		if errs := syntaxErrors(query); (len(errs) > 0) != wantErrors {
			t.Errorf("%s: syntax errors %q, want errors=%v", query, errs, wantErrors)
		}
	}
}

// An unquoted value that starts with a digit (a file name, a hash, a hex
// code) is one value, not a number or time span plus an invented _raw term.
func TestDigitLeadingBareValue(t *testing.T) {
	for _, tc := range []struct {
		query string
		want  []ExpectedCondition
	}{
		{`index=x process_name=7z.exe`, []ExpectedCondition{{"index", "=", "x"}, {"process_name", "=", "7z.exe"}}},
		{`index=x hash=5d41402abc4b2a76b9719d911017c592`, []ExpectedCondition{{"index", "=", "x"}, {"hash", "=", "5d41402abc4b2a76b9719d911017c592"}}},
		{`index=x id=0x1F status=ok`, []ExpectedCondition{{"index", "=", "x"}, {"id", "=", "0x1F"}, {"status", "=", "ok"}}},
		{`index=x 7z.exe status=ok earliest=-5m`, []ExpectedCondition{{"index", "=", "x"}, {"_raw", "contains", "7z.exe"}, {"status", "=", "ok"}}},
	} {
		r := ExtractConditions(tc.query)
		if errs := syntaxErrors(tc.query); len(errs) != 0 {
			t.Errorf("%s: syntax errors %q", tc.query, errs)
		}
		if len(r.Errors) != 0 || r.ExpressionIncomplete || len(r.Conditions) != len(tc.want) {
			t.Errorf("%s: errors=%q incomplete=%v conditions=%s", tc.query, r.Errors, r.ExpressionIncomplete, conditionSummary(r.Conditions))
			continue
		}
		for _, want := range tc.want {
			assertHasCondition(t, r.Conditions, want)
		}
	}
}

// Relative time modifiers take every unit Splunk lists (min, hours, days, mon,
// sec ...) and weekday snaps such as @w1. A longer unit must not leave its
// tail behind as a _raw term or stop the parser.
func TestRelativeTimeUnits(t *testing.T) {
	for _, modifier := range []string{"-2hours", "-15min", "-30days", "-1mon", "-10sec", "-1qtr", "-2years", "-7d@w1", "-1mon@mon", "-500ms"} {
		query := `index=x earliest=` + modifier + ` user=bob`
		r := ExtractConditions(query)
		if errs := syntaxErrors(query); len(errs) != 0 {
			t.Errorf("%s: syntax errors %q", query, errs)
		}
		if len(r.Errors) != 0 || r.ExpressionIncomplete || len(r.Conditions) != 2 {
			t.Errorf("%s: errors=%q incomplete=%v conditions=%s", query, r.Errors, r.ExpressionIncomplete, conditionSummary(r.Conditions))
			continue
		}
		assertHasCondition(t, r.Conditions, ExpectedCondition{"index", "=", "x"})
		assertHasCondition(t, r.Conditions, ExpectedCondition{"user", "=", "bob"})
	}
}

// In where, `field <op> func(arg)` compares the field with the function's
// result. It is one opaque scalar predicate, not a comparison with a field
// named after the function plus the argument as a second ANDed operand.
func TestWhereComparisonWithFunctionCall(t *testing.T) {
	for _, tc := range []struct {
		query      string
		scalars    []string
		conditions []ExpectedCondition
	}{
		{`index=x | where user=lower(src_user)`, []string{"user=lower(src_user)"}, []ExpectedCondition{{"index", "=", "x"}}},
		{`index=x | where count>tonumber(threshold)`, []string{"count>tonumber(threshold)"}, []ExpectedCondition{{"index", "=", "x"}}},
		{`index=x | where a!=upper(b) AND c=1`, []string{"a!=upper(b)"}, []ExpectedCondition{{"index", "=", "x"}, {"c", "=", "1"}}},
		{`index=x | where a=mvindex(b,0)`, []string{"a=mvindex(b,0)"}, []ExpectedCondition{{"index", "=", "x"}}},
	} {
		r := ExtractConditions(tc.query)
		if errs := syntaxErrors(tc.query); len(errs) != 0 {
			t.Errorf("%s: syntax errors %q", tc.query, errs)
		}
		if len(r.Errors) != 0 || r.ExpressionIncomplete || len(r.Conditions) != len(tc.conditions) {
			t.Errorf("%s: errors=%q incomplete=%v conditions=%s", tc.query, r.Errors, r.ExpressionIncomplete, conditionSummary(r.Conditions))
			continue
		}
		for _, want := range tc.conditions {
			assertHasCondition(t, r.Conditions, want)
		}
		var scalars []string
		var collect func(*ConditionExpression)
		collect = func(expression *ConditionExpression) {
			if expression == nil {
				return
			}
			if expression.Operator == BooleanScalar && expression.Scalar != nil {
				scalars = append(scalars, expression.Scalar.Expression)
			}
			for _, child := range expression.Children {
				collect(child)
			}
		}
		collect(r.Expression)
		if !reflect.DeepEqual(scalars, tc.scalars) {
			t.Errorf("%s: scalar predicates %q, want %q", tc.query, scalars, tc.scalars)
		}
	}
}

// XOR is an eval operator with the lowest precedence (after OR). The Boolean
// expression model has no exclusive or, so a where clause with XOR must say
// its expression is unavailable instead of reading XOR as a field ANDed in.
func TestWhereXORIsReportedIncomplete(t *testing.T) {
	for _, query := range []string{
		`index=x | where a=1 XOR b=2`,
		`index=x | where (a=1 XOR b=2) OR c=3`,
	} {
		r := ExtractConditions(query)
		if len(r.Errors) != 0 || !r.ExpressionIncomplete || r.Expression != nil {
			t.Errorf("%s: errors=%q incomplete=%v expression=%v", query, r.Errors, r.ExpressionIncomplete, r.Expression)
		}
		assertNoCondition(t, r.Conditions, "XOR")
	}

	// Like AND and OR, XOR is a keyword: a value "xor" needs quotes. Inside an
	// eval assignment it parses as an operator.
	for query, want := range map[string]ExpectedCondition{
		`index=x mode="xor"`:                             {"mode", "=", "xor"},
		`index=x | eval z=if(a XOR b, 1, 0) | where z=1`: {"z", "=", "1"},
	} {
		r := ExtractConditions(query)
		if len(r.Errors) != 0 || r.ExpressionIncomplete || r.Expression == nil {
			t.Errorf("%s: errors=%q incomplete=%v expression=%v", query, r.Errors, r.ExpressionIncomplete, r.Expression)
		}
		assertHasCondition(t, r.Conditions, want)
	}
}

// assertNativeParse requires a complete native parse of query: no syntax
// errors, no incomplete flag, the `where marker=1` stage, and the stage types.
func assertNativeParse(t *testing.T, query string, stageTypes ...string) {
	t.Helper()
	if errs := syntaxErrors(query); len(errs) != 0 {
		t.Errorf("%s: syntax errors %q", query, errs)
	}
	r := ExtractConditions(query)
	if len(r.Errors) != 0 || r.ExpressionIncomplete || !hasCondition(r.Conditions, "marker", "=", "1") {
		t.Errorf("%s: errors=%q incomplete=%v conditions=%s", query, r.Errors, r.ExpressionIncomplete, conditionSummary(r.Conditions))
	}
	stages := ClassifyPipelineStages(query)
	got := make([]string, 0, len(stages))
	for _, stage := range stages {
		got = append(got, stage.CommandType)
	}
	if !reflect.DeepEqual(got, stageTypes) {
		t.Errorf("%s: stage types %q, want %q", query, got, stageTypes)
	}
}

// Options the Search Reference documents for top, rare, bin/bucket, head,
// sort and streamstats, in the positions it shows them.
func TestCommandOptionsParse(t *testing.T) {
	assertNativeParse(t, `index=x | top limit=10 user | where marker=1`, "search", "top", "where")
	assertNativeParse(t, `index=web | top limit=10 useother=t countfield=requests showperc=f uri | where marker=1`, "search", "top", "where")
	assertNativeParse(t, `index=x status=200 | top 1 productName by categoryId showperc=f countfield=total | where marker=1`, "search", "top", "where")
	assertNativeParse(t, `index=dns | rare limit=20 query_type by src_ip | where marker=1`, "search", "rare", "where")
	assertNativeParse(t, `index=x | rare url limit=5 | where marker=1`, "search", "rare", "where")
	assertNativeParse(t, `index=x | bin span=5m _time | where marker=1`, "search", "bucket", "where")
	assertNativeParse(t, `index=x | bin _time span=5m as minute | where marker=1`, "search", "bucket", "where")
	assertNativeParse(t, `index=x | bucket span=1d _time | where marker=1`, "search", "bucket", "where")
	assertNativeParse(t, `index=x | head limit=10 | where marker=1`, "search", "head", "where")
	assertNativeParse(t, `index=x | sort limit=12 host | where marker=1`, "search", "sort", "where")
	assertNativeParse(t, `index=x | streamstats window=10 avg(value) as rolling_avg current=f by host | where marker=1`, "search", "streamstats", "where")
	assertNativeParse(t, `index=x | streamstats avg(foo) by bar window=5 global=f | where marker=1`, "search", "streamstats", "where")
}

// chart OVER <row-split> BY <column-split>, timechart with several functions
// and split-by options, and a comma-separated tstats BY list.
func TestAggregationSplitClausesParse(t *testing.T) {
	assertNativeParse(t, `index=x | chart count over user by action | where marker=1`, "search", "chart", "where")
	assertNativeParse(t, `index=x | chart max(delay) over site by org | where marker=1`, "search", "chart", "where")
	assertNativeParse(t, `index=x | timechart span=1d dc(user) as u, count as c | where marker=1`, "search", "timechart", "where")
	assertNativeParse(t, `index=x | timechart per_hour(price) by productName usenull=f useother=f | where marker=1`, "search", "timechart", "where")
	assertNativeParse(t, `| tstats count where index=* by index, sourcetype | where marker=1`, "tstats", "where")
	assertNativeParse(t, `| tstats count from datamodel=Authentication.Authentication by _time, Authentication.src span=1h | where marker=1`, "tstats", "where")

	r := ExtractConditions(`index=x | chart count over user by action`)
	for _, field := range []string{"user", "action"} {
		if !containsString(r.GroupByFields, field) {
			t.Errorf("chart over/by: GroupByFields %q lacks %s", r.GroupByFields, field)
		}
	}
	r = ExtractConditions(`| tstats count where index=* by index, sourcetype, host`)
	if !reflect.DeepEqual(r.GroupByFields, []string{"index", "sourcetype", "host"}) {
		t.Errorf("tstats BY list: GroupByFields %q, want index, sourcetype and host", r.GroupByFields)
	}
	if aggregations := ExtractConditions(`index=x | timechart dc(user) as u, count as c`).Aggregations; len(aggregations) != 2 {
		t.Errorf("timechart with two functions: aggregations %+v", aggregations)
	}
}

// lookup field AS field OUTPUT, the tag::<field>= search modifier, rename
// pairs without commas and transaction filter strings.
func TestFieldClausesParse(t *testing.T) {
	assertNativeParse(t, `index=x | lookup dnslookup clientip AS src_ip OUTPUT clienthost | where marker=1`, "search", "lookup", "where")
	assertNativeParse(t, `index=x | lookup usertogroup user as local_user OUTPUT group as user_group | where marker=1`, "search", "lookup", "where")
	assertNativeParse(t, `index=x tag::host=web | where marker=1`, "search", "where")
	assertNativeParse(t, `index=x | rename src_user AS "Actioned By" user AS User | where marker=1`, "search", "rename", "where")
	assertNativeParse(t, `index=x | transaction user startswith=EventCode=4624 endswith=EventCode=4634 | where marker=1`, "search", "transaction", "where")
	assertNativeParse(t, `index=x | transaction clientip endswith=eval(first!=last) | where marker=1`, "search", "transaction", "where")

	assertHasCondition(t, ExtractConditions(`index=x tag::host=web`).Conditions, ExpectedCondition{"tag::host", "=", "web"})
	if aliases := ExtractConditions(`index=x | rename src_user AS "Actioned By" user AS User`).FieldAliases; aliases["Actioned By"] != "src_user" || aliases["User"] != "user" {
		t.Errorf("rename pairs without commas: FieldAliases %v", aliases)
	}
}

// fields, table and rename take wildcard field names (fields - _*, rename EU*
// AS EMEA*). The dataflow cannot resolve such a selector, so the stage is
// opaque, but the rest of the query is parsed. A rest endpoint may hold : and
// URL escapes.
func TestWildcardFieldNamesAndRestPathsParse(t *testing.T) {
	assertNativeParse(t, `| rest /services/admin/inputstatus/ModularInputs:modular%20input%20commands splunk_server=local | where marker=1`, "rest", "where")
	assertNativeParse(t, `index=x | fields - _* | where marker=1`, "search", "fields", "where")
	assertNativeParse(t, `index=x | fields inputs* | where marker=1`, "search", "fields", "where")
	assertNativeParse(t, `index=x | table _time inputs* | where marker=1`, "search", "table", "where")
	assertNativeParse(t, `index=x | rename EU* AS EMEA* | where marker=1`, "search", "rename", "where")

	r := ExtractConditions(`index=x | table _time inputs* | where marker=1`)
	if r.Dataflow == nil || r.Dataflow.Complete {
		t.Errorf("a wildcard table selector must leave the dataflow incomplete: %+v", r.Dataflow)
	}
}
