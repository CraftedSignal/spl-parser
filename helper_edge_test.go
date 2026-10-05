package spl

import "testing"

func TestHelperNilInputsAreSafe(t *testing.T) {
	if IsStatisticalQuery(nil) {
		t.Fatal("nil parse result should not be statistical")
	}
	if HasUnmappedComputedFields(nil) {
		t.Fatal("nil parse result should not have unmapped computed fields")
	}
	if HasComplexWhereConditions(nil) {
		t.Fatal("nil parse result should not have complex where conditions")
	}
	if got := GetEventTypeFromConditions(nil); got != "" {
		t.Fatalf("nil parse result event type = %q, want empty", got)
	}
}

func TestSPLHelperClassificationEdges(t *testing.T) {
	statistical := &ParseResult{Commands: []string{"search", "stats"}}
	if !IsStatisticalQuery(statistical) {
		t.Fatal("stats command should be classified as statistical")
	}
	if IsStatisticalQuery(&ParseResult{Commands: []string{"search", "where"}}) {
		t.Fatal("non-aggregation commands should not be classified as statistical")
	}

	unmapped := &ParseResult{Conditions: []Condition{
		{Field: "derived", IsComputed: true},
	}}
	if !HasUnmappedComputedFields(unmapped) {
		t.Fatal("computed condition without source field should be unmapped")
	}
	mapped := &ParseResult{Conditions: []Condition{
		{Field: "derived", IsComputed: true, SourceField: "EventCode"},
	}}
	if HasUnmappedComputedFields(mapped) {
		t.Fatal("computed condition with source field should be mapped")
	}

	complex := &ParseResult{
		Commands: []string{"where"},
		Conditions: []Condition{
			{Field: "src_ip", Operator: "cidrmatch", Value: "10.0.0.0/8", PipeStage: 1},
		},
	}
	if !HasComplexWhereConditions(complex) {
		t.Fatal("cidrmatch where operator should be complex")
	}
	negatedWhere := &ParseResult{
		Commands: []string{"where"},
		Conditions: []Condition{
			{Field: "user", Operator: "=", Value: "SYSTEM", Negated: true, PipeStage: 1},
		},
	}
	if !HasComplexWhereConditions(negatedWhere) {
		t.Fatal("negated where condition should be complex")
	}
	notWhere := &ParseResult{
		Commands: []string{"search"},
		Conditions: []Condition{
			{Field: "process", Operator: "matches", Value: ".*", PipeStage: 1},
		},
	}
	if HasComplexWhereConditions(notWhere) {
		t.Fatal("complex operator outside a where command should not be complex where")
	}
	simpleWhere := &ParseResult{
		Commands: []string{"where"},
		Conditions: []Condition{
			{Field: "EventCode", Operator: "=", Value: "4688", PipeStage: 1},
		},
	}
	if HasComplexWhereConditions(simpleWhere) {
		t.Fatal("simple where equality should not be complex")
	}

	eventTypes := map[string]string{
		"4688": "windows_4688",
		"4624": "windows_4624",
		"4625": "windows_4625",
	}
	for eventCode, want := range eventTypes {
		got := GetEventTypeFromConditions(&ParseResult{Conditions: []Condition{
			{Field: "EventCode", Operator: "=", Value: eventCode},
		}})
		if got != want {
			t.Fatalf("EventCode %s mapped to %q, want %q", eventCode, got, want)
		}
	}
	sysmon := &ParseResult{Conditions: []Condition{
		{Field: "sourcetype", Operator: "=", Value: "XmlWinEventLog:Microsoft-Windows-Sysmon/Operational"},
		{Field: "EventCode", Operator: "=", Value: "1"},
	}}
	if got := GetEventTypeFromConditions(sysmon); got != "sysmon_1" {
		t.Fatalf("Sysmon EventCode 1 mapped to %q, want sysmon_1", got)
	}
	sysmonNetwork := &ParseResult{Conditions: []Condition{
		{Field: "sourcetype", Operator: "=", Value: "XmlWinEventLog:Microsoft-Windows-Sysmon/Operational"},
		{Field: "EventCode", Operator: "=", Value: "3"},
	}}
	if got := GetEventTypeFromConditions(sysmonNetwork); got != "sysmon_3" {
		t.Fatalf("Sysmon EventCode 3 mapped to %q, want sysmon_3", got)
	}
	if got := GetEventTypeFromConditions(&ParseResult{Conditions: []Condition{
		{Field: "sourcetype", Operator: "=", Value: "WinEventLog:Security"},
	}}); got != "" {
		t.Fatalf("missing EventCode should not map event type, got %q", got)
	}
	if got := GetEventTypeFromConditions(&ParseResult{Conditions: []Condition{
		{Field: "EventID", Operator: "=", Value: "9999"},
	}}); got != "" {
		t.Fatalf("unknown EventID should not map event type, got %q", got)
	}
}

func TestIsCommandKeywordCaseInsensitive(t *testing.T) {
	if !IsCommandKeyword("StAtS") {
		t.Fatal("stats should be recognized as an SPL command keyword")
	}
	if IsCommandKeyword("EventCode") {
		t.Fatal("event data fields should not be classified as command keywords")
	}
}

func TestPortableNegationWordBoundaries(t *testing.T) {
	conditions := extractPortablePredicatesFromText(`not_user="alice" AND path="C:\notepad.exe" AND NOT action="allow"`, 0)

	assertHasConditionWithNegation(t, conditions, ExpectedCondition{Field: "not_user", Operator: "=", Value: "alice"}, false)
	assertHasConditionWithNegation(t, conditions, ExpectedCondition{Field: "path", Operator: "=", Value: `C:\notepad.exe`}, false)
	assertHasConditionWithNegation(t, conditions, ExpectedCondition{Field: "action", Operator: "=", Value: "allow"}, true)
}

func TestFirstJoinOrSubsearchStageEdges(t *testing.T) {
	if got := FirstJoinOrSubsearchStage(`index=main EventCode=4688 | where user!="SYSTEM"`); got != -1 {
		t.Fatalf("query without join or append returned stage %d, want -1", got)
	}
	if got := FirstJoinOrSubsearchStage(`index=main | stats count by user | join user [ search index=other ]`); got != 2 {
		t.Fatalf("join stage = %d, want 2", got)
	}
	if got := FirstJoinOrSubsearchStage(`index=main | append [ search index=other ]`); got != 1 {
		t.Fatalf("append stage = %d, want 1", got)
	}
}
