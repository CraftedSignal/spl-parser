package spl

import (
	"strings"
	"testing"
)

func TestSPLDecodeEscapedQuotesBoundedDeepEscapes(t *testing.T) {
	input := strings.Repeat(`\`, 10) + `"`
	want := strings.Repeat(`\`, 2) + `"`

	if got := decodeEscapedQuotes(input); got != want {
		t.Fatalf("decodeEscapedQuotes() = %q, want %q", got, want)
	}
}

func TestSPLLDAPFilterMalformedInputs(t *testing.T) {
	for _, input := range []string{
		`cn=alice`,
		`(   `,
		`(cn)`,
	} {
		if got := parseLDAPFilterConditions(input, 3); len(got) != 0 {
			t.Fatalf("parseLDAPFilterConditions(%q) = %#v, want none", input, got)
		}
	}
}

func TestSPLLDAPFilterKeepsValidChildrenBeforeMalformedChild(t *testing.T) {
	got := parseLDAPFilterConditions(`(&(cn=alice)(broken))`, 4)
	if len(got) != 1 {
		t.Fatalf("conditions = %#v, want one valid condition", got)
	}
	if got[0].Field != "cn" || got[0].Operator != "=" || got[0].Value != "alice" || got[0].PipeStage != 4 {
		t.Fatalf("condition = %#v", got[0])
	}
}

func TestSPLLDAPAssertionSemanticOperatorsAndInvalidFields(t *testing.T) {
	cases := []struct {
		body     string
		negated  bool
		operator string
		value    string
	}{
		{body: `cn=*`, operator: "isnotnull"},
		{body: `cn=adm*`, operator: "like", value: "adm*"},
		{body: `cn~=alice`, operator: "matches", value: "alice"},
		{body: `cn=alice`, negated: true, operator: "=", value: "alice"},
	}

	for _, tc := range cases {
		got, ok := ldapAssertionCondition(tc.body, tc.negated, 7)
		if !ok {
			t.Fatalf("ldapAssertionCondition(%q) failed", tc.body)
		}
		if got.Field != "cn" || got.Operator != tc.operator || got.Value != tc.value || got.Negated != tc.negated || got.PipeStage != 7 {
			t.Fatalf("ldapAssertionCondition(%q) = %#v", tc.body, got)
		}
	}

	for _, body := range []string{`broken`, `and=alice`, `cn=`} {
		if got, ok := ldapAssertionCondition(body, false, 0); ok {
			t.Fatalf("ldapAssertionCondition(%q) = %#v, want false", body, got)
		}
	}
}

func TestSPLFirstCommandResourceArgNoResource(t *testing.T) {
	if got := firstCommandResourceArg(`ldapsearch`, `ldapsearch`); got != "" {
		t.Fatalf("firstCommandResourceArg() = %q, want empty", got)
	}
}
