package spl

import (
	"strings"
	"testing"
)

// ExpectedCondition defines a condition to find (set-based, not positional)
type ExpectedCondition struct {
	Field    string
	Operator string
	Value    string
}

// ExpectedJoin defines expected join structure
type ExpectedJoin struct {
	Type                string
	JoinFields          []string
	SubsearchConditions []ExpectedCondition
	IsAppend            bool
}

// JoinTestCase is a declarative test case for queries with joins
type JoinTestCase struct {
	Name               string
	Query              string
	MainConditions     []ExpectedCondition
	ExpectedJoins      []ExpectedJoin
	PostJoinConditions []ExpectedCondition
	Provenance         map[string]FieldProvenance
}

// assertHasCondition checks that a ParseResult contains a matching condition
func assertHasCondition(t *testing.T, conditions []Condition, expected ExpectedCondition) {
	t.Helper()
	for _, c := range conditions {
		fieldMatch := strings.EqualFold(c.Field, expected.Field)
		opMatch := expected.Operator == "" || c.Operator == expected.Operator
		valMatch := expected.Value == "" || c.Value == expected.Value
		if fieldMatch && opMatch && valMatch {
			return
		}
	}
	t.Errorf("missing condition: field=%q op=%q value=%q\n  in conditions: %v",
		expected.Field, expected.Operator, expected.Value, conditionSummary(conditions))
}

// assertNoCondition checks that a field is NOT present in conditions
func assertNoCondition(t *testing.T, conditions []Condition, field string) {
	t.Helper()
	for _, c := range conditions {
		if strings.EqualFold(c.Field, field) {
			t.Errorf("unexpected condition on field %q: %+v", field, c)
		}
	}
}

// conditionSummary returns a readable summary of conditions for error messages
func conditionSummary(conditions []Condition) string {
	parts := make([]string, len(conditions))
	for i, c := range conditions {
		parts[i] = c.Field + " " + c.Operator + " " + c.Value
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
