package spl

import "testing"

func TestSPLJoinCommandQuotedOptionsAndFields(t *testing.T) {
	result := ExtractConditions(`index=main | join type="left" max=2 "user name" [ search index=auth user="alice" ]`)

	if len(result.Joins) != 1 {
		t.Fatalf("joins = %#v, want one join", result.Joins)
	}
	join := result.Joins[0]
	if join.Type != "left" {
		t.Fatalf("join type = %q, want left", join.Type)
	}
	if join.Options["max"] != "2" {
		t.Fatalf("join max option = %q, want 2 in %#v", join.Options["max"], join.Options)
	}
	if !containsString(join.JoinFields, "user name") {
		t.Fatalf("quoted join field not preserved: %#v", join.JoinFields)
	}
}

func TestSPLRexCommandQuotedSourceField(t *testing.T) {
	extractor := &conditionExtractor{
		groupByFields:  make([]string, 0),
		computedFields: make(map[string]string),
		commands:       make([]string, 0),
	}
	rexCtx, ok := splParserForTest(t, `rex field="CommandLine" "(?<script>[^ ]+)"`).RexCommand().(*RexCommandContext)
	if !ok {
		t.Fatalf("expected rex context")
	}

	extractor.EnterRexCommand(rexCtx)
	if extractor.computedFields["script"] != "CommandLine" {
		t.Fatalf("quoted rex source field not mapped: %#v", extractor.computedFields)
	}
}

func TestSPLExtractSubsearchTextNilSafe(t *testing.T) {
	extractor := &conditionExtractor{}
	if got := extractor.extractSubsearchText(nil); got != "" {
		t.Fatalf("nil subsearch text = %q, want empty", got)
	}
}
