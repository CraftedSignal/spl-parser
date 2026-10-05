package spl

import "testing"

func TestSPLCompletionDepthLDAPAndDedupEdges(t *testing.T) {
	if conditions := extractPortableSPLConditionsDepth(`search user="alice"`, 7); conditions != nil {
		t.Fatalf("depth guard returned conditions: %#v", conditions)
	}

	ldapSkipped := extractLDAPSearchConditions(`index=main | ldapsearch search="not an ldap filter"`)
	if len(ldapSkipped) != 0 {
		t.Fatalf("non-filter ldapsearch emitted conditions: %#v", ldapSkipped)
	}
	ldapConditions := extractLDAPSearchConditions(`index=main | ldapsearch search="(&(cn=Alice)(memberOf=*))"`)
	requireSPLCondition(t, ldapConditions, "cn", "=", "Alice", false)
	requireSPLCondition(t, ldapConditions, "memberOf", "isnotnull", "", false)

	if deduped := DeduplicateConditions(nil); deduped != nil {
		t.Fatalf("nil conditions deduped to %#v", deduped)
	}

	options := extractQuotedOptionValues(`ldapsearch search='(&(cn=Alice)(sn=\'O\'))' search="ignored"`, "search")
	if len(options) != 2 || decodeSPLStringLiteral(options[0]) != "(&(cn=Alice)(sn='O'))" {
		t.Fatalf("quoted search options not preserved/decoded: %#v", options)
	}
}

func TestSPLCompletionSubsearchTextFallback(t *testing.T) {
	query := NewEmptyQueryContext()
	query.AddTokenNode(newSPLTestToken(SPLParserIDENTIFIER, "search", -1, -1))
	query.AddTokenNode(newSPLTestToken(SPLParserIDENTIFIER, " ", -1, -1))
	query.AddTokenNode(newSPLTestToken(SPLParserIDENTIFIER, `user="alice"`, -1, -1))

	subsearch := NewEmptySubsearchContext()
	subsearch.AddChild(query)

	extractor := &conditionExtractor{originalQuery: `[ search user="alice" ]`}
	if got := extractor.extractSubsearchText(subsearch); got != `search user="alice"` {
		t.Fatalf("subsearch fallback text = %q", got)
	}
}

func TestSPLCompletionEmbeddedPredicateFallback(t *testing.T) {
	conditions := extractEmbeddedPredicateConditions(`noop | sort user="alice"`, 3)
	condition := requireSPLCondition(t, conditions, "user", "=", "alice", false)
	if condition.PipeStage != 3 {
		t.Fatalf("embedded fallback pipe stage = %d, want 3", condition.PipeStage)
	}
}
