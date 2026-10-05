package spl

import "testing"

type wrappedExpressionContext struct {
	*ExpressionContext
	text string
}

func (w *wrappedExpressionContext) GetText() string { return w.text }

func TestSPLEmbeddedPredicateSegmentBoundaryEdges(t *testing.T) {
	cases := []struct {
		segment string
		first   bool
		want    string
		ok      bool
	}{
		{segment: "", ok: false},
		{segment: `search user="alice",`, want: `user="alice"`, ok: true},
		{segment: `where status>=400`, want: `status>=400`, ok: true},
		{segment: `filter action="blocked"`, want: `action="blocked"`, ok: true},
		{segment: `union sourcetype="auth"`, want: `sourcetype="auth"`, ok: true},
		{segment: `and user="alice"`, want: `and user="alice"`, ok: true},
		{segment: `or user="alice"`, want: `or user="alice"`, ok: true},
		{segment: `(user="alice")`, want: `(user="alice")`, ok: true},
		{segment: `!user="alice"`, want: `!user="alice"`, ok: true},
		{segment: `not user="alice"`, want: `not user="alice"`, ok: true},
		{segment: `user="alice"`, first: true, want: `user="alice"`, ok: true},
		{segment: `join user="alice"`, ok: false},
		{segment: `sort user="alice"`, ok: false},
	}

	for _, tc := range cases {
		got, ok := embeddedPredicateSegment(tc.segment, tc.first)
		if ok != tc.ok || got != tc.want {
			t.Fatalf("embeddedPredicateSegment(%q, %v) = (%q, %v), want (%q, %v)", tc.segment, tc.first, got, ok, tc.want, tc.ok)
		}
	}
}

func TestSPLSubsearchJoinAndComparisonEdges(t *testing.T) {
	subsearches := splitBracketedSubsearches(`search message="[not subsearch]" [ search user="alice" [ search src_ip="1.2.3.4" ] ] [ search status="failed" ]`)
	if len(subsearches) != 2 {
		t.Fatalf("splitBracketedSubsearches() = %#v", subsearches)
	}

	if conditions := extractSubsearchConditions(`search [ search user="alice" ]`, 7); conditions != nil {
		t.Fatalf("depth guard returned %#v", conditions)
	}
	subConditions := extractSubsearchConditions(`search message="[not subsearch]" [ search user="alice" [ search src_ip="1.2.3.4" ] ]`, 0)
	requireSPLCondition(t, subConditions, "user", "=", "alice", false)
	requireSPLCondition(t, subConditions, "src_ip", "=", "1.2.3.4", false)

	result := ExtractConditions(`index=main | join type=left max=1 user [ search source="auth" status="failed" | table user src_ip ] | where user=="alice"`)
	if result == nil {
		t.Fatalf("ExtractConditions returned nil")
	}
	if len(result.Joins) == 0 {
		t.Fatalf("join metadata missing: %#v", result)
	}
	join := result.Joins[0]
	if join.Type != "left" || join.Options["max"] != "1" || !containsString(join.JoinFields, "user") {
		t.Fatalf("join options/fields not preserved: %#v", join)
	}
	requireSPLCondition(t, join.Subsearch.Conditions, "status", "=", "failed", false)

	equality := requireSPLCondition(t, result.Conditions, "user", "==", "alice", false)
	if equality.PipeStage == 0 {
		t.Fatalf("where equality should be attributed to a later pipe stage: %#v", equality)
	}
}

func TestSPLExpressionScannerAndDedupBoundaryEdges(t *testing.T) {
	if startsWithDigit("") {
		t.Fatalf("empty string should not start with digit")
	}
	if got := extractValue(nil); got != "" {
		t.Fatalf("nil value = %q", got)
	}
	if values := extractValueList(nil); values != nil {
		t.Fatalf("nil value list = %#v", values)
	}
	identifier := splParserForTest(t, `OtherField`).Value()
	if splValueIsFieldReference(identifier, "") {
		t.Fatalf("empty value should not be treated as field reference")
	}

	if field := extractFirstFieldFromExpression(nil); field != "" {
		t.Fatalf("nil expression field = %q", field)
	}
	if field := extractFirstFieldFromExpression(&wrappedExpressionContext{ExpressionContext: NewEmptyExpressionContext()}); field != "" {
		t.Fatalf("empty expression field = %q", field)
	}
	for expr, want := range map[string]string{
		`""`:                                    "",
		`lower(CommandLine)`:                    "CommandLine",
		`coalesce(first_field, second_field)`:   "first_field",
		`Process."-".CommandLine`:               "Process",
		`case(EventCode=4688, AccountName, "")`: "EventCode",
	} {
		ctx := splParserForTest(t, expr).Expression()
		if got := extractFirstFieldFromExpression(ctx); got != want {
			t.Fatalf("extractFirstFieldFromExpression(%q) = %q, want %q", expr, got, want)
		}
	}

	result := ExtractConditions(`search user="alice" | where src_ip = dest_ip action!="allow"`)
	requireSPLCondition(t, result.Conditions, "src_ip", "=", "", false)
	notEqual := requireSPLCondition(t, result.Conditions, "action", "!=", "allow", false)
	if notEqual.ValueReference != "" {
		t.Fatalf("literal not-equal should not be a field reference: %#v", notEqual)
	}
	spacedEquality := ExtractConditions(`search user == "alice"`)
	requireSPLCondition(t, spacedEquality.Conditions, "user", "==", "alice", false)
	compactEquality := ExtractConditions(`search user=="alice"`)
	requireSPLCondition(t, compactEquality.Conditions, "user", "==", "alice", false)

	deduped := DeduplicateConditions([]Condition{
		{Field: "EventCode", Operator: "=", Value: "*", PipeStage: 2},
		{Field: "User", Operator: "=", Value: "alice", PipeStage: 1},
		{Field: "user", Operator: "=", Value: "alice", PipeStage: 1},
		{Field: "User", Operator: "=", Value: "bob", PipeStage: 1, Negated: true},
		{Field: "User", Operator: "=", Value: "bob", PipeStage: 1, Negated: true},
	})
	if len(deduped) != 2 {
		t.Fatalf("deduped conditions = %#v", deduped)
	}
	requireSPLCondition(t, deduped, "User", "=", "alice", false)
	requireSPLCondition(t, deduped, "User", "=", "bob", true)
}
