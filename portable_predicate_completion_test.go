package spl

import "testing"

func TestSPLPortableComparisonRemainingEdges(t *testing.T) {
	conditions := extractPortablePredicatesFromText(`and="skip" OR .bad="skip" OR empty=() OR user=="alice" OR image=("cmd.exe","powershell.exe")`, 4)

	user := requireSPLCondition(t, conditions, "user", "==", "alice", false)
	if user.PipeStage != 4 {
		t.Fatalf("portable comparison stage = %d, want 4", user.PipeStage)
	}
	image := requireSPLCondition(t, conditions, "image", "=", "cmd.exe", false)
	if len(image.Alternatives) != 2 || image.Alternatives[1] != "powershell.exe" {
		t.Fatalf("parenthesized comparison alternatives not preserved: %#v", image)
	}

	for _, skipped := range []string{"and", ".bad", "empty"} {
		for _, condition := range conditions {
			if condition.Field == skipped {
				t.Fatalf("invalid/empty portable predicate field %q produced condition %#v in %#v", skipped, condition, conditions)
			}
		}
	}

	for _, field := range []string{"", "null", "true", "field with space", "field>", "field."} {
		if !shouldSkipPortableField(field) {
			t.Fatalf("field %q should be skipped", field)
		}
	}
}

func TestSPLPortableFunctionPredicateSkipEdges(t *testing.T) {
	conditions := extractPortablePredicatesFromText(`net_ip_subnet(and,"10.0.0.0/8") OR net_ip_subnet(src_ip,"") OR cidrmatch("", dest_ip) OR cidrmatch("10.0.0.0/8", null) OR isnull(null) OR isnotnull(and)`, 2)
	if len(conditions) != 0 {
		t.Fatalf("invalid portable function predicates produced conditions: %#v", conditions)
	}
	if got := scanBalancedGroupEnd(`("unterminated`, 0, '(', ')'); got != -1 {
		t.Fatalf("unterminated group end = %d", got)
	}
}
