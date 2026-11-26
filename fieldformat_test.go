package spl

import "testing"

// fieldformat <field>=<eval-expression> changes how a field is shown, not
// its value (fieldformat start_time = strftime(start_time, "%H:%M:%S")).
func TestFieldformatParses(t *testing.T) {
	assertNativeParse(t, `index=x | fieldformat start_time = strftime(start_time, "%H:%M:%S") | where marker=1`, "search", "fieldformat", "where")
	assertNativeParse(t, `index=x | fieldformat "First Event"=strftime('First Event', "%c") | where marker=1`, "search", "fieldformat", "where")
	assertNativeParse(t, `index=x | fieldformat total=tostring(total, "commas") | where marker=1`, "search", "fieldformat", "where")
}

// The expression formats; it filters nothing and computes no new value, so
// a comparison inside it is no condition and the field stays a raw one.
func TestFieldformatExpressionIsNoCondition(t *testing.T) {
	r := ExtractConditions(`index=x | fieldformat marker=if(size>5, "big", "small") | where marker=1`)
	if len(r.Errors) != 0 || r.ExpressionIncomplete {
		t.Fatalf("errors=%q incomplete=%v", r.Errors, r.ExpressionIncomplete)
	}
	if r.Dataflow == nil || !r.Dataflow.Complete {
		t.Fatalf("fieldformat should preserve a complete field scope: dataflow=%+v", r.Dataflow)
	}
	for _, stage := range r.Dataflow.Stages {
		if stage.Command == "fieldformat" && stage.Effect != "retain" {
			t.Fatalf("fieldformat should retain its input field values, got stage %+v", stage)
		}
	}
	if hasCondition(r.Conditions, "size", ">", "5") {
		t.Errorf("the comparison inside fieldformat became a condition: %s", conditionSummary(r.Conditions))
	}
	if !hasCondition(r.Conditions, "marker", "=", "1") {
		t.Errorf("the where condition is missing: %s", conditionSummary(r.Conditions))
	}
	for _, c := range r.Conditions {
		if c.Field == "marker" && c.IsComputed {
			t.Errorf("fieldformat made marker a computed field: %+v", c)
		}
	}
}

// fieldformat stays a plain word as a field name and as a value, like the
// other command names.
func TestFieldformatAsAPlainWord(t *testing.T) {
	assertNativeParse(t, `index=x fieldformat=1 | where marker=1`, "search", "where")
	assertNativeParse(t, `index=x action=fieldformat | table fieldformat | where marker=1`, "search", "table", "where")
}
