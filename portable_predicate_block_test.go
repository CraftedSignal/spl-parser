package spl

import (
	"strings"
	"testing"
)

func TestSPLParenthesizedPredicateBlocksIgnoreNonPredicates(t *testing.T) {
	conditions := extractParenthesizedPredicateBlockConditions(`metadata type=hosts (not a predicate) (user="alice" OR status=404)`, 6)

	requireSPLCondition(t, conditions, "user", "=", "alice", false)
	status := requireSPLCondition(t, conditions, "status", "=", "404", false)
	if status.LogicalOp != "OR" {
		t.Fatalf("status logical op = %q, want OR", status.LogicalOp)
	}
}

func TestSPLEmbeddedPredicateBareFallback(t *testing.T) {
	conditions := extractEmbeddedPredicateConditions(`user="alice" OR status=404`, 7)

	requireSPLCondition(t, conditions, "user", "=", "alice", false)
	status := requireSPLCondition(t, conditions, "status", "=", "404", false)
	if status.PipeStage != 7 {
		t.Fatalf("fallback predicate stage = %d, want 7", status.PipeStage)
	}
}

func TestSPLPortablePredicatesStripSubsearchesButKeepQuotedBrackets(t *testing.T) {
	conditions := extractPortablePredicatesFromText(`message="[ user=\"not-subsearch\" ]" [ search user="sub" ] user="main"`, 8)

	requireSPLCondition(t, conditions, "message", "=", `[ user="not-subsearch" ]`, false)
	requireSPLCondition(t, conditions, "user", "=", "main", false)
	for _, condition := range conditions {
		if condition.Field == "user" && condition.Value == "sub" {
			t.Fatalf("subsearch predicate leaked into outer predicates: %#v", conditions)
		}
	}
}

func TestSPLPortableComparisonExtendsBalancedParenthesizedValue(t *testing.T) {
	text := `image=("cmd.exe", if(process=="pwsh","powershell.exe","pwsh.exe")) user="alice"`
	match := portableComparisonPattern.FindStringSubmatchIndex(text)
	if match == nil {
		t.Fatalf("portable comparison did not match %q", text)
	}

	got := portableComparisonMatch(text, match, 9)
	wantEnd := strings.Index(text, ` user=`)
	if got.end != wantEnd {
		t.Fatalf("portableComparisonMatch end = %d, want %d for %#v", got.end, wantEnd, got)
	}
	if got.cond.Field != "image" || got.cond.Operator != "=" || got.cond.Value != "cmd.exe" || got.cond.PipeStage != 9 {
		t.Fatalf("portableComparisonMatch condition = %#v", got.cond)
	}
	if len(got.cond.Alternatives) != 2 || !strings.Contains(got.cond.Alternatives[1], "powershell.exe") {
		t.Fatalf("balanced alternatives not preserved: %#v", got.cond.Alternatives)
	}
}

func TestSPLPortableNegationNestedGroupAndBounds(t *testing.T) {
	text := `NOT (foo="x" AND (bar="y") baz="z")`
	if !portableNegatedAt(text, strings.Index(text, "baz"), false) {
		t.Fatalf("nested NOT group did not apply to baz")
	}
	if !portableNegatedAt("x", 99, true) {
		t.Fatalf("explicit negation should invert false context even when start is out of range")
	}
	if hasWordAt("NO", 0, "NOT") || hasWordAt("NOT", -1, "NOT") {
		t.Fatalf("hasWordAt accepted out-of-range word")
	}
}
